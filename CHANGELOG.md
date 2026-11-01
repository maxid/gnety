# Changelog

> Development snapshots are untagged; entries below are chronological.


## [Unreleased]

- `codec/coap`: added `CodeRequestEntityIncomplete` (4.08), the answer to a block-wise request body whose blocks are missing or out of order (RFC 7959).

- `codec/modbus`: added the Modbus RTU wire format — `RTUFrame`, `CRC16`, whole-datagram `Parse`, direction-aware stream readers (`Read`/`ReadResponse`) whose frame length comes from the function code, typed `ReadTypedRequest`/`ReadTypedResponse`, and `RTUOptions{NoCRC}` for gateways that strip the checksum. Added `Frame.Parse` for whole MBAP datagrams.
- `codec/modbus`: added typed PDUs for 0x16 Mask Write Register, 0x17 Read/Write Multiple Registers and 0x2B/0x0E Read Device Identification, with the matching response checks.
- `codec/modbus`: added `Functions`, a function-code registry that decodes vendor codes (or replaces a standard code's payload) before the built-in decoder, including PDU-length hooks for RTU streams.
- Transport: `ClientConfig.TLSConfig`/`DTLSConfig` and `Config.TLSConfig`/`DTLSConfig` accept in-memory security configurations instead of certificate files.
- `Client.SetDeadline` bounds or aborts an in-flight `Send`/`Receive`, so a protocol library can implement a per-attempt timeout and cancellation.

- `codec/mqtt`: added `ReadPublishInto`, a PUBLISH reader that decodes into
  caller-supplied storage instead of allocating. The returned `PublishView`
  exposes the fixed-header flags, the remaining length, the topic as a `[]byte`
  slice, the packet identifier, the offset and length of the MQTT 5 property
  block, the decoded `Properties` (nil when the packet carried none) and the
  payload; all slices alias the caller's buffer and are valid only until the next
  read that uses it. The validation is the one `Decoder.Decode` applies to a
  PUBLISH — size limit before any body byte is read, QoS > 2, DUP with QoS 0,
  the UTF-8 and U+0000 rules, packet id 0, no property block at 3.1.1, and the
  same property table for a 5.0 block — with the same errors, and the corpus in
  `publish_test.go` decodes every frame through both paths and requires the same
  accept/reject decision and the same field values. An MQTT 3.1.1 PUBLISH (and an
  MQTT 5 one with an empty property block) decodes with zero allocations; a
  property block that carries properties is copied out of the buffer and decoded
  by the shared table, which allocates, so the correctness of the property rules
  stays in one place. `ErrNotPublish` reports that the next packet is not a
  PUBLISH without consuming it, so a caller can fall back to `Decode`.
- Added `SocketOptions` for server and client: `SO_REUSEADDR`, `SO_REUSEPORT`,
  TCP keepalive/nodelay/linger, read/write buffers and a `Control` hook.
  DTLS does not expose socket options yet.
- Added custom socket injection: `Config.Listener`/`Config.PacketConn` for
  servers and `NewClient(net.Conn, ClientConfig)` for clients.
- Added TCP/UDP port helpers: bind-probe occupancy, next free port and a shared
  allocation set for adjacent ports.


- `codec/tlv`: the OMA LwM2M TLV wire format (type nibble, 8/16-bit identifier,
  inline and 8/16/24-bit lengths, nested object instance / multiple resource
  entries) built on `codec/wire`. It never allocates from an unvalidated
  declared length, bounds nesting to `MaxDepth`, rejects duplicate identifiers
  inside one scope and treats a truncated payload as malformed, because a TLV
  payload is a datagram. `Marshal` sorts every scope by identifier, so the same
  tree always produces the same bytes, and neither direction retains the other
  side's buffer. `ParseLimit` is the size-bounded entry point. The encoder is
  byte-compatible with the implementation it replaces (`compat_test.go` keeps
  the old one for a differential test); the LwM2M server library now delegates
  to this package.


- N1 metrics and write backpressure: `Config.Metrics` receives low-cardinality
  transport events (session open/close/reject, message in/out, write failures)
  with `NoopMetrics` as the default; `Config.MaxQueuedWrites` bounds concurrent
  writers per session (default 8, negative disables) and returns `ErrWriteBusy`
  instead of queueing without bound.


- `codec/wire`: shared byte-level primitives for protocol codecs — bounded
  reader/writer, length prefixes, base-128 varint, bounded UTF-8 strings, chunk
  reassembly and classified errors (`ErrIncomplete` / `ErrMalformed` /
  `ErrTooLarge` / `ErrUnsupported`). Decoding never allocates from an unvalidated
  declared length and never aliases the read buffer.


- `codec/modbus`: Modbus TCP wire format (MBAP header + PDU, function-code
  constants, exception flag) built on `codec/wire`; polling, transaction
  matching and exception mapping stay in the Modbus client library.


- N1 limits/timeouts: MaxSessions admission, per-session pending cap, write deadline,
  shutdown timeout; DTLS clients require CA or explicit InsecureSkipVerify.


- Moved RFC 6455 WebSocket handshake/frame/message/Conn into codec/http/websocket;
  transport/server/client only wire the codec. Public Config/behavior unchanged.


- WebSocket API: Origin allowlist, Ping/Pong, CloseWithStatus with close codes/reason,
  SetReadLimit and Subprotocol accessors; UTF-8 DFA validation and close handshake.


- Native RFC 6455 WebSocket server/client (standard library only): handshake,
  frame codec, fragmentation/control frames/UTF-8 validation and bounded reads.


- DTLS server/client over UDP using pion/dtls (certificate/CA/mTLS).


- Client connector for TCP/UDP/TCP-TLS with Codec/DatagramCodec.


## [0.3.0-dev.1] - 2026-10-29

- TLS/WebSocket/WSS listeners (`tcp-tls`, `ws`, `wss`) with path/subprotocol/TLS options.
- `RawHandler` escape hatch for protocol libraries that own framing/state machines.
- Pipeline middleware (`Chain`/`Recover`), `Broadcast`, line/fixed-length codecs.
- `codec/coap` wire-format codec (RFC 7252) and protocol codec package layout.


## [0.2.2] - 2026-10-29

- Fix accept-vs-stop race: reject connections accepted after shutdown, so Stop
  cannot wait on an untracked session.


## [0.2.1] - 2026-10-29

- Fix shutdown race: track accepted connections and close them on Stop so a
  connection that has not yet registered a session cannot block shutdown.


## [0.2.0] - 2026-10-29

- Request/response correlation via pluggable RequestKeyPolicy.
- Heartbeat messages, heartbeat/idle timeouts.
- Configurable length-field codec: magic, offset, 1/2/4-byte length, endianness, header-included length.
- BCD encode/decode helpers.
- Tests for correlation, heartbeat, advanced framing and BCD.


## [0.1.1] - 2026-10-29

- Documentation cleanup: product-neutral, self-contained docs.


## [0.1.0] - 2026-10-29

- Initial TCP/UDP server framework: codec interfaces, length-field/delimiter
  codecs, byte datagram codec, sessions with attributes, lifecycle handler,
  idle timeout, graceful shutdown, examples and tests.
