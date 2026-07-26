package common

import (
	"errors"
	"net"
	"time"

	appcommon "github.com/QuantumNous/new-api/common"

	"github.com/gorilla/websocket"
)

const WebSocketIdleCloseReason = "websocket idle timeout"

// WebSocketIdleTimeoutMinutes is the client WebSocket idle timeout in minutes.
// A non-positive value disables the timeout.
var WebSocketIdleTimeoutMinutes = appcommon.GetEnvOrDefault("WEBSOCKET_IDLE_TIMEOUT_MINUTES", 10)

func GetWebSocketIdleTimeout() time.Duration {
	if WebSocketIdleTimeoutMinutes <= 0 {
		return 0
	}
	return time.Duration(WebSocketIdleTimeoutMinutes) * time.Minute
}

// RefreshClientWebSocketReadDeadline counts data messages as activity. Gorilla
// handles Ping/Pong control frames inside ReadMessage, so heartbeats alone do
// not extend this deadline.
func RefreshClientWebSocketReadDeadline(conn *websocket.Conn) error {
	if conn == nil {
		return errors.New("websocket connection is nil")
	}
	timeout := GetWebSocketIdleTimeout()
	if timeout <= 0 {
		return conn.SetReadDeadline(time.Time{})
	}
	return conn.SetReadDeadline(time.Now().Add(timeout))
}

func IsWebSocketIdleTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
