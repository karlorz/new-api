package dto

import (
	"testing"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResponsesTokenCountMetaIncludesFunctionCallOutput guards pre-consume
// sizing for tool-result turns. A Responses turn can consist of nothing but a
// function_call_output item; counting only content estimated such a turn at
// zero tokens and under-reserved quota.
func TestResponsesTokenCountMetaIncludesFunctionCallOutput(t *testing.T) {
	toolResult := "the weather in Shanghai is 31C and humid"

	var request OpenAIResponsesRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"model": "gpt-4o",
		"input": [
			{"type": "function_call_output", "call_id": "call_1", "output": "`+toolResult+`"}
		]
	}`), &request))

	meta := request.GetTokenCountMeta()
	require.NotNil(t, meta)
	assert.Contains(t, meta.CombineText, toolResult, "function_call_output must reach token counting")
}

func TestResponsesTokenCountMetaIncludesStructuredFunctionCallOutput(t *testing.T) {
	var request OpenAIResponsesRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"model": "gpt-4o",
		"input": [
			{"type": "function_call_output", "call_id": "call_1", "output": {"temperature": "31C", "city": "Shanghai"}}
		]
	}`), &request))

	meta := request.GetTokenCountMeta()
	require.NotNil(t, meta)
	assert.Contains(t, meta.CombineText, "Shanghai", "structured tool output must reach token counting")
}

func TestResponsesTokenCountMetaStillCountsPlainContent(t *testing.T) {
	var request OpenAIResponsesRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"model": "gpt-4o",
		"input": [
			{"type": "message", "role": "user", "content": "hello there"}
		]
	}`), &request))

	meta := request.GetTokenCountMeta()
	require.NotNil(t, meta)
	assert.Contains(t, meta.CombineText, "hello there")
}
