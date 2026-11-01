[English](ROADMAP.md) | 简体中文

# 路线图

- [x] TCP server：codec、session、生命周期、idle timeout、graceful stop
- [x] UDP server：datagram codec、peer session、idle cleanup
- [x] Length-field（u32 BE）与 delimiter codec
- [x] 请求/响应关联（`RequestKeyPolicy`）
- [x] Heartbeat 消息与 heartbeat/idle timeout
- [x] Pipeline 中间件（Chain/Recover）与 Broadcast
- [x] Line/Fixed-length frame codec
- [x] 协议 codec kit：共享 `codec/wire` 原语（有界 reader/writer、varint、UTF-8、chunk 重组、错误分类）
- [x] 协议 codec kit：MQTT、CoAP、HTTP/WebSocket、Modbus MBAP+RTU+PDU（含厂商功能码注册表）、LwM2M TLV 线格式
- [x] Metrics hook（低基数 transport/reason 事件）
- [x] TLS / WebSocket / WSS listener
- [x] channel 风格 socket 选项：ReuseAddr/ReusePort、TCP keepalive/nodelay/linger、buffer、Control hook
- [x] 自定义 socket：Config.Listener/PacketConn、Client NewClient(net.Conn)
- [x] 端口工具：TCP/UDP bind-probe、下一个空闲端口、共享分配集合
- [x] DTLS transport（server/client）
- [x] 内存态 TLS/DTLS 配置与 `Client.SetDeadline`，供协议库实现单次尝试超时
- [ ] 可选 gnet backend + 基准
- [x] 可配置 length-field framing（magic/offset/size/endian）与 BCD 辅助
- [ ] GBK/GB18030 辅助
- [x] Client connector（TCP/UDP/TCP-TLS）
- [ ] Client reconnect/backpressure
- [ ] v1.0 API 稳定化
