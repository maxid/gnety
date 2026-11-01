[English](README.md) | 简体中文

# codec/http/websocket

仅标准库的 RFC 6455 WebSocket codec，按典型 HTTP codec 栈分层。

- `handshake.go`：HTTP Upgrade、v13 Key/Accept、精确 subprotocol、Origin 策略。
- `frame.go`：frame header/length/masking/opcode/control frame 线格式。
- `message.go`（在 `websocket.go` 内）：fragmentation、UTF-8 校验、Ping/Pong/Close、read limit。
- `conn.go`（在 `websocket.go` 内）：`net.Conn` 适配，提供 `Ping`、`CloseWithStatus`、`SetReadLimit`、`Subprotocol`。

`Accept` 使用传入的 writer：默认 `w` 必须实现 `http.Hijacker`。框架包装 writer 时，调用方提供 `Options.Hijack`（例如 `http.NewResponseController(w).Hijack`），而不是让库穿透包装。

公开 API：

```go
opts := websocket.Options{Subprotocol: "mqtt", OriginPatterns: []string{"example.com"}}
conn, err := websocket.Accept(w, r, opts)          // server
conn, err = websocket.ClientHandshake(raw, u, origin, opts) // TCP/TLS dial 之后
```

它不 import gnety server/session/transport，也不含 MQTT/CoAP/LwM2M 状态。
