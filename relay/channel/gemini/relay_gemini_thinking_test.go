package gemini

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newGeminiThinkingTestInfo(model string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		OriginModelName: model,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl:    "https://example.test",
			UpstreamModelName: model,
		},
	}
}

func newGeminiThinkingTestContext() *gin.Context {
	return gin.CreateTestContextOnly(httptest.NewRecorder(), gin.New())
}

func TestResolveClaudeThinkingConfig(t *testing.T) {
	budget := 1024
	cases := []struct {
		name            string
		request         dto.ClaudeRequest
		wantConfig      bool
		wantThoughts    *bool
		wantBudget      *int
		wantLevel       *string
		wantTraceEffort string
	}{
		{
			name: "manual budget",
			request: dto.ClaudeRequest{Thinking: &dto.Thinking{
				Type: "enabled", BudgetTokens: &budget,
			}},
			wantConfig:   true,
			wantThoughts: common.GetPointer(true),
			wantBudget:   &budget,
		},
		{
			name: "effort wins over budget",
			request: dto.ClaudeRequest{
				Thinking:     &dto.Thinking{Type: "enabled", BudgetTokens: &budget},
				OutputConfig: []byte(`{"effort":"xhigh"}`),
			},
			wantConfig:      true,
			wantThoughts:    common.GetPointer(true),
			wantLevel:       common.GetPointer("xhigh"),
			wantTraceEffort: "xhigh",
		},
		{
			name: "disabled wins over effort",
			request: dto.ClaudeRequest{
				Thinking:     &dto.Thinking{Type: "disabled"},
				OutputConfig: []byte(`{"effort":"high"}`),
			},
			wantConfig:   true,
			wantThoughts: common.GetPointer(false),
		},
		{
			name:         "adaptive enables visible thoughts without a level",
			request:      dto.ClaudeRequest{Thinking: &dto.Thinking{Type: "adaptive"}},
			wantConfig:   true,
			wantThoughts: common.GetPointer(true),
		},
		{
			name:       "no client intent",
			request:    dto.ClaudeRequest{},
			wantConfig: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config, traceEffort := resolveClaudeThinkingConfig(&tc.request)
			if !tc.wantConfig {
				require.Nil(t, config)
				require.Empty(t, traceEffort)
				return
			}

			require.NotNil(t, config)
			require.NotNil(t, config.IncludeThoughts)
			require.Equal(t, *tc.wantThoughts, *config.IncludeThoughts)
			if tc.wantBudget == nil {
				require.Nil(t, config.ThinkingBudget)
			} else {
				require.Equal(t, *tc.wantBudget, *config.ThinkingBudget)
			}
			if tc.wantLevel == nil {
				require.Nil(t, config.ThinkingLevel)
			} else {
				require.Equal(t, *tc.wantLevel, *config.ThinkingLevel)
			}
			require.Equal(t, tc.wantTraceEffort, traceEffort)
		})
	}
}

func TestConvertClaudeRequestAppliesThinkingIntentWithTools(t *testing.T) {
	budget := 1024
	request := &dto.ClaudeRequest{
		Model: "gemini-3.7-flash-high",
		Messages: []dto.ClaudeMessage{{
			Role:    "user",
			Content: "Look up the weather in Tokyo.",
		}},
		Tools: []dto.Tool{{
			Name:        "lookup_weather",
			InputSchema: map[string]any{"type": "object"},
		}},
		Thinking:     &dto.Thinking{Type: "enabled", BudgetTokens: &budget},
		OutputConfig: []byte(`{"effort":"high"}`),
	}
	info := newGeminiThinkingTestInfo("gemini-3.7-flash-high")

	converted, err := (&Adaptor{}).ConvertClaudeRequest(newGeminiThinkingTestContext(), info, request)
	require.NoError(t, err)
	geminiRequest, ok := converted.(*dto.GeminiChatRequest)
	require.True(t, ok)
	require.NotNil(t, geminiRequest.GenerationConfig.ThinkingConfig)
	require.NotNil(t, geminiRequest.GenerationConfig.ThinkingConfig.IncludeThoughts)
	assert.True(t, *geminiRequest.GenerationConfig.ThinkingConfig.IncludeThoughts)
	require.NotNil(t, geminiRequest.GenerationConfig.ThinkingConfig.ThinkingLevel)
	assert.Equal(t, "high", *geminiRequest.GenerationConfig.ThinkingConfig.ThinkingLevel)
	assert.Nil(t, geminiRequest.GenerationConfig.ThinkingConfig.ThinkingBudget)
	assert.Equal(t, "high", info.ReasoningEffort)
	require.NotEmpty(t, geminiRequest.Tools)
}

