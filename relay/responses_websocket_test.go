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
	if err != nil {
		t.Fatalf("normalizeResponsesWSCreateEvent() error = %v", err)
	}
	req := create.Request
	if eventID != "evt_1" {
		t.Fatalf("eventID = %q, want evt_1", eventID)
	}
	if req.Model != "gpt-5.3-codex-spark" {
		t.Fatalf("model = %q", req.Model)
	}
	if strings.TrimSpace(string(create.Generate)) != "false" {
		t.Fatalf("generate = %s, want false", create.Generate)
	}
	if req.Stream != nil {
		t.Fatalf("stream = %v, want nil", req.Stream)
	}
	if req.StreamOptions != nil {
		t.Fatalf("stream_options = %#v, want nil", req.StreamOptions)
	}
	if strings.TrimSpace(string(req.Store)) != "false" {
		t.Fatalf("store = %s, want false", req.Store)
	}
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
	if err != nil {
		t.Fatalf("normalizeResponsesWSCreateEvent() error = %v", err)
	}
	req := create.Request
	if eventID != "evt_2" {
		t.Fatalf("eventID = %q, want evt_2", eventID)
	}
	if req.Model != "gpt-5.3-codex-spark" {
		t.Fatalf("model = %q", req.Model)
	}
	if strings.TrimSpace(string(create.Generate)) != "false" {
		t.Fatalf("generate = %s, want false", create.Generate)
	}
	if req.Stream != nil {
		t.Fatalf("stream = %v, want nil", req.Stream)
	}
	if req.StreamOptions != nil {
		t.Fatalf("stream_options = %#v, want nil", req.StreamOptions)
	}
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
	if err != nil {
		t.Fatalf("buildResponsesWSCreateEvent() error = %v", err)
	}
	var data map[string]any
	if err := common.Unmarshal(got, &data); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if data["type"] != responsesWSEventTypeResponseCreate {
		t.Fatalf("type = %#v", data["type"])
	}
	if data["model"] != "gpt-5.3-codex-spark" || data["input"] != "hi" || data["store"] != false {
		t.Fatalf("unexpected flat event fields: %s", got)
	}
	if data["generate"] != false {
		t.Fatalf("generate = %#v, want false", data["generate"])
	}
	for _, key := range []string{"response", "event_id", "stream", "background", "stream_options"} {
		if _, ok := data[key]; ok {
			t.Fatalf("field %q should not be present in upstream event: %s", key, got)
		}
	}
}

func TestHTTPResponsesRequestDoesNotMarshalGenerate(t *testing.T) {
	var req dto.OpenAIResponsesRequest
	if err := common.Unmarshal([]byte(`{"model":"gpt-5.3-codex-spark","input":"hi","generate":false}`), &req); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	got, err := common.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var data map[string]any
	if err := common.Unmarshal(got, &data); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if _, ok := data["generate"]; ok {
		t.Fatalf("generate leaked into HTTP request JSON: %s", got)
	}
}

func TestBuildResponsesWSErrorPayloadIncludesStatus(t *testing.T) {
	payload, err := buildResponsesWSErrorPayload("evt_err", types.NewErrorWithStatusCode(
		errors.New("model is required"),
		types.ErrorCodeInvalidRequest,
		http.StatusBadRequest,
		types.ErrOptionWithSkipRetry(),
	))
	if err != nil {
		t.Fatalf("buildResponsesWSErrorPayload() error = %v", err)
	}
	var data struct {
		Type    string             `json:"type"`
		Status  int                `json:"status"`
		EventID string             `json:"event_id"`
		Error   *types.OpenAIError `json:"error"`
	}
	if err := common.Unmarshal(payload, &data); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if data.Type != "error" || data.Status != http.StatusBadRequest || data.EventID != "evt_err" {
		t.Fatalf("unexpected error event: %s", payload)
	}
	if data.Error == nil || data.Error.Code != string(types.ErrorCodeInvalidRequest) {
		t.Fatalf("unexpected error body: %#v", data.Error)
	}
}

