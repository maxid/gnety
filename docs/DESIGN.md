# gnety design notes

## Why a separate library

Applications often need custom device protocols over TCP and UDP (vendor
binary protocols, telemetry devices, UDP datagrams) with framing, sessions and
request correlation. Go's ecosystem offers:

- `net` — correct, simple, goroutine-per-connection; no framing/session model;
- `gnet` — fast event loop; no framing/session/handler model;

`gnety` targets a **high-level developer experience**: composable pipeline,
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
- Codec kit: frame codecs and wire-format-only protocol codecs live in
  `codec/<protocol>`, with the shared byte-level primitives (bounded readers and
  writers, length prefixes, varints, bounded UTF-8, chunk reassembly, error
  classification) in `codec/wire`; MQTT, CoAP and Modbus (MBAP + PDU) are
  implemented. TLS/WebSocket/WSS servers
  and clients are implemented. WebSocket is a standard-library-only RFC 6455
  implementation inside gnety: handshake, frame codec and message/stream
  adaptation are separate stages. It supports fragmentation, control frames,
  masking, UTF-8 validation, exact subprotocols and bounded message reads.
- UDP: one read loop; session keyed by remote address; `Session.Send` writes a
  datagram; optional idle cleanup.
- Stop: closes listener/packet conn and sessions, waits with context timeout.

## Limits and timeouts

- `MaxSessions` is enforced at accept/WS/DTLS before a connection is tracked; UDP peers use the same cap.
- `MaxPendingRequests` bounds per-session request correlation; over-limit returns `ErrTooManyPending`.
- `WriteTimeout` applies to Session/Client writes; timeout returns `ErrWriteTimeout`.
- `ShutdownTimeout` bounds Stop; timeout returns `ErrShutdownTimeout`.
- DTLS clients must provide `CAFile` or explicitly set `TLS.InsecureSkipVerify`; production cannot silently skip verification.

## Non-goals / deliberate limits

- No annotation-based dispatch, no reflection pipelines.
- No MQTT/CoAP/LwM2M state machines; those have dedicated libraries.
- No business semantics (device auth, TSL, presence).

## Roadmap alignment

1. Done: request/response correlation, heartbeat, advanced framing/BCD,
   DTLS, client connector, and MQTT/CoAP wire codecs.
2. Next: metrics hooks, bounded backpressure/session governance, and Modbus
   wire codec. HTTP/WebSocket codec placeholders remain demand-driven.
3. Later: optional gnet backend and benchmarks; GBK/GB18030 helpers if needed.
4. v1.0: API stability, conformance and soak tests.
