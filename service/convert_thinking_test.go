package service

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newClaudeConversionInfo(sendResponseCount int) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		SendResponseCount: sendResponseCount,
		ClaudeConvertInfo: &relaycommon.ClaudeConvertInfo{
			LastMessagesType: relaycommon.LastMessageTypeNone,
		},
	}
}

func claudeResponseTypes(responses []*dto.ClaudeResponse) []string {
	types := make([]string, 0, len(responses))
	for _, response := range responses {
		types = append(types, response.Type)
	}
	return types
}

func TestStreamResponseOpenAI2ClaudeOrdersThinkingTextAndToolUse(t *testing.T) {
	finishReason := "tool_calls"
	reasoning := "I need current weather."
	content := "Checking Tokyo now."
	info := newClaudeConversionInfo(1)

	responses := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl-test",
		Model: "gemini-3.7-flash-high",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			FinishReason: &finishReason,
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
				ReasoningContent: &reasoning,
				Content:          &content,
				ToolCalls: []dto.ToolCallResponse{{
					ID:   "tool-1",
					Type: "function",
					Function: dto.FunctionResponse{
						Name:      "lookup_weather",
						Arguments: `{"city":"Tokyo"}`,
					},
				}},
			},
		}},
	}, info)

	require.Equal(t, []string{
		"message_start",
		"content_block_start", "content_block_delta", "content_block_stop",
		"content_block_start", "content_block_delta", "content_block_stop",
		"content_block_start", "content_block_delta", "content_block_stop",
		"message_delta", "message_stop",
	}, claudeResponseTypes(responses))

	assert.Equal(t, "thinking", responses[1].ContentBlock.Type)
	assert.Equal(t, "thinking_delta", responses[2].Delta.Type)
	assert.Equal(t, reasoning, *responses[2].Delta.Thinking)
	assert.Equal(t, 0, *responses[1].Index)
	assert.Equal(t, "text", responses[4].ContentBlock.Type)
	assert.Equal(t, content, *responses[5].Delta.Text)
	assert.Equal(t, 1, *responses[4].Index)
	assert.Equal(t, "tool_use", responses[7].ContentBlock.Type)
	assert.Equal(t, "lookup_weather", responses[7].ContentBlock.Name)
	assert.Equal(t, 2, *responses[7].Index)
	assert.Equal(t, "tool_use", *responses[10].Delta.StopReason)
	assert.Nil(t, responses[10].Usage)
	assert.True(t, info.ClaudeConvertInfo.Done)
}

func TestStreamResponseOpenAI2ClaudeClosesWithoutUsageAfterFirstChunk(t *testing.T) {
	finishReason := "stop"
	reasoning := "brief thought"
	info := newClaudeConversionInfo(1)

	responses := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			FinishReason: &finishReason,
			Delta:        dto.ChatCompletionsStreamResponseChoiceDelta{ReasoningContent: &reasoning},
		}},
	}, info)

	require.Equal(t, []string{
		"message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop",
	}, claudeResponseTypes(responses))
	assert.Equal(t, "end_turn", *responses[4].Delta.StopReason)
	assert.Nil(t, responses[4].Usage)
	assert.True(t, info.ClaudeConvertInfo.Done)
}

func TestStreamResponseOpenAI2ClaudeClosesUsageOnlyTerminalChunk(t *testing.T) {
	info := newClaudeConversionInfo(2)
	info.ClaudeConvertInfo.LastMessagesType = relaycommon.LastMessageTypeText
	info.ClaudeConvertInfo.Index = 3
	info.FinishReason = "stop"

	responses := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Usage: &dto.Usage{PromptTokens: 5, CompletionTokens: 7},
	}, info)

	require.Equal(t, []string{"content_block_stop", "message_delta", "message_stop"}, claudeResponseTypes(responses))
	assert.Equal(t, 3, *responses[0].Index)
	require.NotNil(t, responses[1].Usage)
	assert.Equal(t, 5, responses[1].Usage.InputTokens)
	assert.Equal(t, 7, responses[1].Usage.OutputTokens)
	assert.Equal(t, "end_turn", *responses[1].Delta.StopReason)
}

func TestResponseOpenAI2ClaudeIncludesThinkingBeforeText(t *testing.T) {
	reasoning := "reason through it"
	response := ResponseOpenAI2Claude(&dto.OpenAITextResponse{
		Id:    "chatcmpl-test",
		Model: "gemini-3.7-flash-high",
		Choices: []dto.OpenAITextResponseChoice{{
			FinishReason: "stop",
			Message: dto.Message{
				Role:             "assistant",
				Content:          "final answer",
				ReasoningContent: &reasoning,
			},
		}},
	}, &relaycommon.RelayInfo{})

	require.Len(t, response.Content, 2)
	assert.Equal(t, "thinking", response.Content[0].Type)
	assert.Equal(t, reasoning, *response.Content[0].Thinking)
	assert.Equal(t, "text", response.Content[1].Type)
	assert.Equal(t, "final answer", *response.Content[1].Text)
}

func TestResponseOpenAI2ClaudeKeepsThinkingTextAndToolUse(t *testing.T) {
	reasoning := "I need to use the weather tool."
	message := dto.Message{Role: "assistant", ReasoningContent: &reasoning}
	message.SetStringContent("Checking Tokyo now.")
	message.SetToolCalls([]dto.ToolCallResponse{{
		ID:   "tool-1",
		Type: "function",
		Function: dto.FunctionResponse{
			Name:      "lookup_weather",
			Arguments: `{"city":"Tokyo"}`,
		},
	}})

	response := ResponseOpenAI2Claude(&dto.OpenAITextResponse{
		Choices: []dto.OpenAITextResponseChoice{{
			FinishReason: "tool_calls",
			Message:      message,
		}},
	}, &relaycommon.RelayInfo{})

	require.Len(t, response.Content, 3)
	assert.Equal(t, "thinking", response.Content[0].Type)
	assert.Equal(t, reasoning, *response.Content[0].Thinking)
	assert.Equal(t, "text", response.Content[1].Type)
	assert.Equal(t, "Checking Tokyo now.", *response.Content[1].Text)
	assert.Equal(t, "tool_use", response.Content[2].Type)
	assert.Equal(t, "lookup_weather", response.Content[2].Name)
}

func TestStreamResponseOpenAI2ClaudeDoesNotDuplicateTerminalEvents(t *testing.T) {
	finishReason := "stop"
	info := newClaudeConversionInfo(2)

	responses := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{FinishReason: &finishReason}},
	}, info)
	require.Equal(t, []string{"message_delta", "message_stop"}, claudeResponseTypes(responses))

	assert.Empty(t, StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{}, info))
	assert.True(t, info.ClaudeConvertInfo.Done)
}
