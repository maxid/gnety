# gnety

A small **TCP/UDP transport framework for custom device protocols** in Go.
It sits between the standard library / `gnet` (raw transports) and application
protocols: framing codecs, sessions, lifecycle handlers, idle timeouts and
graceful shutdown.

> Status: **v0.2.2** — TCP/UDP servers, codecs, sessions, request/response correlation,
> heartbeats, idle timeout and graceful shutdown.
> License: MIT. Design notes: [docs/DESIGN.md](docs/DESIGN.md).

## Positioning

gnety is a **transport layer**: TCP/UDP (and future TLS/WebSocket/DTLS) listeners
and connectors, framing codecs, sessions, lifecycle, idle/heartbeat and
request/response correlation.

- It contains **no protocol semantics** (no MQTT, CoAP or LwM2M).
- Protocol libraries (MQTT/CoAP/LwM2M implementations) may depend on gnety for transport.
- gnety must never import a protocol library.

## Scope

**In scope**

- custom binary/text protocols over **TCP** (length-field, delimiter, custom codecs)
- **UDP** datagram servers (session per peer)
- session identity/attributes, lifecycle hooks, idle timeout, graceful stop
- request/response correlation via a pluggable `RequestKeyPolicy`
- heartbeat messages and heartbeat/idle timeouts
- configurable length-field framing (magic, offset, size, endianness) and BCD helpers
- a stable base for future TLS and event-loop backends

**Out of scope**

- MQTT — use [gmqx](https://github.com/maxid/gmqx)
- HTTP/WebSocket — use `net/http`
- business semantics (device auth, TSL, presence) — belongs to the application
- Netty-style annotations/pipelines — Go code is explicit

## Quick start (TCP echo)

```go
srv, _ := gnety.New(gnety.Config{
    Network: "tcp",
    Addr:    ":9000",
    Codec:   gnety.LengthFieldCodec{},
    Handler: gnety.HandlerFuncs{
        Message: func(s *gnety.Session, msg any) { _ = s.Send(msg) },
    },
})
_ = srv.Start(context.Background())
defer srv.Stop(context.Background())
```

UDP:

```go
srv, _ := gnety.New(gnety.Config{
    Network:       "udp",
    Addr:          ":9001",
    DatagramCodec: gnety.BytesDatagramCodec{},
    Handler:       gnety.HandlerFuncs{Message: func(s *gnety.Session, msg any) { _ = s.Send(msg) }},
})
```

Request/response:

```go
policy := gnety.RequestKeyPolicyFuncs{
    Request:  func(msg any) (string, bool) { return keyOf(msg), true },
    Response: func(msg any) (string, bool) { return keyOf(msg), true },
}
srv, _ := gnety.New(gnety.Config{Network: "tcp", Addr: ":9000", Codec: codec,
    RequestKeyPolicy: policy, Handler: handler})

// inside a handler:
resp, err := session.Request(ctx, request)
```

## Design choices

| Decision | Why |
|---|---|
| Stdlib `net` backend first (goroutine per TCP connection) | correct, portable, low dependency; IoT hubs commonly need tens of thousands of connections, which Go handles well |
| Optional `gnet` backend (roadmap) | event-loop performance for 100k+ connections without changing the public API |
| Explicit `Codec` / `DatagramCodec` | Go has no annotation reflection pipeline; explicit codecs are testable and fast |
| Session + `Handler` (`OnOpen/OnMessage/OnClose`) | explicit lifecycle without a framework-specific pipeline |
| `IdleTimeout` + `Touch()` | covers heartbeat/keepalive at the transport layer |

## References

- [go-netty/go-netty](https://github.com/go-netty/go-netty) — Netty-inspired pipeline framework (Apache-2.0); design reference, not a dependency.
- [panjf2000/gnet](https://github.com/panjf2000/gnet) — high-performance event-loop networking (Apache-2.0); planned optional backend.

## Versioning

See [VERSIONING.md](VERSIONING.md).

## License

MIT. See [LICENSE](LICENSE).
