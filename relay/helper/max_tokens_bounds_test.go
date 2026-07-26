package helper

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

// TestMaxTokensBounds guards the billing invariant that user-supplied max
// token fields are bounded on every relay format. These values feed
// pre-consume quota math (preConsumedTokens * ratio); a huge or
// wrapped-negative value (e.g. 18446744073686646784 parsed into *uint) must
// be rejected at validation instead of corrupting the pre-charge.
func TestResponsesMaxTokensBounds(t *testing.T) {
	// The Responses WebSocket relay parses response.create events itself and
	// never goes through GetAndValidateResponsesRequest, so it must reach the
	// same bound through the shared validator.
	t.Run("responses websocket transport shares the bound", func(t *testing.T) {
		huge := uint(18446744073686646784)
		err := ValidateResponsesRequest(&dto.OpenAIResponsesRequest{Model: "gpt-4o", MaxOutputTokens: &huge})
		require.Error(t, err)
		require.Contains(t, err.Error(), "max_output_tokens is invalid")

		normal := uint(8192)
		require.NoError(t, ValidateResponsesRequest(&dto.OpenAIResponsesRequest{Model: "gpt-4o", MaxOutputTokens: &normal}))
	})

	t.Run("responses websocket transport requires model", func(t *testing.T) {
		err := ValidateResponsesRequest(&dto.OpenAIResponsesRequest{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "model is required")
	})
}
