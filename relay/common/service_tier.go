package common

import (
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/model_setting"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// PeekJSONServiceTier returns the request service_tier if present.
// OpenAI/Codex send a string such as "fast", "priority", or "default".
func PeekJSONServiceTier(jsonData []byte) string {
	if len(jsonData) == 0 {
		return ""
	}
	result := gjson.GetBytes(jsonData, "service_tier")
	if !result.Exists() || result.Type != gjson.String {
		return ""
	}
	return strings.TrimSpace(result.String())
}

func NormalizeServiceTierRaw(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

func ServiceTierFromRequest(request dto.Request) string {
	if request == nil {
		return ""
	}
	switch req := request.(type) {
	case *dto.OpenAIResponsesRequest:
		return strings.TrimSpace(req.ServiceTier)
	case *dto.ClaudeRequest:
		return strings.TrimSpace(req.ServiceTier)
	case *dto.GeneralOpenAIRequest:
		return NormalizeServiceTierRaw(req.ServiceTier)
	default:
		return ""
	}
}

// ResolveRequestedServiceTier prefers the parsed request object, then billing
// input, then the original HTTP body. Logging uses this so a stripped
// service_tier still appears in usage logs.
func ResolveRequestedServiceTier(ctx *gin.Context, info *RelayInfo) string {
	if info != nil {
		if tier := ServiceTierFromRequest(info.Request); tier != "" {
			return tier
		}
		if info.BillingRequestInput != nil {
			if tier := PeekJSONServiceTier(info.BillingRequestInput.Body); tier != "" {
				return tier
			}
		}
	}
	if ctx == nil || ctx.Request == nil {
		return ""
	}
	storage, err := common.GetBodyStorage(ctx)
	if err != nil || storage == nil {
		return ""
	}
	body, err := storage.Bytes()
	if err != nil {
		return ""
	}
	return PeekJSONServiceTier(body)
}

// ServiceTierForwarded reports whether the channel/global passthrough would
// keep service_tier on the upstream request. It does not inspect the bytes
// actually sent after later param overrides.
func ServiceTierForwarded(info *RelayInfo) bool {
	if info == nil {
		return false
	}
	if model_setting.GetGlobalSettings().PassThroughRequestEnabled {
		return true
	}
	if info.ChannelMeta == nil {
		return false
	}
	return info.ChannelSetting.PassThroughBodyEnabled || info.ChannelOtherSettings.AllowServiceTier
}