func TestResponsesWSInvalidRequestErrorUsesBadRequestStatus(t *testing.T) {
	payload, err := buildResponsesWSErrorPayload("", newResponsesWSInvalidRequestError(errors.New("bad event")))
	if err != nil {
		t.Fatalf("buildResponsesWSErrorPayload() error = %v", err)
	}
	var data struct {
		Status int `json:"status"`
	}
	if err := common.Unmarshal(payload, &data); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if data.Status != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", data.Status, http.StatusBadRequest)
	}
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
	if err != nil {
		t.Fatalf("removeResponsesWSTransportFields() error = %v", err)
	}
	var data map[string]any
	if err := common.Unmarshal(got, &data); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	for _, key := range []string{"stream", "background", "stream_options"} {
		if _, ok := data[key]; ok {
			t.Fatalf("transport field %q still present in %s", key, got)
		}
	}
	if data["store"] != false {
		t.Fatalf("store = %#v, want false", data["store"])
	}
}

func TestToWebSocketURL(t *testing.T) {
	tests := map[string]string{
		"https://api.openai.com/v1/responses":             "wss://api.openai.com/v1/responses",
		"http://127.0.0.1:3000/v1/responses":              "ws://127.0.0.1:3000/v1/responses",
		"wss://chatgpt.com/backend-api/codex/responses":   "wss://chatgpt.com/backend-api/codex/responses",
		"ws://127.0.0.1:3000/backend-api/codex/responses": "ws://127.0.0.1:3000/backend-api/codex/responses",
	}

	for input, want := range tests {
		if got := toWebSocketURL(input); got != want {
			t.Fatalf("toWebSocketURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestPrepareResponsesWebSocketHeaders(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	c.Request.Header.Set("OpenAI-Beta", "responses_websockets=2026-02-06")
	header := http.Header{
		"Accept":       []string{"text/event-stream"},
		"Content-Type": []string{"application/json"},
	}

	prepareResponsesWebSocketHeaders(c, &header)

	assert.Empty(t, header.Get("Accept"), "SSE Accept must not leak into a WebSocket handshake")
	assert.Equal(t, "application/json", header.Get("Content-Type"))
	assert.Equal(t, "responses_websockets=2026-02-06", header.Get("OpenAI-Beta"))
}

func TestPrepareResponsesWebSocketHeadersWithoutClientBetaPreservesAdaptorBeta(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	header := http.Header{}
	header.Set("Accept", "text/event-stream")
	header.Set("OpenAI-Beta", "responses=experimental")

	prepareResponsesWebSocketHeaders(c, &header)

	assert.Empty(t, header.Get("Accept"))
	assert.Equal(t, "responses=experimental", header.Get("OpenAI-Beta"))
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

	if apiErr == nil {
		t.Fatal("apiErr is nil")
	}
	if session.target != nil {
		t.Fatal("target was not cleared")
	}
	if session.getCurrent() != nil {
		t.Fatal("current response was not released")
	}
	if committed == nil || *committed {
		t.Fatalf("commit success = %v, want false", committed)
	}
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
	if apiErr != nil {
		t.Fatalf("handleControlEventWriteFailure() error = %v", apiErr)
	}
	if session.target != nil {
		t.Fatal("target was not cleared")
	}

	if err := clientConn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	_, payload, err := clientConn.ReadMessage()
	if err != nil {
		t.Fatalf("read responses error event: %v", err)
	}
	var data struct {
		Type   string `json:"type"`
		Status int    `json:"status"`
	}
	if err := common.Unmarshal(payload, &data); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if data.Type != "error" || data.Status == 0 {
		t.Fatalf("unexpected error event: %s", payload)
	}
}

// TestFinalizeResponsesWSUsageBillsInterruptedStream pins the billing policy
// for a stream that never reached its terminal event — the client disconnected,
// upstream died, or the idle timeout fired. Upstream already generated (and
// charged us for) that output, so it must be billable from the observed delta
// text, not refunded in full.
func TestFinalizeResponsesWSUsageBillsInterruptedStream(t *testing.T) {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-sonnet-4"}}
	info.SetEstimatePromptTokens(123)
	state := &responsesWSCallState{info: info, usage: &dto.Usage{}}
	state.outputText.WriteString("partial answer streamed before the client vanished")

	require.True(t, finalizeResponsesWSUsage(state), "generated output must be billable")
	assert.Positive(t, state.usage.CompletionTokens, "completion tokens should be counted from observed output")
	assert.Equal(t, 123, state.usage.PromptTokens, "prompt tokens should fall back to the pre-consume estimate")
	assert.Equal(t, state.usage.PromptTokens+state.usage.CompletionTokens, state.usage.TotalTokens)
}

func TestFinalizeResponsesWSUsageReportsNothingBillableWithoutOutput(t *testing.T) {
	state := &responsesWSCallState{
		info:  &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-sonnet-4"}},
		usage: &dto.Usage{},
	}

	assert.False(t, finalizeResponsesWSUsage(state), "a call that produced nothing must stay refundable")
}

// TestFinishCallAbortedRefundsDespiteObservedOutput guards the other side of the
// policy: when the request never reached upstream there is nothing to pay for,
// even if stale state carries text.
func TestFinishCallAbortedRefundsDespiteObservedOutput(t *testing.T) {
	var committed *bool
	session := &responsesWSSession{}
	state := &responsesWSCallState{
		info:  &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-sonnet-4"}},
		usage: &dto.Usage{},
		commitRate: func(success bool) {
			committed = &success
		},
	}
	state.outputText.WriteString("never sent upstream")
	session.current = state

	session.finishCall(state, responsesWSCallAborted)

	assert.Nil(t, session.getCurrent(), "current response was not released")
	require.NotNil(t, committed, "commit was not invoked")
	assert.False(t, *committed, "an aborted call must not be committed as a successful request")
}

// TestApplyTerminalResponseUsageRecordsFailedResponseUsage covers the fix for
// terminal failure events: upstream reports real usage on response.failed, and
// discarding it meant billing nothing for output the provider already charged.
func TestApplyTerminalResponseUsageRecordsFailedResponseUsage(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	session := &responsesWSSession{c: c}
	state := &responsesWSCallState{
		info:  &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-sonnet-4"}},
		usage: &dto.Usage{},
	}

	session.applyTerminalResponseUsage(state, &dto.OpenAIResponsesResponse{
		Usage: &dto.Usage{InputTokens: 40, OutputTokens: 9, TotalTokens: 49},
	})

	assert.Equal(t, 40, state.usage.PromptTokens)
	assert.Equal(t, 9, state.usage.CompletionTokens)
}

func TestResponsesWSSlotCapIsPerUserAndReleased(t *testing.T) {
	original := responsesWSMaxPerUser
	responsesWSMaxPerUser = 2
	defer func() { responsesWSMaxPerUser = original }()

	const userId = 4242
	require.True(t, acquireResponsesWSSlot(userId))
	require.True(t, acquireResponsesWSSlot(userId))
	assert.False(t, acquireResponsesWSSlot(userId), "third concurrent session must be rejected")
	assert.True(t, acquireResponsesWSSlot(userId+1), "cap must be scoped per user")

	releaseResponsesWSSlot(userId)
	assert.True(t, acquireResponsesWSSlot(userId), "releasing must free a slot")

	releaseResponsesWSSlot(userId)
	releaseResponsesWSSlot(userId)
	releaseResponsesWSSlot(userId + 1)

	responsesWSCountMu.Lock()
	defer responsesWSCountMu.Unlock()
	assert.Empty(t, responsesWSCounts, "fully released users must not leak counter entries")
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

	if session.getCurrent() != nil {
		t.Fatal("current response was not released")
	}
	if committed == nil || *committed {
		t.Fatalf("commit success = %v, want false", committed)
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
