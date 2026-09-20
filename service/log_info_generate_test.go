package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestGenerateTextOtherInfoMarksWebSocketTransport(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	now := time.Now()
	relayInfo := &relaycommon.RelayInfo{
		ClientWs:          &websocket.Conn{},
		StartTime:         now,
		FirstResponseTime: now,
		ChannelMeta:       &relaycommon.ChannelMeta{},
	}

	other := GenerateTextOtherInfo(ctx, relayInfo, 1, 1, 1, 0, 0, 0, 1)

	require.Equal(t, true, other["ws"])
}

func TestGenerateTextOtherInfoOmitsWebSocketTransportForHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	now := time.Now()
	relayInfo := &relaycommon.RelayInfo{
		StartTime:         now,
		FirstResponseTime: now,
		ChannelMeta:       &relaycommon.ChannelMeta{},
	}

	other := GenerateTextOtherInfo(ctx, relayInfo, 1, 1, 1, 0, 0, 0, 1)

	_, ok := other["ws"]
	require.False(t, ok)
}

func TestGenerateTextOtherInfoRecordsServiceTier(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	now := time.Now()
	relayInfo := &relaycommon.RelayInfo{
		StartTime:         now,
		FirstResponseTime: now,
		Request:           &dto.OpenAIResponsesRequest{ServiceTier: "fast"},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelOtherSettings: dto.ChannelOtherSettings{AllowServiceTier: true},
		},
	}

	other := GenerateTextOtherInfo(ctx, relayInfo, 1, 1, 1, 0, 0, 0, 1)

	require.Equal(t, "fast", other["service_tier"])
	_, forwarded := other["service_tier_forwarded"]
	require.False(t, forwarded)
}

func TestGenerateTextOtherInfoRecordsOverriddenServiceTier(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	now := time.Now()
	relayInfo := &relaycommon.RelayInfo{
		StartTime:                       now,
		FirstResponseTime:               now,
		Request:                         &dto.OpenAIResponsesRequest{},
		ChannelMeta:                     &relaycommon.ChannelMeta{},
		ParamOverrideServiceTier:        "fast",
		ParamOverrideServiceTierApplied: true,
	}

	other := GenerateTextOtherInfo(ctx, relayInfo, 1, 1, 1, 0, 0, 0, 1)

	require.Equal(t, "fast", other["service_tier"])
	_, forwarded := other["service_tier_forwarded"]
	require.False(t, forwarded)
}

func TestGenerateTextOtherInfoOmitsRemovedServiceTier(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	now := time.Now()
	relayInfo := &relaycommon.RelayInfo{
		StartTime:                       now,
		FirstResponseTime:               now,
		Request:                         &dto.OpenAIResponsesRequest{ServiceTier: "fast"},
		ChannelMeta:                     &relaycommon.ChannelMeta{ChannelOtherSettings: dto.ChannelOtherSettings{AllowServiceTier: true}},
		ParamOverrideServiceTierApplied: true,
	}

	other := GenerateTextOtherInfo(ctx, relayInfo, 1, 1, 1, 0, 0, 0, 1)

	_, ok := other["service_tier"]
	require.False(t, ok)
}

func TestGenerateTextOtherInfoMarksStrippedServiceTier(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	now := time.Now()
	relayInfo := &relaycommon.RelayInfo{
		StartTime:         now,
		FirstResponseTime: now,
		Request:           &dto.OpenAIResponsesRequest{ServiceTier: "fast"},
		ChannelMeta:       &relaycommon.ChannelMeta{},
	}

	other := GenerateTextOtherInfo(ctx, relayInfo, 1, 1, 1, 0, 0, 0, 1)

	require.Equal(t, "fast", other["service_tier"])
	require.Equal(t, false, other["service_tier_forwarded"])
}

func TestGenerateTextOtherInfoOmitsServiceTierWhenAbsent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	now := time.Now()
	relayInfo := &relaycommon.RelayInfo{
		StartTime:         now,
		FirstResponseTime: now,
		ChannelMeta:       &relaycommon.ChannelMeta{},
	}

	other := GenerateTextOtherInfo(ctx, relayInfo, 1, 1, 1, 0, 0, 0, 1)

	_, ok := other["service_tier"]
	require.False(t, ok)
}
