package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
)

func TestAllowedWebSocketOrigin(t *testing.T) {
	previousServerAddress := system_setting.ServerAddress
	previousTrustedURLs := common.SessionCookieTrustedURLs
	system_setting.ServerAddress = "https://panel.example.com"
	common.SessionCookieTrustedURLs = []string{"https://trusted.example.com"}
	t.Cleanup(func() {
		system_setting.ServerAddress = previousServerAddress
		common.SessionCookieTrustedURLs = previousTrustedURLs
	})

	tests := []struct {
		name    string
		origin  []string
		allowed bool
	}{
		{name: "codex cli without origin", allowed: true},
		{name: "same https host", origin: []string{"https://api.example.com"}, allowed: true},
		{name: "same http host", origin: []string{"http://api.example.com"}, allowed: true},
		{name: "configured server address", origin: []string{"https://panel.example.com"}, allowed: true},
		{name: "trusted browser origin", origin: []string{"https://trusted.example.com"}, allowed: true},
		{name: "untrusted host", origin: []string{"https://evil.example.com"}},
		{name: "trusted suffix attack", origin: []string{"https://trusted.example.com.evil.test"}},
		{name: "null origin", origin: []string{"null"}},
		{name: "origin with path", origin: []string{"https://api.example.com/path"}},
		{name: "comma joined origins", origin: []string{"https://api.example.com, https://evil.example.com"}},
		{name: "multiple origin headers", origin: []string{"https://api.example.com", "https://evil.example.com"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "http://api.example.com/v1/responses", nil)
			request.Host = "api.example.com"
			for _, origin := range test.origin {
				request.Header.Add("Origin", origin)
			}
			assert.Equal(t, test.allowed, isAllowedWebSocketOrigin(request))
		})
	}
}

func TestAllowedWebSocketOriginRejectsNilRequest(t *testing.T) {
	assert.False(t, isAllowedWebSocketOrigin(nil))
}
