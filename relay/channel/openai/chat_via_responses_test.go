package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func TestOaiResponsesToChatStreamHandlerDropsKeepaliveAndContinues(t *testing.T) {
	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() {
		constant.StreamingTimeout = oldStreamingTimeout
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		DisablePing:        true,
		RelayFormat:        types.RelayFormatOpenAI,
		ChannelMeta:        &relaycommon.ChannelMeta{UpstreamModelName: "test-model"},
		ShouldIncludeUsage: false,
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

	usage, apiErr := OaiResponsesToChatStreamHandler(c, info, resp)
	if apiErr != nil {
		t.Fatalf("OaiResponsesToChatStreamHandler() error = %v", apiErr)
	}
	if usage == nil {
		t.Fatal("OaiResponsesToChatStreamHandler() returned nil usage")
	}
	if usage.InputTokens != 2 || usage.OutputTokens != 1 || usage.TotalTokens != 3 {
		t.Fatalf("usage = %+v, want input=2 output=1 total=3", usage)
	}

	body := recorder.Body.String()
	t.Logf("forwarded Chat Completions SSE: %q", body)
	if strings.Contains(body, "keepalive") || strings.Contains(body, "sequence_number") {
		t.Fatalf("transport keepalive leaked into Chat Completions SSE: %q", body)
	}
	if !strings.Contains(body, "hello") {
		t.Fatalf("converted stream is missing output text: %q", body)
	}
	if !strings.Contains(body, `"finish_reason":"stop"`) {
		t.Fatalf("converted stream is missing terminal stop chunk: %q", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("converted stream is missing SSE termination: %q", body)
	}
}
