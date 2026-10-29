# Roadmap

- [x] TCP server: codec, sessions, lifecycle, idle timeout, graceful stop
- [x] UDP server: datagram codec, peer sessions, idle cleanup
- [x] Length-field (u32 BE) and delimiter codecs
- [ ] Request/reply correlation (`RequestKeyPolicy` equivalent)
- [ ] Heartbeat helpers + metrics hooks
- [ ] TLS listener / WebSocket transport
- [ ] Optional gnet backend + benchmarks
- [ ] Advanced framing (magic, dynamic length, endian, BCD/GBK)
- [ ] Client connector + reconnect
- [ ] v1.0 API stabilization
