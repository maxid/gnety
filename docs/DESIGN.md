# gnety design notes

## Why a separate library

Applications often need custom device protocols over TCP and UDP (vendor
binary protocols, telemetry devices, UDP datagrams) with framing, sessions and
request correlation. Go's ecosystem offers:

- `net` — correct, simple, goroutine-per-connection; no framing/session model;
- `gnet` — fast event loop; no framing/session/handler model;
- `go-netty` — Netty-inspired pipeline; smaller community/maintenance than gnet.

`gnety` targets a **Netty-class developer experience**: composable pipeline,
transport + framing + session + lifecycle, and reusable protocol codecs. It
does not clone the API, but it aims to be as easy to extend.

Boundary: gnety owns **wire formats** (frame codecs and protocol codec
subpackages); protocol libraries own **state machines** and business semantics.

## Components

```
Server
 ├── transport: net.Listener (TCP) / net.PacketConn (UDP)
 ├── Codec (TCP) / DatagramCodec (UDP)
 ├── Session (ID, RemoteAddr, attrs, Send/Close, Touch)
 └── Handler (OnOpen / OnMessage / OnClose)
```

- TCP: one goroutine per connection; `bufio.Reader` + codec; read deadline =
  `IdleTimeout`; `Session.Send` encodes under a write mutex.
- Pipeline: `Middleware` + `Chain` wrap handlers (outermost first); `Recover`
  converts panics into session close + log.
- Codec kit: frame codecs today; protocol codecs (HTTP/1.1, WebSocket, MQTT,
  CoAP) are planned as subpackages with wire-format-only APIs.
- UDP: one read loop; session keyed by remote address; `Session.Send` writes a
  datagram; optional idle cleanup.
- Stop: closes listener/packet conn and sessions, waits with context timeout.

## Non-goals / deliberate limits

- No annotation-based dispatch, no reflection pipelines.
- No MQTT/HTTP; those have dedicated libraries.
- No business semantics (device auth, TSL, presence).
- No built-in TLS/WebSocket yet (roadmap; `tls.Listener` can be added behind an
  option without breaking the API).

## Roadmap alignment

1. v0.2 (done): request/response correlation, heartbeat, advanced framing/BCD.
2. Next: pipeline polish, TLS/WebSocket/DTLS, client connector.
3. v0.3: protocol codec kit (HTTP/1.1, WebSocket, MQTT, CoAP) + metrics hooks.
4. v0.4: optional gnet backend, benchmarks, GBK/GB18030 helpers.
5. v1.0: API stability, conformance and soak tests.
