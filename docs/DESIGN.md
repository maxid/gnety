# gnety design notes

## Why a separate library

XDMP needs to support custom device protocols over TCP and UDP (ETing, JT808-like,
vendor TCP, UDP datagram devices). The Java platform has `xdmp-netty` +
`xdmp-netty-protocol` with length-field/delimiter framing, sessions, request
correlation and annotation dispatch. Go's ecosystem offers:

- `net` — correct, simple, goroutine-per-connection; no framing/session model;
- `gnet` — fast event loop; no framing/session/handler model;
- `go-netty` — Netty-inspired pipeline; smaller community/maintenance than gnet.

`gnety` provides the missing middle layer: **transport + framing + session +
lifecycle**, with a stdlib backend now and an optional `gnet` backend later.
It does not try to clone Netty's API.

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
- UDP: one read loop; session keyed by remote address; `Session.Send` writes a
  datagram; optional idle cleanup.
- Stop: closes listener/packet conn and sessions, waits with context timeout.

## Non-goals / deliberate limits

- No annotation-based dispatch, no reflection pipelines.
- No MQTT/HTTP; those have dedicated libraries.
- No business semantics (device auth, TSL, presence).
- No built-in TLS/WebSocket yet (roadmap; `tls.Listener` can be added behind an
  option without breaking the API).

## Roadmap alignment with Java xdmp-netty

1. v0.2: request/reply correlation (`RequestKeyPolicy` equivalent), heartbeat
   API, metrics hooks, TLS.
2. v0.3: optional gnet backend (build tag or engine option), benchmark vs
   stdlib/gnet.
3. v0.4: advanced framing (magic number, dynamic length field, endianness,
   BCD/GBK helpers), client (connector) and reconnection.
4. v1.0: API stability + conformance/soak tests with the XDMP hub.
