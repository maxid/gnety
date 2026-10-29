# Roadmap

- [x] TCP server: codec, sessions, lifecycle, idle timeout, graceful stop
- [x] UDP server: datagram codec, peer sessions, idle cleanup
- [x] Length-field (u32 BE) and delimiter codecs
- [x] Request/response correlation (`RequestKeyPolicy`)
- [x] Heartbeat messages and heartbeat/idle timeouts
- [x] Pipeline middleware (Chain/Recover) and Broadcast
- [x] Line/Fixed-length frame codecs
- [ ] Protocol codec kit: package skeleton created; MQTT/CoAP/HTTP/WebSocket wire formats next
- [ ] Metrics hooks
- [x] TLS / WebSocket / WSS listeners
- [x] DTLS transport (server/client)
- [ ] Optional gnet backend + benchmarks
- [x] Configurable length-field framing (magic/offset/size/endian) and BCD helpers
- [ ] GBK/GB18030 helpers
- [x] Client connector (TCP/UDP/TCP-TLS)
- [ ] Client reconnect/backpressure
- [ ] v1.0 API stabilization
