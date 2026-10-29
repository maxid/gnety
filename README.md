# gnety

A small **TCP/UDP transport framework for custom device protocols** in Go.
It fills the gap between the standard library / `gnet` (raw transports) and the
needs of the XDMP device-access layer: framing codecs, sessions, lifecycle
handlers, idle timeouts and graceful shutdown.

> Status: **v0.1.0** — TCP/UDP servers, codecs, sessions, handlers, idle timeout.
> License: MIT. Design notes: [docs/DESIGN.md](docs/DESIGN.md).

## Scope

**In scope**

- custom binary/text protocols over **TCP** (length-field, delimiter, custom codecs)
- **UDP** datagram servers (session per peer)
- session identity/attributes, lifecycle hooks, idle timeout, graceful stop
- a stable base for future request/response correlation, heartbeat and TLS

**Out of scope**

- MQTT — use [gmqx](https://github.com/maxid/gmqx)
- HTTP/WebSocket — use `net/http`
- rules/TSL/business semantics — belongs to the XDMP hub adapter
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

## Design choices

| Decision | Why |
|---|---|
| Stdlib `net` backend first (goroutine per TCP connection) | correct, portable, low dependency; IoT hubs commonly need tens of thousands of connections, which Go handles well |
| Optional `gnet` backend (roadmap) | event-loop performance for 100k+ connections without changing the public API |
| Explicit `Codec` / `DatagramCodec` | Go has no annotation reflection pipeline; explicit codecs are testable and fast |
| Session + `Handler` (`OnOpen/OnMessage/OnClose`) | matches the Java `xdmp-netty` lifecycle model without cloning Netty |
| `IdleTimeout` + `Touch()` | covers heartbeat/keepalive at the transport layer |

## Relation to Java `xdmp-netty`

| Java xdmp-netty | gnety |
|---|---|
| `NettyTcpServer` / `NettyUdpServer` | `Server` with `Network: "tcp" \| "udp"` |
| `LengthFieldBasedFrameDecoder`, `DelimiterBasedFrameDecoder`, `MagicLengthField...` | `LengthFieldCodec`, `DelimiterCodec` (advanced variants roadmap) |
| `Session` / `SessionManager` / `clientId` index | `Session` / `Server.SessionCount()` (clientId index roadmap) |
| `@Endpoint` / `@Mapping` dispatch | `Handler.OnMessage` (explicit dispatch) |
| `RequestKeyPolicy` (request/reply correlation) | roadmap v0.2 |
| heartbeat / idle handling | `IdleTimeout` |
| annotation `@Async` | Go goroutine per connection |

## References

- [go-netty/go-netty](https://github.com/go-netty/go-netty) — Netty-inspired pipeline framework (Apache-2.0); design reference, not a dependency.
- [panjf2000/gnet](https://github.com/panjf2000/gnet) — high-performance event-loop networking (Apache-2.0); planned optional backend.
- Java XDMP `xdmp-netty` / `xdmp-netty-protocol` — migration source of requirements.

## License

MIT. See [LICENSE](LICENSE).
