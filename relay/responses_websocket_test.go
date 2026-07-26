package relay

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeResponsesWSCreateEventWrapper(t *testing.T) {
	message := []byte(`{
		"type": "response.create",
		"event_id": "evt_1",
		"generate": false,
		"response": {
			"model": "gpt-5.3-codex-spark",
			"input": "hi",
			"store": false,
			"stream": true,
			"stream_options": {"include_usage": true}
		}
	}`)

	create, eventID, err := normalizeResponsesWSCreateEvent(message)
	require.NoError(t, err)
	req := create.Request
	assert.Equal(t, "evt_1", eventID)
	assert.Equal(t, "gpt-5.3-codex-spark", req.Model)
	assert.Equal(t, "false", strings.TrimSpace(string(create.Generate)))
	assert.Nil(t, req.Stream)
	assert.Nil(t, req.StreamOptions)
	assert.Equal(t, "false", strings.TrimSpace(string(req.Store)))
}

func TestNormalizeResponsesWSCreateEventFlat(t *testing.T) {
	message := []byte(`{
		"type": "response.create",
		"event_id": "evt_2",
		"model": "gpt-5.3-codex-spark",
		"input": "hi",
		"generate": false,
		"stream": true,
		"background": true,
		"stream_options": {"include_usage": true}
	}`)

	create, eventID, err := normalizeResponsesWSCreateEvent(message)
	require.NoError(t, err)
	req := create.Request
	assert.Equal(t, "evt_2", eventID)
	assert.Equal(t, "gpt-5.3-codex-spark", req.Model)
	assert.Equal(t, "false", strings.TrimSpace(string(create.Generate)))
	assert.Nil(t, req.Stream)
	assert.Nil(t, req.StreamOptions)
}

func TestBuildResponsesWSCreateEventIsFlat(t *testing.T) {
	payload := []byte(`{
		"model": "gpt-5.3-codex-spark",
		"input": "hi",
		"store": false,
		"event_id": "evt_upstream",
		"stream": true,
		"background": true,
		"stream_options": {"include_usage": true}
	}`)

	got, err := buildResponsesWSCreateEvent(payload, common.RawMessage(`false`))
	require.NoError(t, err)
	var data map[string]any
	require.NoError(t, common.Unmarshal(got, &data))
	assert.Equal(t, responsesWSEventTypeResponseCreate, data["type"])
	assert.Equal(t, "gpt-5.3-codex-spark", data["model"])
	assert.Equal(t, "hi", data["input"])
	assert.Equal(t, false, data["store"])
	assert.Equal(t, false, data["generate"])
	for _, key := range []string{"response", "event_id", "stream", "background", "stream_options"} {
		assert.NotContains(t, data, key, "field %q should not be present in upstream event", key)
	}
}

func TestHTTPResponsesRequestDoesNotMarshalGenerate(t *testing.T) {
	var req dto.OpenAIResponsesRequest
	require.NoError(t, common.Unmarshal([]byte(`{"model":"gpt-5.3-codex-spark","input":"hi","generate":false}`), &req))
	got, err := common.Marshal(req)
	require.NoError(t, err)
	var data map[string]any
	require.NoError(t, common.Unmarshal(got, &data))
	assert.NotContains(t, data, "generate", "generate leaked into HTTP request JSON: %s", got)
}

func TestBuildResponsesWSErrorPayloadIncludesStatus(t *testing.T) {
	payload, err := buildResponsesWSErrorPayload("evt_err", types.NewErrorWithStatusCode(
		errors.New("model is required"),
		types.ErrorCodeInvalidRequest,
		http.StatusBadRequest,
		types.ErrOptionWithSkipRetry(),
	))
	require.NoError(t, err)
	var data struct {
		Type    string             `json:"type"`
		Status  int                `json:"status"`
		EventID string             `json:"event_id"`
		Error   *types.OpenAIError `json:"error"`
	}
	require.NoError(t, common.Unmarshal(payload, &data))
	assert.Equal(t, "error", data.Type)
	assert.Equal(t, http.StatusBadRequest, data.Status)
	assert.Equal(t, "evt_err", data.EventID)
	require.NotNil(t, data.Error)
	assert.Equal(t, string(types.ErrorCodeInvalidRequest), data.Error.Code)
}

