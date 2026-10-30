# codec/http

HTTP/1.1 wire-format helpers for the WebSocket upgrade path.

- General HTTP server/client stays in `net/http`; gnety does not implement an HTTP framework.
- The only implemented consumer today is `codec/http/websocket`, which owns RFC 6455 handshake/frame/message/`net.Conn` adaptation.
- HTTP Upgrade request/response validation (method, version, tokens, key) is exercised by the WebSocket tests.

The WebSocket codec is independent of transport/server/session; see
[`websocket/README.md`](websocket/README.md).
