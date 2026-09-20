package common

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/setting/model_setting"

	"github.com/stretchr/testify/require"
)

func TestPeekJSONServiceTier(t *testing.T) {
	require.Equal(t, "fast", PeekJSONServiceTier([]byte(`{"service_tier":"fast","model":"gpt-5"}`)))
	require.Equal(t, "priority", PeekJSONServiceTier([]byte(`{"service_tier":"priority"}`)))
	require.Equal(t, "", PeekJSONServiceTier([]byte(`{"model":"gpt-5"}`)))
	require.Equal(t, "", PeekJSONServiceTier(nil))
	require.Equal(t, "", PeekJSONServiceTier([]byte(`{"service_tier":null}`)))
	require.Equal(t, "", PeekJSONServiceTier([]byte(`{"service_tier":{"mode":"fast"}}`)))
	require.Equal(t, "", PeekJSONServiceTier([]byte(`{"service_tier":1}`)))
}

func TestServiceTierFromRequest(t *testing.T) {
	require.Equal(t, "fast", ServiceTierFromRequest(&dto.OpenAIResponsesRequest{ServiceTier: "fast"}))
	require.Equal(t, "priority", ServiceTierFromRequest(&dto.ClaudeRequest{ServiceTier: "priority"}))
	require.Equal(t, "flex", ServiceTierFromRequest(&dto.GeneralOpenAIRequest{
		ServiceTier: []byte(`"flex"`),
	}))
	require.Equal(t, "", ServiceTierFromRequest(&dto.OpenAIResponsesRequest{}))
	require.Equal(t, "", ServiceTierFromRequest(nil))
}

func TestResolveRequestedServiceTierPrefersOverrideIncludingRemoval(t *testing.T) {
	info := &RelayInfo{
		ParamOverrideServiceTier:        "fast",
		ParamOverrideServiceTierApplied: true,
		Request:                         &dto.OpenAIResponsesRequest{ServiceTier: "default"},
	}
	require.Equal(t, "fast", ResolveRequestedServiceTier(nil, info))
	require.True(t, ServiceTierForwarded(info))

	info.ParamOverrideServiceTier = ""
	require.Equal(t, "", ResolveRequestedServiceTier(nil, info))
	require.False(t, ServiceTierForwarded(info))
}

func TestResolveRequestedServiceTierPrefersRequestThenBillingBody(t *testing.T) {
	info := &RelayInfo{
		Request: &dto.OpenAIResponsesRequest{ServiceTier: "fast"},
		BillingRequestInput: &billingexpr.RequestInput{
			Body: []byte(`{"service_tier":"default"}`),
		},
	}
	require.Equal(t, "fast", ResolveRequestedServiceTier(nil, info))

	info.Request = &dto.OpenAIResponsesRequest{}
	require.Equal(t, "default", ResolveRequestedServiceTier(nil, info))
}

func TestServiceTierForwarded(t *testing.T) {
	require.False(t, ServiceTierForwarded(nil))
	require.False(t, ServiceTierForwarded(&RelayInfo{}))

	blocked := &RelayInfo{ChannelMeta: &ChannelMeta{}}
	require.False(t, ServiceTierForwarded(blocked))

	allowed := &RelayInfo{
		ChannelMeta: &ChannelMeta{
			ChannelOtherSettings: dto.ChannelOtherSettings{AllowServiceTier: true},
		},
	}
	require.True(t, ServiceTierForwarded(allowed))

	passThrough := &RelayInfo{
		ChannelMeta: &ChannelMeta{
			ChannelSetting: dto.ChannelSettings{PassThroughBodyEnabled: true},
		},
	}
	require.True(t, ServiceTierForwarded(passThrough))

	original := model_setting.GetGlobalSettings().PassThroughRequestEnabled
	model_setting.GetGlobalSettings().PassThroughRequestEnabled = true
	t.Cleanup(func() {
		model_setting.GetGlobalSettings().PassThroughRequestEnabled = original
	})
	require.True(t, ServiceTierForwarded(blocked))
}
