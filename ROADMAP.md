# Roadmap

- [x] TCP server: codec, sessions, lifecycle, idle timeout, graceful stop
- [x] UDP server: datagram codec, peer sessions, idle cleanup
- [x] Length-field (u32 BE) and delimiter codecs
- [x] Request/response correlation (`RequestKeyPolicy`)
- [x] Heartbeat messages and heartbeat/idle timeouts
- [ ] Metrics hooks
- [ ] TLS listener / WebSocket transport
- [ ] Optional gnet backend + benchmarks
- [x] Configurable length-field framing (magic/offset/size/endian) and BCD helpers
- [ ] GBK/GB18030 helpers
- [ ] Client connector + reconnect
- [ ] v1.0 API stabilization
