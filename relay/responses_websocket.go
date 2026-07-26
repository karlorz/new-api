package relay

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	appconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	appmodel "github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/wsmanager"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const responsesWSEventTypeResponseCreate = "response.create"

// responsesWSWriteTimeout bounds a single blocked write so a peer that stops
// reading cannot pin a connection forever. Without it the write never returns,
// and idle timeout, channel disable and shutdown all block behind it.
const responsesWSWriteTimeout = 30 * time.Second

// responsesWSMaxMessageBytes bounds one inbound WebSocket message. The HTTP
// body limit does not cover WebSocket frames, so without it a valid key can
// stream unbounded data into memory. It follows MAX_REQUEST_BODY_MB so the same
// payload is accepted over both transports of /v1/responses, and only diverges
// when WEBSOCKET_MAX_MESSAGE_MB is set explicitly.
//
// Read lazily: constant.MaxRequestBodyMB is populated by InitEnv from main, so
// a package-level var here would capture zero.
//
// NOTE: gorilla enforces this against the compressed wire length (conn.go:924,
// before the decompression reader is attached at conn.go:1019). It is a real
// memory bound only while permessage-deflate stays disabled — see the upgrader
// in controller/relay.go.
func responsesWSMaxMessageBytes() int64 {
	maxMB := common.GetEnvOrDefault("WEBSOCKET_MAX_MESSAGE_MB", 0)
	if maxMB <= 0 {
		maxMB = appconstant.MaxRequestBodyMB
	}
	if maxMB <= 0 {
		maxMB = 32
	}
	return int64(maxMB) << 20
}

// responsesWSMaxPerUser caps concurrent Responses WebSocket sessions per user;
// 0 disables the cap. Idle sessions hold a goroutine, a socket and an upstream
// connection for up to WEBSOCKET_IDLE_TIMEOUT_MINUTES.
var responsesWSMaxPerUser = common.GetEnvOrDefault("RESPONSES_WEBSOCKET_MAX_PER_USER", 8)

var (
	responsesWSCountMu sync.Mutex
	responsesWSCounts  = map[int]int{}
)

// responsesWSCallOutcome decides how a finished call is billed.
type responsesWSCallOutcome int

const (
	// responsesWSCallAborted means upstream never accepted the request payload,
	// so nothing was generated and the pre-consumed quota is returned in full.
	responsesWSCallAborted responsesWSCallOutcome = iota
	// responsesWSCallSettled means upstream accepted the request: bill what was
	// observed, whether the stream ended normally, failed, or was cut short by a
	// disconnect. This matches the HTTP and realtime relays, which also settle
	// on mid-stream client disconnect rather than refunding generated output.
	responsesWSCallSettled
)

type responsesWSCreateEvent struct {
	Type    string            `json:"type"`
	EventID string            `json:"event_id,omitempty"`
	Request common.RawMessage `json:"response,omitempty"`
}

type responsesWSCreateRequest struct {
	Request  dto.OpenAIResponsesRequest
	Generate common.RawMessage
}

type responsesWSErrorEvent struct {
	Type    string             `json:"type"`
	Status  int                `json:"status"`
	EventID string             `json:"event_id,omitempty"`
	Error   *types.OpenAIError `json:"error"`
}

type responsesWSCallState struct {
	info       *relaycommon.RelayInfo
	commitRate middleware.ModelRequestRateLimitCommit

	// mu guards usage and outputText. The upstream reader goroutine appends to
	// them while the client goroutine may win the race to finish the call on a
	// disconnect and read them for settlement.
	mu         sync.Mutex
	usage      *dto.Usage
	outputText strings.Builder
	imageCalls *relaycommon.ImageGenerationCallCounter
}

type responsesWSSession struct {
	c              *gin.Context
	client         *websocket.Conn
	target         *websocket.Conn
	unregister     func()
	lockedModel    string
	lockedChannel  *appmodel.Channel
	nextEventIndex int
	closeOnce      sync.Once

	clientWriteMu sync.Mutex
	// targetMu guards target and unregister. It is never held across network
	// I/O, so closing the session cannot block behind an in-flight write.
	targetMu sync.Mutex
	// targetWriteMu only serializes writes, as gorilla allows a single writer.
	targetWriteMu sync.Mutex
	stateMu       sync.Mutex
	current       *responsesWSCallState
}

func ResponsesWebSocketHelper(c *gin.Context, client *websocket.Conn) *types.NewAPIError {
	return responsesWebSocketHelper(c, client, relaycommon.RefreshClientWebSocketReadDeadline)
}

