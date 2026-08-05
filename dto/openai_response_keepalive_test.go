package dto

import "testing"

func TestIsResponsesTransportEventType(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		eventType string
		want      bool
	}{
		{name: "keepalive", eventType: "keepalive", want: true},
		{name: "keep_alive", eventType: "keep_alive", want: true},
		{name: "heartbeat", eventType: "heartbeat", want: true},
		{name: "case insensitive", eventType: "KeepAlive", want: true},
		{name: "trimmed", eventType: "  keepalive  ", want: true},
		{name: "response created", eventType: "response.created", want: false},
		{name: "response completed", eventType: "response.completed", want: false},
		{name: "error", eventType: "error", want: false},
		{name: "empty", eventType: "", want: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsResponsesTransportEventType(tc.eventType); got != tc.want {
				t.Fatalf("IsResponsesTransportEventType(%q) = %v, want %v", tc.eventType, got, tc.want)
			}
		})
	}
}