func TestCovertOpenAI2GeminiResolvesThinkingIntent(t *testing.T) {
	for _, level := range []string{"low", "xhigh", "max"} {
		t.Run("reasoning effort "+level, func(t *testing.T) {
			info := newGeminiThinkingTestInfo("gemini-3.7-flash-high")
			request := dto.GeneralOpenAIRequest{ReasoningEffort: level}

			converted, err := CovertOpenAI2Gemini(newGeminiThinkingTestContext(), request, info)
			require.NoError(t, err)
			require.NotNil(t, converted.GenerationConfig.ThinkingConfig)
			require.NotNil(t, converted.GenerationConfig.ThinkingConfig.IncludeThoughts)
			require.True(t, *converted.GenerationConfig.ThinkingConfig.IncludeThoughts)
			require.NotNil(t, converted.GenerationConfig.ThinkingConfig.ThinkingLevel)
			require.Equal(t, level, *converted.GenerationConfig.ThinkingConfig.ThinkingLevel)
			require.Nil(t, converted.GenerationConfig.ThinkingConfig.ThinkingBudget)
			require.Equal(t, level, info.ReasoningEffort)
		})
	}

	t.Run("explicit google config wins and preserves false", func(t *testing.T) {
		extraBody, err := common.Marshal(map[string]any{
			"google": map[string]any{
				"thinking_config": map[string]any{
					"thinking_level":   "low",
					"include_thoughts": false,
				},
			},
		})
		require.NoError(t, err)

		converted, err := CovertOpenAI2Gemini(
			newGeminiThinkingTestContext(),
			dto.GeneralOpenAIRequest{ReasoningEffort: "max", ExtraBody: extraBody},
			newGeminiThinkingTestInfo("gemini-3.7-flash-high"),
		)
		require.NoError(t, err)
		config := converted.GenerationConfig.ThinkingConfig
		require.NotNil(t, config)
		require.NotNil(t, config.IncludeThoughts)
		assert.False(t, *config.IncludeThoughts)
		require.NotNil(t, config.ThinkingLevel)
		assert.Equal(t, "low", *config.ThinkingLevel)
		assert.Nil(t, config.ThinkingBudget)
	})

	t.Run("unrelated google body does not suppress reasoning effort", func(t *testing.T) {
		extraBody, err := common.Marshal(map[string]any{
			"google": map[string]any{
				"image_config": map[string]any{"aspect_ratio": "1:1"},
			},
		})
		require.NoError(t, err)

		converted, err := CovertOpenAI2Gemini(
			newGeminiThinkingTestContext(),
			dto.GeneralOpenAIRequest{ReasoningEffort: "high", ExtraBody: extraBody},
			newGeminiThinkingTestInfo("gemini-3.7-flash-high"),
		)
		require.NoError(t, err)
		require.NotNil(t, converted.GenerationConfig.ThinkingConfig)
		require.NotNil(t, converted.GenerationConfig.ThinkingConfig.ThinkingLevel)
		assert.Equal(t, "high", *converted.GenerationConfig.ThinkingConfig.ThinkingLevel)
	})

	for _, tc := range []struct {
		name    string
		config  map[string]any
		message string
	}{
		{
			name:    "level and budget conflict",
			config:  map[string]any{"thinking_level": "high", "thinking_budget": 1024},
			message: "cannot contain both",
		},
		{
			name:    "decimal budget",
			config:  map[string]any{"thinking_budget": 12.5},
			message: "must be an integer",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			extraBody, err := common.Marshal(map[string]any{
				"google": map[string]any{"thinking_config": tc.config},
			})
			require.NoError(t, err)

			_, err = CovertOpenAI2Gemini(
				newGeminiThinkingTestContext(),
				dto.GeneralOpenAIRequest{ExtraBody: extraBody},
				newGeminiThinkingTestInfo("gemini-3.7-flash-high"),
			)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.message)
		})
	}
}