func responsesWebSocketHelper(c *gin.Context, client *websocket.Conn, refreshReadDeadline func(*websocket.Conn) error) *types.NewAPIError {
	userId := common.GetContextKeyInt(c, appconstant.ContextKeyUserId)
	if !acquireResponsesWSSlot(userId) {
		return types.NewErrorWithStatusCode(
			fmt.Errorf("too many concurrent responses websocket connections (limit %d)", responsesWSMaxPerUser),
			types.ErrorCodeInvalidRequest,
			http.StatusTooManyRequests,
			types.ErrOptionWithSkipRetry(),
		)
	}
	defer releaseResponsesWSSlot(userId)

	session := &responsesWSSession{
		c:      c,
		client: client,
	}
	defer session.closeTarget()
	defer session.settleCurrent()
	client.SetReadLimit(responsesWSMaxMessageBytes())
	if err := refreshReadDeadline(client); err != nil {
		return types.NewError(err, types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
	}

	for {
		messageType, message, err := client.ReadMessage()
		if err != nil {
			if relaycommon.IsWebSocketIdleTimeout(err) {
				logger.LogInfo(c, "responses websocket closed after idle timeout")
				session.closeForIdleTimeout()
				return nil
			}
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				return nil
			}
			return types.NewError(err, types.ErrorCodeBadRequestBody, types.ErrOptionWithSkipRetry())
		}
		if err := refreshReadDeadline(client); err != nil {
			return types.NewError(err, types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
		}

		eventType, eventErr := responsesWSEventType(message)
		if eventErr != nil {
			session.sendError("", newResponsesWSInvalidRequestError(eventErr))
			continue
		}

		if eventType != responsesWSEventTypeResponseCreate {
			if !session.hasTarget() {
				session.sendError("", newResponsesWSInvalidRequestError(errors.New("first responses websocket event must be response.create")))
				continue
			}
			if err := session.writeTarget(messageType, message); err != nil {
				return session.handleControlEventWriteFailure(err)
			}
			continue
		}

		create, eventID, err := normalizeResponsesWSCreateEvent(message)
		if err != nil {
			session.sendError("", newResponsesWSInvalidRequestError(err))
			continue
		}
		if err := helper.ValidateResponsesRequest(&create.Request); err != nil {
			session.sendError(eventID, newResponsesWSInvalidRequestError(err))
			continue
		}
		if err := session.handleResponseCreate(create, eventID); err != nil {
			session.sendError(eventID, err)
		}
	}
}

func responsesWSEventType(message []byte) (string, error) {
	var event struct {
		Type string `json:"type"`
	}
	if err := common.Unmarshal(message, &event); err != nil {
		return "", fmt.Errorf("invalid websocket event json: %w", err)
	}
	if strings.TrimSpace(event.Type) == "" {
		return "", errors.New("websocket event type is required")
	}
	return event.Type, nil
}

func newResponsesWSInvalidRequestError(err error) *types.NewAPIError {
	return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
}

func normalizeResponsesWSCreateEvent(message []byte) (responsesWSCreateRequest, string, error) {
	var event responsesWSCreateEvent
	if err := common.Unmarshal(message, &event); err != nil {
		return responsesWSCreateRequest{}, "", err
	}
	if event.Type != responsesWSEventTypeResponseCreate {
		return responsesWSCreateRequest{}, event.EventID, fmt.Errorf("unsupported event type %q", event.Type)
	}

	var generate common.RawMessage
	var raw map[string]common.RawMessage
	if err := common.Unmarshal(message, &raw); err == nil {
		if generateRaw, ok := raw["generate"]; ok {
			generate = generateRaw
		}
	}

	payload := event.Request
	if len(payload) == 0 {
		if err := common.Unmarshal(message, &raw); err != nil {
			return responsesWSCreateRequest{}, event.EventID, err
		}
		delete(raw, "type")
		delete(raw, "event_id")
		delete(raw, "background")
		delete(raw, "generate")
		delete(raw, "stream")
		delete(raw, "stream_options")
		var err error
		payload, err = common.Marshal(raw)
		if err != nil {
			return responsesWSCreateRequest{}, event.EventID, err
		}
	} else {
		var responseMap map[string]common.RawMessage
		if err := common.Unmarshal(payload, &responseMap); err == nil {
			if len(generate) == 0 {
				if generateRaw, ok := responseMap["generate"]; ok {
					generate = generateRaw
				}
			}
			if _, exists := responseMap["generate"]; exists {
				delete(responseMap, "generate")
				if merged, err := common.Marshal(responseMap); err == nil {
					payload = merged
				}
			}
		}
	}

	var req dto.OpenAIResponsesRequest
	if err := common.Unmarshal(payload, &req); err != nil {
		return responsesWSCreateRequest{}, event.EventID, err
	}
	req.Stream = nil
	req.StreamOptions = nil
	return responsesWSCreateRequest{
		Request:  req,
		Generate: generate,
	}, event.EventID, nil
}

func (s *responsesWSSession) handleResponseCreate(create responsesWSCreateRequest, eventID string) *types.NewAPIError {
	req := create.Request
	if s.lockedModel != "" && req.Model != s.lockedModel {
		return types.NewErrorWithStatusCode(
			fmt.Errorf("responses websocket connection is locked to model %q; got %q", s.lockedModel, req.Model),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}

	if s.hasCurrent() {
		return types.NewErrorWithStatusCode(
			errors.New("another response.create is already in progress on this websocket connection"),
			types.ErrorCodeInvalidRequest,
			http.StatusConflict,
			types.ErrOptionWithSkipRetry(),
		)
	}

	commitRate, apiErr := middleware.CheckModelRequestRateLimit(s.c)
	if apiErr != nil {
		return apiErr
	}

	if !s.hasTarget() {
		return s.connectAndSendFirst(create, commitRate)
	}

	state, payload, apiErr := s.prepareCall(create, commitRate)
	if apiErr != nil {
		commitRate(false)
		return apiErr
	}
	if !s.tryReserveCurrent(state) {
		state.refund(s.c)
		commitRate(false)
		return types.NewErrorWithStatusCode(
			errors.New("another response.create is already in progress on this websocket connection"),
			types.ErrorCodeInvalidRequest,
			http.StatusConflict,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if err := s.writeTarget(websocket.TextMessage, payload); err != nil {
		return s.handleTargetWriteFailureWithState(state, err)
	}
	return nil
}

func (s *responsesWSSession) handleControlEventWriteFailure(err error) *types.NewAPIError {
	apiErr := s.handleTargetWriteFailure(err)
	s.sendError("", apiErr)
	return nil
}

func (s *responsesWSSession) handleTargetWriteFailure(err error) *types.NewAPIError {
	s.closeTarget()
	apiErr := types.NewError(err, types.ErrorCodeBadResponse)
	apiErr, _ = s.processChannelError(s.lockedChannel, apiErr, nil)
	return apiErr
}

func (s *responsesWSSession) handleTargetWriteFailureWithState(state *responsesWSCallState, err error) *types.NewAPIError {
	s.finishCall(state, responsesWSCallAborted)
	return s.handleTargetWriteFailure(err)
}

func (s *responsesWSSession) connectAndSendFirst(create responsesWSCreateRequest, commitRate middleware.ModelRequestRateLimitCommit) *types.NewAPIError {
	req := create.Request
	if err := checkResponsesWSModelAccess(s.c, req.Model); err != nil {
		commitRate(false)
		return err
	}

	retryParam := &service.RetryParam{
		Ctx:         s.c,
		TokenGroup:  common.GetContextKeyString(s.c, appconstant.ContextKeyUsingGroup),
		ModelName:   req.Model,
		RequestPath: s.c.Request.URL.Path,
		Retry:       common.GetPointer(0),
	}
	if retryParam.TokenGroup == "" {
		retryParam.TokenGroup = common.GetContextKeyString(s.c, appconstant.ContextKeyTokenGroup)
	}

	var lastErr *types.NewAPIError
	for ; retryParam.GetRetry() <= common.RetryTimes; retryParam.IncreaseRetry() {
		channel, apiErr := selectResponsesWSChannel(s.c, req.Model, retryParam)
		if apiErr != nil {
			lastErr = apiErr
			break
		}
		addResponsesWSUsedChannel(s.c, channel.Id)

		if channel.Type != appconstant.ChannelTypeOpenAI && channel.Type != appconstant.ChannelTypeCodex {
			lastErr = types.NewErrorWithStatusCode(
				fmt.Errorf("responses websocket only supports OpenAI and Codex channels, got channel type %d", channel.Type),
				types.ErrorCodeInvalidRequest,
				http.StatusBadRequest,
				types.ErrOptionWithSkipRetry(),
			)
			continue
		}

		state, payload, apiErr := s.prepareCall(create, commitRate)
		if apiErr != nil {
			commitRate(false)
			return apiErr
		}

		adaptor := GetAdaptor(state.info.ApiType)
		if adaptor == nil {
			state.refund(s.c)
			apiErr = types.NewError(fmt.Errorf("invalid api type: %d", state.info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
			var shouldRetry bool
			lastErr, shouldRetry = s.processChannelError(channel, apiErr, retryParam)
			if !shouldRetry {
				break
			}
			continue
		}
		adaptor.Init(state.info)
		target, apiErr := dialResponsesWebSocketUpstream(s.c, adaptor, state.info)
		if apiErr != nil {
			state.refund(s.c)
			var shouldRetry bool
			lastErr, shouldRetry = s.processChannelError(channel, apiErr, retryParam)
			if !shouldRetry {
				break
			}
			continue
		}

		s.setTarget(target)
		if !s.tryReserveCurrent(state) {
			s.closeTarget()
			state.refund(s.c)
			commitRate(false)
			return types.NewErrorWithStatusCode(errors.New("another response.create is already in progress on this websocket connection"), types.ErrorCodeInvalidRequest, http.StatusConflict, types.ErrOptionWithSkipRetry())
		}
		if err := s.writeTarget(websocket.TextMessage, payload); err != nil {
			s.finishCall(state, responsesWSCallAborted)
			s.closeTarget()
			apiErr = types.NewError(err, types.ErrorCodeBadResponse)
			var shouldRetry bool
			lastErr, shouldRetry = s.processChannelError(channel, apiErr, retryParam)
			if !shouldRetry {
				break
			}
			continue
		}

		s.lockedModel = req.Model
		s.lockedChannel = channel
		s.registerChannelClose(channel.Id)
		service.RecordChannelAffinity(s.c, channel.Id)
		s.startTargetReader()
		return nil
	}

	if lastErr == nil {
		lastErr = types.NewError(errors.New("failed to connect responses websocket upstream"), types.ErrorCodeDoRequestFailed, types.ErrOptionWithSkipRetry())
	}
	commitRate(false)
	return lastErr
}

func (s *responsesWSSession) processChannelError(channel *appmodel.Channel, apiErr *types.NewAPIError, retryParam *service.RetryParam) (*types.NewAPIError, bool) {
	if apiErr == nil {
		return nil, false
	}
	apiErr = service.NormalizeViolationFeeError(apiErr)
	statusCodeMapping := ""
	if s.c != nil {
		statusCodeMapping = s.c.GetString("status_code_mapping")
	}
	service.ResetStatusCode(apiErr, statusCodeMapping)
	if channel != nil && s.c != nil {
		service.ProcessChannelError(s.c, *types.NewChannelError(
			channel.Id,
			channel.Type,
			channel.Name,
			channel.ChannelInfo.IsMultiKey,
			common.GetContextKeyString(s.c, appconstant.ContextKeyChannelKey),
			channel.GetAutoBan(),
		), apiErr)
	}
	if retryParam == nil {
		return apiErr, false
	}
	return apiErr, service.ShouldRetryRelayError(s.c, apiErr, common.RetryTimes-retryParam.GetRetry())
}

func (s *responsesWSSession) prepareCall(create responsesWSCreateRequest, commitRate middleware.ModelRequestRateLimitCommit) (*responsesWSCallState, []byte, *types.NewAPIError) {
	req := create.Request
	common.SetContextKey(s.c, appconstant.ContextKeyRequestStartTime, time.Now())
	relayInfo := relaycommon.GenRelayInfoResponses(s.c, &req)
	// The stream field is stripped from the frame before parsing, so
	// GenRelayInfoResponses sees stream=nil and would record the call as
	// non-stream, which also hides first-response time in the usage-log UI.
	// WebSocket delivery is inherently incremental; mark it streaming like the
	// realtime relay does.
	relayInfo.IsStream = true
	s.c.Set(string(appconstant.ContextKeyIsStream), true)
	relayInfo.RequestId = fmt.Sprintf("%s-ws-%d", relayInfo.RequestId, s.nextEventIndex)
	s.nextEventIndex++

	meta := req.GetTokenCountMeta()
	if setting.ShouldCheckPromptSensitive() && meta != nil {
		contains, words := service.CheckSensitiveText(meta.CombineText)
		if contains {
			return nil, nil, types.NewError(fmt.Errorf("user sensitive words detected: %s", strings.Join(words, ", ")), types.ErrorCodeSensitiveWordsDetected, types.ErrOptionWithSkipRetry())
		}
	}

	tokens, err := service.EstimateRequestToken(s.c, meta, relayInfo)
	if err != nil {
		return nil, nil, types.NewError(err, types.ErrorCodeCountTokenFailed)
	}
	relayInfo.SetEstimatePromptTokens(tokens)

	priceData, err := helper.ModelPriceHelper(s.c, relayInfo, tokens, meta)
	if err != nil {
		return nil, nil, types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest))
	}
	if !priceData.FreeModel {
		if apiErr := service.PreConsumeBilling(s.c, priceData.QuotaToPreConsume, relayInfo); apiErr != nil {
			return nil, nil, apiErr
		}
	}

	payload, apiErr := buildResponsesWSCreatePayload(s.c, relayInfo, req, create.Generate)
	if apiErr != nil {
		if relayInfo.Billing != nil {
			relayInfo.Billing.Refund(s.c)
		}
		return nil, nil, apiErr
	}

	relayInfo.ClientWs = s.client

	return &responsesWSCallState{
		info:       relayInfo,
		usage:      &dto.Usage{},
		imageCalls: &relaycommon.ImageGenerationCallCounter{},
		commitRate: commitRate,
	}, payload, nil
}

func buildResponsesWSCreatePayload(c *gin.Context, relayInfo *relaycommon.RelayInfo, req dto.OpenAIResponsesRequest, generate common.RawMessage) ([]byte, *types.NewAPIError) {
	relayInfo.InitChannelMeta(c)
	request, err := common.DeepCopy(&req)
	if err != nil {
		return nil, types.NewError(fmt.Errorf("failed to copy responses request: %w", err), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	if err := helper.ModelMappedHelper(c, relayInfo, request); err != nil {
		return nil, types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}

	adaptor := GetAdaptor(relayInfo.ApiType)
	if adaptor == nil {
		return nil, types.NewError(fmt.Errorf("invalid api type: %d", relayInfo.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(relayInfo)
	convertedRequest, err := adaptor.ConvertOpenAIResponsesRequest(c, relayInfo, *request)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	relaycommon.AppendRequestConversionFromRequest(relayInfo, convertedRequest)
	jsonData, err := common.Marshal(convertedRequest)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	jsonData, err = relaycommon.RemoveDisabledFields(jsonData, relayInfo.ChannelOtherSettings, relayInfo.ChannelSetting.PassThroughBodyEnabled)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	jsonData, err = removeResponsesWSTransportFields(jsonData)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	if len(relayInfo.ParamOverride) > 0 {
		jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, relayInfo)
		if err != nil {
			return nil, newAPIErrorFromParamOverride(err)
		}
	}

	event, err := buildResponsesWSCreateEvent(jsonData, generate)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	return event, nil
}

func buildResponsesWSCreateEvent(jsonData []byte, generate common.RawMessage) ([]byte, error) {
	var event map[string]common.RawMessage
	if err := common.Unmarshal(jsonData, &event); err != nil {
		return nil, err
	}
	typeData, err := common.Marshal(responsesWSEventTypeResponseCreate)
	if err != nil {
		return nil, err
	}
	event["type"] = typeData
	delete(event, "event_id")
	delete(event, "background")
	delete(event, "stream")
	delete(event, "stream_options")
	if len(generate) > 0 {
		event["generate"] = generate
	}
	return common.Marshal(event)
}

func removeResponsesWSTransportFields(jsonData []byte) ([]byte, error) {
	var data map[string]any
	if err := common.Unmarshal(jsonData, &data); err != nil {
		return jsonData, err
	}
	delete(data, "stream")
	delete(data, "stream_options")
	delete(data, "background")
	return common.Marshal(data)
}

func dialResponsesWebSocketUpstream(c *gin.Context, adaptor relaychannel.Adaptor, info *relaycommon.RelayInfo) (*websocket.Conn, *types.NewAPIError) {
	fullRequestURL, err := adaptor.GetRequestURL(info)
	if err != nil {
		return nil, types.NewError(fmt.Errorf("get request url failed: %w", err), types.ErrorCodeDoRequestFailed)
	}
	fullRequestURL = toWebSocketURL(fullRequestURL)

	targetHeader := http.Header{}
	if err := adaptor.SetupRequestHeader(c, &targetHeader, info); err != nil {
		return nil, types.NewError(fmt.Errorf("setup request header failed: %w", err), types.ErrorCodeDoRequestFailed)
	}
	headerOverride, err := relaychannel.ResolveHeaderOverride(info, c)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeChannelHeaderOverrideInvalid)
	}
	for key, value := range headerOverride {
		targetHeader.Set(key, value)
	}

	targetConn, resp, err := websocket.DefaultDialer.Dial(fullRequestURL, targetHeader)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if resp != nil {
			statusCode = resp.StatusCode
		}
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("dial failed to %s: %w", relaycommon.SanitizeURLForLog(fullRequestURL), err), types.ErrorCodeDoRequestFailed, statusCode)
	}
	targetConn.SetReadLimit(responsesWSMaxMessageBytes())
	return targetConn, nil
}

func toWebSocketURL(raw string) string {
	switch {
	case strings.HasPrefix(raw, "https://"):
		return "wss://" + strings.TrimPrefix(raw, "https://")
	case strings.HasPrefix(raw, "http://"):
		return "ws://" + strings.TrimPrefix(raw, "http://")
	default:
		return raw
	}
}

func (s *responsesWSSession) startTargetReader() {
	target := s.getTarget()
	if target == nil {
		return
	}
	go func() {
		for {
			messageType, message, err := target.ReadMessage()
			if err != nil {
				if !s.isTarget(target) {
					// The session already replaced or closed this upstream
					// connection; do not tear down the client for a stale reader.
					return
				}
				if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					logger.LogError(s.c, "responses websocket upstream read failed: "+err.Error())
				}
				s.settleCurrent()
				_ = s.client.Close()
				return
			}
			s.observeUpstreamMessage(message)
			if err := s.writeClient(messageType, message); err != nil {
				logger.LogError(s.c, "responses websocket client write failed: "+err.Error())
				s.settleCurrent()
				s.closeTarget()
				return
			}
			// Upstream traffic also counts as activity: a long generation can
			// stream for minutes while the client only listens, and that must
			// not trip the client idle timeout.
			//
			// Calling this from the reader goroutine while the client goroutine
			// blocks in ReadMessage is safe despite gorilla listing
			// SetReadDeadline as a read method: it is a bare passthrough to
			// net.Conn.SetReadDeadline (conn.go:1105), which is documented to be
			// callable concurrently with a blocked Read.
			_ = relaycommon.RefreshClientWebSocketReadDeadline(s.client)
		}
	}()
}

func (s *responsesWSSession) observeUpstreamMessage(message []byte) {
	state := s.getCurrent()
	if state == nil {
		return
	}
	state.info.SetFirstResponseTime()

	var streamResponse dto.ResponsesStreamResponse
	if err := common.Unmarshal(message, &streamResponse); err != nil {
		return
	}

	switch streamResponse.Type {
	case "response.completed", "response.done", "response.incomplete",
		"response.failed", "response.cancelled", "response.canceled":
		// A terminal event carries the authoritative usage even when the response
		// failed, so settle on it instead of discarding what upstream generated
		// and already billed us for.
		s.applyTerminalResponseUsage(state, streamResponse.Response)
		s.finishCall(state, responsesWSCallSettled)
	case "response.output_text.delta":
		state.mu.Lock()
		state.outputText.WriteString(streamResponse.Delta)
		state.mu.Unlock()
	case dto.ResponsesOutputTypeItemDone:
		if streamResponse.Item != nil {
			state.imageCalls.Observe(streamResponse.Item, streamResponse.OutputIndex)
			switch streamResponse.Item.Type {
			case dto.BuildInCallWebSearchCall, dto.BuildInCallFileSearchCall:
				state.info.CountBillableToolCall(streamResponse.Item.Type, "")
			case dto.BuildInCallFunctionCall:
				state.info.CountBillableToolCall(streamResponse.Item.Type, streamResponse.Item.Name)
			}
		}
	case "error":
		s.finishCall(state, responsesWSCallSettled)
	}
}

func (s *responsesWSSession) applyTerminalResponseUsage(state *responsesWSCallState, response *dto.OpenAIResponsesResponse) {
	if state == nil || response == nil {
		return
	}
	if response.Usage != nil {
		state.mu.Lock()
		service.ApplyResponsesUsage(state.usage, response.Usage)
		state.mu.Unlock()
	}
	if relaycommon.IsNonBillableResponsesStatus(response.Status) {
		state.imageCalls.Reset()
	} else {
		for i := range response.Output {
			idx := i
			state.imageCalls.Observe(&response.Output[i], &idx)
		}
	}
	state.imageCalls.Commit(state.info)
}

func (s *responsesWSSession) finishCall(state *responsesWSCallState, outcome responsesWSCallOutcome) {
	if state == nil {
		return
	}
	if !s.clearCurrent(state) {
		return
	}
	// Refund only when upstream produced nothing: either it never accepted the
	// request, or it accepted but no usage and no output text were observed.
	// Anything actually generated gets billed, otherwise disconnecting just
	// before the terminal event would yield free output.
	if outcome == responsesWSCallAborted || !finalizeResponsesWSUsage(state) {
		state.refund(s.c)
		if state.commitRate != nil {
			state.commitRate(false)
		}
		return
	}

	// Bill a snapshot: the goroutine that lost the clearCurrent race may still
	// be applying a late terminal event to state.usage under state.mu.
	state.mu.Lock()
	usage := *state.usage
	state.mu.Unlock()
	service.PostTextConsumeQuota(s.c, state.info, &usage, nil)
	if state.commitRate != nil {
		state.commitRate(true)
	}
}

// finalizeResponsesWSUsage fills in what upstream did not report — the usual
// case for a stream cut short — and reports whether anything is billable.
func finalizeResponsesWSUsage(state *responsesWSCallState) bool {
	if state == nil || state.usage == nil || state.info == nil {
		return false
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.usage.CompletionTokens == 0 {
		if output := state.outputText.String(); output != "" {
			state.usage.CompletionTokens = service.CountTextToken(output, state.info.UpstreamModelName)
		}
	}
	if state.usage.PromptTokens == 0 && state.usage.CompletionTokens != 0 {
		state.usage.PromptTokens = state.info.GetEstimatePromptTokens()
	}
	if state.usage.TotalTokens == 0 {
		state.usage.TotalTokens = state.usage.PromptTokens + state.usage.CompletionTokens
	}
	return state.usage.TotalTokens > 0
}

func (state *responsesWSCallState) refund(c *gin.Context) {
	if state != nil && state.info != nil && state.info.Billing != nil {
		state.info.Billing.Refund(c)
	}
}

func (s *responsesWSSession) tryReserveCurrent(state *responsesWSCallState) bool {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.current != nil {
		return false
	}
	s.current = state
	return true
}

func (s *responsesWSSession) hasCurrent() bool {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.current != nil
}

func (s *responsesWSSession) clearCurrent(state *responsesWSCallState) bool {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if state != nil && s.current != state {
		return false
	}
	s.current = nil
	return true
}

func (s *responsesWSSession) getCurrent() *responsesWSCallState {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.current
}

// settleCurrent ends an in-flight call that was interrupted rather than
// completed — client disconnect, upstream read failure, idle timeout or channel
// shutdown. The payload already reached upstream, so it settles on observed
// usage; finishCall still refunds if nothing was generated.
func (s *responsesWSSession) settleCurrent() {
	state := s.getCurrent()
	if state != nil {
		s.finishCall(state, responsesWSCallSettled)
	}
}

func acquireResponsesWSSlot(userId int) bool {
	if responsesWSMaxPerUser <= 0 || userId == 0 {
		return true
	}
	responsesWSCountMu.Lock()
	defer responsesWSCountMu.Unlock()
	if responsesWSCounts[userId] >= responsesWSMaxPerUser {
		return false
	}
	responsesWSCounts[userId]++
	return true
}

func releaseResponsesWSSlot(userId int) {
	if responsesWSMaxPerUser <= 0 || userId == 0 {
		return
	}
	responsesWSCountMu.Lock()
	defer responsesWSCountMu.Unlock()
	if responsesWSCounts[userId] <= 1 {
		delete(responsesWSCounts, userId)
		return
	}
	responsesWSCounts[userId]--
}

func (s *responsesWSSession) writeClient(messageType int, message []byte) error {
	s.clientWriteMu.Lock()
	defer s.clientWriteMu.Unlock()
	if err := s.client.SetWriteDeadline(time.Now().Add(responsesWSWriteTimeout)); err != nil {
		return err
	}
	return s.client.WriteMessage(messageType, message)
}

func (s *responsesWSSession) hasTarget() bool {
	s.targetMu.Lock()
	defer s.targetMu.Unlock()
	return s.target != nil
}

func (s *responsesWSSession) getTarget() *websocket.Conn {
	s.targetMu.Lock()
	defer s.targetMu.Unlock()
	return s.target
}

func (s *responsesWSSession) isTarget(target *websocket.Conn) bool {
	s.targetMu.Lock()
	defer s.targetMu.Unlock()
	return target != nil && s.target == target
}

func (s *responsesWSSession) setTarget(target *websocket.Conn) {
	s.targetMu.Lock()
	defer s.targetMu.Unlock()
	s.target = target
}

func (s *responsesWSSession) writeTarget(messageType int, message []byte) error {
	// Resolve the target under targetMu, then release it before writing: a slow
	// upstream must not be able to block closeTarget or the idle/policy paths.
	target := s.getTarget()
	if target == nil {
		return errors.New("responses websocket upstream is not connected")
	}
	s.targetWriteMu.Lock()
	defer s.targetWriteMu.Unlock()
	if err := target.SetWriteDeadline(time.Now().Add(responsesWSWriteTimeout)); err != nil {
		return err
	}
	return target.WriteMessage(messageType, message)
}

func (s *responsesWSSession) sendError(eventID string, apiErr *types.NewAPIError) {
	if apiErr == nil {
		return
	}
	payload, err := buildResponsesWSErrorPayload(eventID, apiErr)
	if err != nil {
		return
	}
	_ = s.writeClient(websocket.TextMessage, payload)
}

func buildResponsesWSErrorPayload(eventID string, apiErr *types.NewAPIError) ([]byte, error) {
	if apiErr == nil {
		return nil, errors.New("api error is nil")
	}
	status := apiErr.StatusCode
	if status == 0 {
		status = http.StatusInternalServerError
	}
	openaiErr := apiErr.ToOpenAIError()
	return common.Marshal(&responsesWSErrorEvent{
		Type:    "error",
		Status:  status,
		EventID: eventID,
		Error:   &openaiErr,
	})
}

func (s *responsesWSSession) closeTarget() {
	var target *websocket.Conn
	var unregister func()
	s.targetMu.Lock()
	target = s.target
	s.target = nil
	unregister = s.unregister
	s.unregister = nil
	s.targetMu.Unlock()
	if unregister != nil {
		unregister()
	}
	if target != nil {
		_ = target.Close()
	}
}

func (s *responsesWSSession) registerChannelClose(channelID int) {
	unregister := wsmanager.Register(channelID, wsmanager.KindResponses, func(reason string) {
		s.closeForPolicy(reason)
	})
	s.targetMu.Lock()
	if s.unregister != nil {
		s.unregister()
	}
	s.unregister = unregister
	s.targetMu.Unlock()
}

func (s *responsesWSSession) closeForPolicy(reason string) {
	s.closeWithCode(websocket.ClosePolicyViolation, reason)
}

func (s *responsesWSSession) closeForIdleTimeout() {
	s.closeWithCode(websocket.CloseGoingAway, relaycommon.WebSocketIdleCloseReason)
}

func (s *responsesWSSession) closeWithCode(code int, reason string) {
	s.closeOnce.Do(func() {
		s.settleCurrent()
		deadline := time.Now().Add(time.Second)
		closeMessage := websocket.FormatCloseMessage(code, reason)
		_ = s.client.WriteControl(websocket.CloseMessage, closeMessage, deadline)
		if target := s.getTarget(); target != nil {
			_ = target.WriteControl(websocket.CloseMessage, closeMessage, deadline)
		}
		s.closeTarget()
		_ = s.client.Close()
	})
}

func checkResponsesWSModelAccess(c *gin.Context, modelName string) *types.NewAPIError {
	if !common.GetContextKeyBool(c, appconstant.ContextKeyTokenModelLimitEnabled) {
		return nil
	}
	raw, ok := common.GetContextKey(c, appconstant.ContextKeyTokenModelLimit)
	if !ok {
		return types.NewErrorWithStatusCode(errors.New("token has no model access"), types.ErrorCodeAccessDenied, http.StatusForbidden, types.ErrOptionWithSkipRetry())
	}
	tokenModelLimit, ok := raw.(map[string]bool)
	if !ok {
		tokenModelLimit = map[string]bool{}
	}
	matchName := ratio_setting.FormatMatchingModelName(modelName)
	if _, ok := tokenModelLimit[matchName]; !ok {
		return types.NewErrorWithStatusCode(fmt.Errorf("token is not allowed to use model %s", modelName), types.ErrorCodeAccessDenied, http.StatusForbidden, types.ErrOptionWithSkipRetry())
	}
	return nil
}

func selectResponsesWSChannel(c *gin.Context, modelName string, retryParam *service.RetryParam) (*appmodel.Channel, *types.NewAPIError) {
	if channelIdRaw, ok := common.GetContextKey(c, appconstant.ContextKeyTokenSpecificChannelId); ok {
		channelID, ok := channelIdRaw.(string)
		if !ok {
			return nil, types.NewErrorWithStatusCode(errors.New("invalid specified channel id"), types.ErrorCodeGetChannelFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		id, err := strconv.Atoi(channelID)
		if err != nil {
			return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeGetChannelFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		channel, err := appmodel.GetChannelById(id, true)
		if err != nil {
			return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeGetChannelFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		if channel.Status != common.ChannelStatusEnabled {
			return nil, types.NewErrorWithStatusCode(errors.New("specified channel is disabled"), types.ErrorCodeGetChannelFailed, http.StatusForbidden, types.ErrOptionWithSkipRetry())
		}
		if err := middleware.SetupContextForSelectedChannel(c, channel, modelName); err != nil {
			return nil, err
		}
		return channel, nil
	}

	usingGroup := common.GetContextKeyString(c, appconstant.ContextKeyUsingGroup)
	if usingGroup == "" {
		usingGroup = retryParam.TokenGroup
	}

	if retryParam.GetRetry() == 0 {
		if preferredChannelID, found := service.GetPreferredChannelByAffinity(c, modelName, usingGroup); found {
			preferred, err := appmodel.CacheGetChannel(preferredChannelID)
			if err == nil && preferred != nil && preferred.Status == common.ChannelStatusEnabled {
				if usingGroup == "auto" {
					userGroup := common.GetContextKeyString(c, appconstant.ContextKeyUserGroup)
					for _, g := range service.GetUserAutoGroup(userGroup) {
						if appmodel.IsChannelEnabledForGroupModel(g, modelName, preferred.Id) {
							common.SetContextKey(c, appconstant.ContextKeyAutoGroup, g)
							service.MarkChannelAffinityUsed(c, g, preferred.Id)
							if err := middleware.SetupContextForSelectedChannel(c, preferred, modelName); err != nil {
								return nil, err
							}
							return preferred, nil
						}
					}
				} else if appmodel.IsChannelEnabledForGroupModel(usingGroup, modelName, preferred.Id) {
					service.MarkChannelAffinityUsed(c, usingGroup, preferred.Id)
					if err := middleware.SetupContextForSelectedChannel(c, preferred, modelName); err != nil {
						return nil, err
					}
					return preferred, nil
				}
			}
		}
	}

	channel, selectGroup, err := service.CacheGetRandomSatisfiedChannel(retryParam)
	if err != nil {
		return nil, types.NewError(fmt.Errorf("获取分组 %s 下模型 %s 的可用渠道失败（retry）: %s", selectGroup, modelName, err.Error()), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	if channel == nil {
		return nil, types.NewError(fmt.Errorf("分组 %s 下模型 %s 的可用渠道不存在（retry）", selectGroup, modelName), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	if err := middleware.SetupContextForSelectedChannel(c, channel, modelName); err != nil {
		return nil, err
	}
	return channel, nil
}

func addResponsesWSUsedChannel(c *gin.Context, channelId int) {
	useChannel := c.GetStringSlice("use_channel")
	useChannel = append(useChannel, fmt.Sprintf("%d", channelId))
	c.Set("use_channel", useChannel)
}
