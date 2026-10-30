# codec/http/websocket

Standard-library-only RFC 6455 WebSocket codec, layered like typical HTTP codec stacks.

- `handshake.go`: HTTP Upgrade, v13 Key/Accept, exact subprotocol, Origin policy.
- `frame.go`: frame header/length/masking/opcode/control frame wire format.
- `message.go` (inside `websocket.go`): fragmentation, UTF-8 validation, Ping/Pong/Close, read limits.
- `conn.go` (inside `websocket.go`): `net.Conn` adapter with `Ping`, `CloseWithStatus`, `SetReadLimit`, `Subprotocol`.

Public API:

```go
opts := websocket.Options{Subprotocol: "mqtt", OriginPatterns: []string{"example.com"}}
conn, err := websocket.Accept(w, r, opts)          // server
conn, err = websocket.ClientHandshake(raw, u, origin, opts) // after TCP/TLS dial
```

It does not import gnety server/session/transport and contains no MQTT/CoAP/LwM2M state.