func TestNativeGeminiModelDoesNotUseLegacySuffixAdapter(t *testing.T) {
	settings := model_setting.GetGeminiSettings()
	previousAdapterState := settings.ThinkingAdapterEnabled
	settings.ThinkingAdapterEnabled = true
	t.Cleanup(func() {
		settings.ThinkingAdapterEnabled = previousAdapterState
	})

	info := newGeminiThinkingTestInfo("gemini-3.7-flash-high")
	require.False(t, shouldApplyGeminiThinkingAdapter(info))

	converted, err := CovertOpenAI2Gemini(newGeminiThinkingTestContext(), dto.GeneralOpenAIRequest{}, info)
	require.NoError(t, err)
	assert.Nil(t, converted.GenerationConfig.ThinkingConfig)

	url, err := (&Adaptor{}).GetRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, "https://example.test/v1beta/models/gemini-3.7-flash-high:generateContent", url)
	assert.Equal(t, "gemini-3.7-flash-high", info.UpstreamModelName)
}

func TestCovertOpenAI2GeminiDoesNotInjectThinkingWithoutIntent(t *testing.T) {
	info := newGeminiThinkingTestInfo("gemini-3.7-flash-high")
	request := dto.GeneralOpenAIRequest{
		Tools: []dto.ToolCallRequest{{
			Type: "function",
			Function: dto.FunctionRequest{
				Name:       "lookup_weather",
				Parameters: map[string]any{"type": "object"},
			},
		}},
	}

	converted, err := CovertOpenAI2Gemini(newGeminiThinkingTestContext(), request, info)
	require.NoError(t, err)
	assert.Nil(t, converted.GenerationConfig.ThinkingConfig)
}

func TestStreamResponseGeminiChat2OpenAISeparatesThoughtTextAndTool(t *testing.T) {
	response, _ := streamResponseGeminiChat2OpenAI(&dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{
			Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{
				{Thought: true, Text: "consider the forecast"},
				{Text: "I will check it."},
				{FunctionCall: &dto.FunctionCall{
					FunctionName: "lookup_weather",
					Arguments:    map[string]any{"city": "Tokyo"},
				}},
			}},
		}},
	})

	require.Len(t, response.Choices, 1)
	choice := response.Choices[0]
	assert.Equal(t, "consider the forecast", choice.Delta.GetReasoningContent())
	assert.Equal(t, "I will check it.", choice.Delta.GetContentString())
	require.Len(t, choice.Delta.ToolCalls, 1)
	assert.Equal(t, "lookup_weather", choice.Delta.ToolCalls[0].Function.Name)
	require.NotNil(t, choice.FinishReason)
	assert.Equal(t, "tool_calls", *choice.FinishReason)
	assert.False(t, strings.Contains(choice.Delta.GetReasoningContent(), "I will check it."))
}

func TestResponseGeminiChat2OpenAISeparatesThoughtTextAndTool(t *testing.T) {
	response := responseGeminiChat2OpenAI(newGeminiThinkingTestContext(), &dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{
			Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{
				{Thought: true, Text: "consider the forecast"},
				{Text: "I will check it."},
				{FunctionCall: &dto.FunctionCall{
					FunctionName: "lookup_weather",
					Arguments:    map[string]any{"city": "Tokyo"},
				}},
			}},
		}},
	})

	require.Len(t, response.Choices, 1)
	choice := response.Choices[0]
	assert.Equal(t, "consider the forecast", choice.Message.GetReasoningContent())
	assert.Equal(t, "I will check it.", choice.Message.StringContent())
	toolCalls := choice.Message.ParseToolCalls()
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "lookup_weather", toolCalls[0].Function.Name)
	assert.Equal(t, "tool_calls", choice.FinishReason)
}
