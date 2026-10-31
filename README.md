English | [简体中文](README.zh-CN.md)

# gnety

A composable **network application framework** for Go: server/client bootstrap,
TCP/UDP/TLS/WebSocket transports, handler pipeline, framing codecs and a
**protocol codec kit**.

The goal is high ease of use and reusable protocol foundations — not just raw
sockets. Wire-format codecs live in gnety; protocol state machines live in the
protocol libraries built on top.

> Status: **development snapshot (untagged)** — TCP/UDP/TLS/WS/WSS/DTLS servers and
> clients, pipeline middleware, frame + MQTT/CoAP/Modbus codecs, sessions,
> request/response correlation, heartbeats, metrics hooks and bounded write queues;
> the HTTP/WebSocket codec packages are placeholders.
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

- limits/timeouts: `MaxSessions`, `MaxPendingRequests`, `WriteTimeout`, `ShutdownTimeout`; explicit DTLS `InsecureSkipVerify`
- server bootstrap (done); client connector (TCP/UDP/TCP-TLS/WS/WSS/DTLS, done); reconnect/backpressure (roadmap)
- composable pipeline/middleware (`Chain`, `Recover`, ...)
- TLS / WebSocket / WSS / DTLS (server + client)
- protocol codec kit (`codec/`): shared byte primitives (`codec/wire`), HTTP/1.1, WebSocket, MQTT, CoAP, Modbus MBAP+PDU, LwM2M TLV (wire format only)
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

## Socket options

`Config.Socket` and `ClientConfig.Socket` expose channel-style socket options for
the listener, accepted connections and client dial:

| Option | Kernel setting |
|---|---|
| `ReuseAddr` | `SO_REUSEADDR` |
| `ReusePort` | `SO_REUSEPORT` (Linux/macOS/BSD; Windows rejects it) |
| `KeepAlive` | `SO_KEEPALIVE`; >0 sets the period, <0 disables |
| `NoDelay` | `TCP_NODELAY` (`nil` keeps Go's default) |
| `Linger` | `SO_LINGER`; <0 disables, >0 seconds |
| `ReadBuffer` / `WriteBuffer` | `SO_RCVBUF` / `SO_SNDBUF` |
| `Control` | arbitrary `syscall.RawConn` options, e.g. `TCP_FASTOPEN` |

```go
noDelay := false
srv, _ := gnety.New(gnety.Config{
    Network: "tcp", Addr: ":9000", Codec: gnety.LengthFieldCodec{},
    Socket: gnety.SocketOptions{
        ReuseAddr:   true,
        ReusePort:   true,
        KeepAlive:   30 * time.Second,
        NoDelay:     &noDelay,
        Linger:      -1,
        ReadBuffer:  64 << 10,
        WriteBuffer: 64 << 10,
    },
})

client, _ := gnety.Dial(ctx, gnety.ClientConfig{
    Network: "tcp", Addr: "127.0.0.1:9000", Codec: gnety.LengthFieldCodec{},
    LocalAddr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1")},
    Socket: gnety.SocketOptions{KeepAlive: 30 * time.Second},
})
```

`SO_REUSEPORT` is opt-in; every listener sharing the port must enable it.
`SO_REUSEADDR` is already set by Go for many Unix listeners, so it is mainly
useful for explicit restarts and Windows. TCP-only options are rejected on UDP;
DTLS does not expose socket options in this version.

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

## Examples

Two runnable echo servers live under `examples/`, using the length-prefixed TCP
codec and the datagram codec from the quick start above:

```sh
go run ./examples/tcp-echo   # listens on :9000
go run ./examples/udp-echo   # listens on :9001
```

Both bind the documented ports and log the actual listen address; stop them
with Ctrl-C.

## Design choices

| Decision | Why |
|---|---|
| Stdlib `net` backend first (goroutine per TCP connection) | correct, portable, low dependency; IoT hubs commonly need tens of thousands of connections, which Go handles well |
| Standard-library-only RFC 6455 WebSocket | no external or legacy WebSocket dependency; separate handshake, frame codec and message/stream stages support fragmentation, control frames and bounded reads |
| Optional `gnet` backend (roadmap) | event-loop performance for 100k+ connections without changing the public API |
| Explicit `Codec` / `DatagramCodec` | Go has no annotation reflection pipeline; explicit codecs are testable and fast |
| Session + `Handler` (`OnOpen/OnMessage/OnClose`) | explicit lifecycle without a framework-specific pipeline |
| `IdleTimeout` + `Touch()` | covers heartbeat/keepalive at the transport layer |

## References

- [panjf2000/gnet](https://github.com/panjf2000/gnet) — high-performance event-loop networking (Apache-2.0); planned optional backend.

## Versioning

See [VERSIONING.md](VERSIONING.md).

## License

MIT. See [LICENSE](LICENSE).
