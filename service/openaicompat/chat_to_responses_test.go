package openaicompat

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestChatCompletionsRequestToResponsesRequestPreservesServiceTier(t *testing.T) {
	request, err := ChatCompletionsRequestToResponsesRequest(&dto.GeneralOpenAIRequest{
		Model:       "gpt-5.6",
		ServiceTier: []byte(`"fast"`),
	})
	require.NoError(t, err)
	require.Equal(t, "fast", request.ServiceTier)
}
