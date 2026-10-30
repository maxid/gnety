package gnety

import ws "github.com/maxid/gnety/codec/http/websocket"

// WebSocketConn is the public WebSocket connection adapter (re-exported from
// codec/http/websocket for source compatibility).
type WebSocketConn = ws.Conn

// WebSocketStatus is a WebSocket close status code.
type WebSocketStatus = ws.StatusCode

// WebSocketCloseError reports a peer close status.
type WebSocketCloseError = ws.CloseError

const (
	WebSocketStatusNormalClosure   = ws.StatusCodeNormalClosure
	WebSocketStatusGoingAway       = ws.StatusCodeGoingAway
	WebSocketStatusProtocolError   = ws.StatusCodeProtocolError
	WebSocketStatusUnsupportedData = ws.StatusCodeUnsupportedData
	WebSocketStatusInvalidPayload  = ws.StatusCodeInvalidPayload
	WebSocketStatusPolicyViolation = ws.StatusCodePolicyViolation
	WebSocketStatusMessageTooBig   = ws.StatusCodeMessageTooBig
	WebSocketStatusInternalError   = ws.StatusCodeInternalError
)