func TestResponsesWSInvalidRequestErrorUsesBadRequestStatus(t *testing.T) {
	payload, err := buildResponsesWSErrorPayload("", newResponsesWSInvalidRequestError(errors.New("bad event")))
	require.NoError(t, err)
	var data struct {
		Status int `json:"status"`
	}
	require.NoError(t, common.Unmarshal(payload, &data))
	assert.Equal(t, http.StatusBadRequest, data.Status)
}

func TestRemoveResponsesWSTransportFields(t *testing.T) {
	payload := []byte(`{
		"model": "gpt-5.3-codex-spark",
		"stream": true,
		"background": true,
		"stream_options": {"include_usage": true},
		"store": false
	}`)

	got, err := removeResponsesWSTransportFields(payload)
	require.NoError(t, err)
	var data map[string]any
	require.NoError(t, common.Unmarshal(got, &data))
	for _, key := range []string{"stream", "background", "stream_options"} {
		assert.NotContains(t, data, key, "transport field %q still present in %s", key, got)
	}
	assert.Equal(t, false, data["store"])
}

func TestToWebSocketURL(t *testing.T) {
	tests := map[string]string{
		"https://api.openai.com/v1/responses":             "wss://api.openai.com/v1/responses",
		"http://127.0.0.1:3000/v1/responses":              "ws://127.0.0.1:3000/v1/responses",
		"wss://chatgpt.com/backend-api/codex/responses":   "wss://chatgpt.com/backend-api/codex/responses",
		"ws://127.0.0.1:3000/backend-api/codex/responses": "ws://127.0.0.1:3000/backend-api/codex/responses",
	}

	for input, want := range tests {
		assert.Equal(t, want, toWebSocketURL(input), "toWebSocketURL(%q)", input)
	}
}

func TestHandleTargetWriteFailureWithStateReleasesCurrentAndClearsTarget(t *testing.T) {
	target, cleanup := newTestResponsesWSTarget(t)
	defer cleanup()

	var committed *bool
	session := &responsesWSSession{target: target}
	state := &responsesWSCallState{
		info: &relaycommon.RelayInfo{},
		commitRate: func(success bool) {
			committed = &success
		},
	}
	session.current = state

	apiErr := session.handleTargetWriteFailureWithState(state, errors.New("write failed"))

	require.NotNil(t, apiErr)
	assert.Nil(t, session.target, "target was not cleared")
	assert.Nil(t, session.getCurrent(), "current response was not released")
	require.NotNil(t, committed, "commit was not invoked")
	assert.False(t, *committed)
}

func TestHandleControlEventWriteFailureSendsResponsesError(t *testing.T) {
	clientConn, serverConn, cleanupClient := newTestWebSocketPair(t)
	defer cleanupClient()
	target, cleanupTarget := newTestResponsesWSTarget(t)
	defer cleanupTarget()

	session := &responsesWSSession{
		client: serverConn,
		target: target,
	}
	apiErr := session.handleControlEventWriteFailure(errors.New("write failed"))
	require.Nil(t, apiErr)
	assert.Nil(t, session.target, "target was not cleared")

	require.NoError(t, clientConn.SetReadDeadline(time.Now().Add(time.Second)))
	_, payload, err := clientConn.ReadMessage()
	require.NoError(t, err)
	var data struct {
		Type   string `json:"type"`
		Status int    `json:"status"`
	}
	require.NoError(t, common.Unmarshal(payload, &data))
	assert.Equal(t, "error", data.Type)
	assert.NotZero(t, data.Status)
}

func TestObserveUpstreamFailedReleasesCurrent(t *testing.T) {
	var committed *bool
	session := &responsesWSSession{}
	state := &responsesWSCallState{
		info: &relaycommon.RelayInfo{},
		commitRate: func(success bool) {
			committed = &success
		},
	}
	session.current = state

	session.observeUpstreamMessage([]byte(`{"type":"response.failed"}`))

	assert.Nil(t, session.getCurrent(), "current response was not released")
	require.NotNil(t, committed, "commit was not invoked")
	assert.False(t, *committed)
}

