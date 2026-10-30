# Changelog

> Development snapshots are untagged; entries below are chronological.


## [Unreleased]

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
