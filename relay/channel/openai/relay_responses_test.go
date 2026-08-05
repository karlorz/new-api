package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

func TestOaiResponsesStreamHandlerDropsKeepaliveAndContinues(t *testing.T) {
	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() {
		constant.StreamingTimeout = oldStreamingTimeout
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	info := &relaycommon.RelayInfo{
		DisablePing: true,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "test-model"},
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"response.created","response":{"id":"resp-1","model":"test-model"}}`,
			`data: {"type":"keepalive","sequence_number":1}`,
			`data: {"type":"response.output_text.delta","delta":"hello"}`,
			`data: {"type":"response.completed","response":{"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}}`,
			"data: [DONE]",
		}, "\n\n"))),
	}

	usage, apiErr := OaiResponsesStreamHandler(c, info, resp)
	if apiErr != nil {
		t.Fatalf("OaiResponsesStreamHandler() error = %v", apiErr)
	}
	if usage == nil {
		t.Fatal("OaiResponsesStreamHandler() returned nil usage")
	}
	if usage.InputTokens != 2 || usage.OutputTokens != 1 || usage.TotalTokens != 3 {
		t.Fatalf("usage = %+v, want input=2 output=1 total=3", usage)
	}

	body := recorder.Body.String()
	t.Logf("forwarded SSE: %q", body)
	if strings.Contains(body, "keepalive") || strings.Contains(body, "sequence_number") {
		t.Fatalf("transport keepalive leaked into downstream SSE: %q", body)
	}
	for _, event := range []string{
		"event: response.created",
		"event: response.output_text.delta",
		"event: response.completed",
	} {
		if !strings.Contains(body, event) {
			t.Fatalf("downstream SSE is missing %q: %q", event, body)
		}
	}
	if strings.Index(body, "event: response.created") >= strings.Index(body, "event: response.output_text.delta") ||
		strings.Index(body, "event: response.output_text.delta") >= strings.Index(body, "event: response.completed") {
		t.Fatalf("semantic event order was not preserved: %q", body)
	}
	if info.StreamStatus == nil || info.StreamStatus.EndReason != relaycommon.StreamEndReasonDone {
		t.Fatalf("stream status = %+v, want done", info.StreamStatus)
	}
}