func TestResponsesWSIdleActivityIgnoresPingAndRefreshesOnDataMessage(t *testing.T) {
	clientPeer, client, cleanup := newTestWebSocketPair(t)
	defer cleanup()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	pingSeen := make(chan struct{}, 1)
	defaultPingHandler := client.PingHandler()
	client.SetPingHandler(func(data string) error {
		pingSeen <- struct{}{}
		return defaultPingHandler(data)
	})
	refreshCalls := make(chan struct{}, 3)
	refresh := func(conn *websocket.Conn) error {
		refreshCalls <- struct{}{}
		return conn.SetReadDeadline(time.Time{})
	}
	done := make(chan *types.NewAPIError, 1)
	go func() {
		done <- responsesWebSocketHelper(ctx, client, refresh)
	}()

	select {
	case <-refreshCalls:
	case <-time.After(time.Second):
		t.Fatal("initial WebSocket idle deadline was not set")
	}
	require.NoError(t, clientPeer.WriteControl(websocket.PingMessage, []byte("heartbeat"), time.Now().Add(time.Second)))
	select {
	case <-pingSeen:
	case <-time.After(time.Second):
		t.Fatal("server did not process WebSocket ping")
	}
	select {
	case <-refreshCalls:
		t.Fatal("WebSocket ping refreshed the application-message idle deadline")
	default:
	}

	require.NoError(t, clientPeer.WriteMessage(websocket.TextMessage, []byte(`{}`)))
	select {
	case <-refreshCalls:
	case <-time.After(time.Second):
		t.Fatal("data message did not refresh the WebSocket idle deadline")
	}
	require.NoError(t, clientPeer.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"),
		time.Now().Add(time.Second),
	))
	select {
	case apiErr := <-done:
		assert.Nil(t, apiErr)
	case <-time.After(time.Second):
		t.Fatal("responses WebSocket helper did not stop after client close")
	}
}

func TestResponsesWSIdleTimeoutClosesConnection(t *testing.T) {
	clientPeer, client, cleanup := newTestWebSocketPair(t)
	defer cleanup()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	refresh := func(conn *websocket.Conn) error {
		return conn.SetReadDeadline(time.Now().Add(25 * time.Millisecond))
	}
	done := make(chan *types.NewAPIError, 1)
	go func() {
		done <- responsesWebSocketHelper(ctx, client, refresh)
	}()

	require.NoError(t, clientPeer.SetReadDeadline(time.Now().Add(time.Second)))
	_, _, err := clientPeer.ReadMessage()
	var closeErr *websocket.CloseError
	require.ErrorAs(t, err, &closeErr)
	assert.Equal(t, websocket.CloseGoingAway, closeErr.Code)
	assert.Equal(t, relaycommon.WebSocketIdleCloseReason, closeErr.Text)
	select {
	case apiErr := <-done:
		assert.Nil(t, apiErr)
	case <-time.After(time.Second):
		t.Fatal("responses WebSocket helper did not stop after idle timeout")
	}
}

func newTestResponsesWSTarget(t *testing.T) (*websocket.Conn, func()) {
	t.Helper()
	target, _, cleanup := newTestWebSocketPair(t)
	return target, cleanup
}

func newTestWebSocketPair(t *testing.T) (*websocket.Conn, *websocket.Conn, func()) {
	t.Helper()
	upgrader := websocket.Upgrader{}
	serverConnCh := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		serverConnCh <- conn
	}))

	targetURL := "ws" + strings.TrimPrefix(server.URL, "http")
	target, _, err := websocket.DefaultDialer.Dial(targetURL, nil)
	if err != nil {
		server.Close()
		t.Fatalf("dial websocket: %v", err)
	}
	serverConn := <-serverConnCh
	cleanup := func() {
		_ = target.Close()
		_ = serverConn.Close()
		server.Close()
	}
	return target, serverConn, cleanup
}
