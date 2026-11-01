[English](README.md) | 简体中文

# codec/http

WebSocket upgrade 路径使用的 HTTP/1.1 线格式辅助。

- 通用 HTTP server/client 仍由 `net/http` 负责；gnety 不实现 HTTP 框架。
- 目前唯一消费者是 `codec/http/websocket`，它拥有 RFC 6455 handshake/frame/message/`net.Conn` 适配。
- HTTP Upgrade 请求/响应校验（method、version、token、key）由 WebSocket 测试覆盖。

WebSocket codec 独立于 transport/server/session；见 [`websocket/README.md`](websocket/README.zh-CN.md)。
