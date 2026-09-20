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
		if info.ParamOverrideServiceTierApplied {
			return info.ParamOverrideServiceTier
		}
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

// ServiceTierForwarded reports whether service_tier is expected to be present
// in the upstream request, including values added by parameter overrides.
func UpdateParamOverrideServiceTier(info *RelayInfo, finalBody []byte) {
	if info == nil || !info.ParamOverrideServiceTierApplied {
		return
	}
	info.ParamOverrideServiceTier = PeekJSONServiceTier(finalBody)
}

func ServiceTierForwarded(info *RelayInfo) bool {
	if info == nil {
		return false
	}
	// Parameter overrides are applied after disabled fields are removed, so a
	// service_tier found in the final body is part of the upstream request.
	if info.ParamOverrideServiceTierApplied {
		return info.ParamOverrideServiceTier != ""
	}
	if model_setting.GetGlobalSettings().PassThroughRequestEnabled {
		return true
	}
	if info.ChannelMeta == nil {
		return false
	}
	return info.ChannelSetting.PassThroughBodyEnabled || info.ChannelOtherSettings.AllowServiceTier
}
