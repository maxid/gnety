# gnety

A composable **network application framework** for Go: server/client bootstrap,
TCP/UDP/TLS/WebSocket transports, handler pipeline, framing codecs and a
**protocol codec kit**.

The goal is high ease of use and reusable protocol foundations — not just raw
sockets. Wire-format codecs live in gnety; protocol state machines live in the
protocol libraries built on top.

> Status: **development snapshot (untagged)** — TCP/UDP/TLS/WS/WSS servers,
> pipeline middleware, frame + CoAP codecs, sessions, request/response correlation
> and heartbeats. MQTT codec, DTLS and client connector are on the roadmap.
> No tags are published until a stable release; use commit pseudo-versions.
> License: MIT. Design notes: [docs/DESIGN.md](docs/DESIGN.md).

## Positioning

gnety is a **framework**, positioned by ease of use and protocol foundations:

| gnety provides | Protocol libraries provide |
|---|---|
| server/client bootstrap and lifecycle | MQTT QoS/retained/session state machine |
| handler pipeline + middleware | CoAP observe/block-wise state machine |
| TCP/UDP/TLS/WebSocket/DTLS transports | LwM2M registration/observe state machine |
| framing codecs (length, delimiter, line, fixed, custom) | device auth, TSL, business rules |
| protocol codec kit (wire format: HTTP/1.1, WebSocket, MQTT, CoAP, ... roadmap) | product semantics |
| sessions, heartbeat/idle, request/response correlation | — |
| metrics hooks, broadcast, test utilities | — |

Rules:

- Wire-format codecs belong to gnety (reusable across applications).
- Protocol state machines and business semantics belong to protocol libraries
  built on gnety.
- gnety must never import a protocol library.

## Scope

**Planned / in progress**

- server bootstrap (done); client connector (TCP/UDP/TCP-TLS, done); reconnect/backpressure (roadmap)
- composable pipeline/middleware (`Chain`, `Recover`, ...)
- TLS / WebSocket / WSS (done); DTLS (planned)
- protocol codec kit skeleton (`codec/`): HTTP/1.1, WebSocket, MQTT, CoAP (wire format only)
- metrics hooks, event-loop backend option
- broadcast and test utilities

**Out of scope**

- protocol state machines and business semantics (MQTT broker logic, LwM2M registry, CoAP observe)
- annotation/reflection magic; Go-idiomatic explicit pipelines

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

TLS / WebSocket:

```go
// TLS
srv, _ := gnety.New(gnety.Config{
    Network: "tcp-tls", Addr: ":9443", Codec: codec,
    TLS: gnety.TLSConfig{CertFile: "cert.pem", KeyFile: "key.pem"},
})
// WebSocket
srv, _ = gnety.New(gnety.Config{
    Network: "ws", Addr: ":9002", Path: "/echo", Subprotocol: "gnety", Codec: codec,
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

Pipeline:

```go
handler := gnety.Chain(baseHandler,
    gnety.Recover(logger),
    myAuthMiddleware,
    myMetricsMiddleware,
)
srv, _ := gnety.New(gnety.Config{Network: "tcp", Addr: ":9000", Codec: codec, Handler: handler})
```

Broadcast:

```go
n := srv.Broadcast([]byte("{\"type\":\"ping\"}"))
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
