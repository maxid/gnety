# Changelog

## [Unreleased]

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
