[English](DESIGN.md) | 简体中文

# gnety 设计说明

## 为什么单独成库

应用经常需要在 TCP/UDP 上跑自定义设备协议（厂商二进制、遥测设备、UDP datagram），并需要 framing、session 与请求关联。Go 生态有：

- `net`：正确、简单、每连接一个 goroutine；没有 framing/session 模型；
- `gnet`：高性能 event loop；没有 framing/session/handler 模型；

`gnety` 目标是**更高的开发体验**：可组合 pipeline、transport + framing + session + 生命周期，以及可复用协议 codec。它不复制 API，但力求易扩展。

边界：gnety 拥有**线格式**（frame codec 与 protocol codec 子包）；协议库拥有**状态机**与业务语义。

## 组件

```
Server
 ├── transport: net.Listener (TCP) / net.PacketConn (UDP)
 ├── Codec (TCP) / DatagramCodec (UDP)
 ├── Session (ID, RemoteAddr, attrs, Send/Close, Touch)
 └── Handler (OnOpen / OnMessage / OnClose)
```

- TCP：每连接一个 goroutine；`bufio.Reader` + codec；read deadline = `IdleTimeout`；`Session.Send` 在写锁下编码。
- Pipeline：`Middleware` + `Chain` 包装 handler（最外层先执行）；`Recover` 把 panic 转为 session close + log。
- Codec kit：frame codec 与仅线格式的 protocol codec 位于 `codec/<protocol>`；共享字节原语（有界 reader/writer、长度前缀、varint、有界 UTF-8、chunk 重组、错误分类）在 `codec/wire`；已实现 MQTT、CoAP、Modbus（MBAP + PDU）、LwM2M TLV。TLS/WebSocket/WSS server/client 已实现。WebSocket 是 gnety 内仅标准库的 RFC 6455 实现：handshake、frame codec、message/stream 适配是独立阶段；支持 fragmentation、control frame、masking、UTF-8 校验、精确 subprotocol 与有界读取。
- Metrics：`Config.Metrics` 接收 session open/close/reject、message in/out、write failure；按契约标签只到 transport/reason，绝不含 client ID、peer address 或 topic。
- 写路径：每 session 同一时刻一个 writer，等待 writer 有界（`MaxQueuedWrites`，默认 8），满了返回 `ErrWriteBusy`，慢 peer 不能无界增长内存。
- UDP：一个 read loop；session 按 remote address 索引；`Session.Send` 写 datagram；可选 idle cleanup。
- Stop：关闭 listener/packet conn 与 session，按 context timeout 等待。

## 限制与超时

- `MaxSessions` 在 accept/WS/DTLS 跟踪连接前执行；UDP peer 使用同一上限。
- `MaxPendingRequests` 限制 per-session 请求关联；超限返回 `ErrTooManyPending`。
- `WriteTimeout` 作用于 Session/Client 写；超时返回 `ErrWriteTimeout`。
- `ShutdownTimeout` 限制 Stop；超时返回 `ErrShutdownTimeout`。
- DTLS client 必须提供 `CAFile` 或显式 `TLS.InsecureSkipVerify`；生产不能静默跳过校验。

## 非目标 / 明确限制

- 没有 annotation dispatch、没有反射 pipeline。
- 没有 MQTT/CoAP/LwM2M 状态机；那些有专门库。
- 没有业务语义（设备认证、TSL、presence）。

## 路线图对齐

1. 已完成：请求/响应关联、heartbeat、advanced framing/BCD、DTLS、client connector、MQTT/CoAP 线格式 codec。
2. 下一步：metrics hook、有界背压/session 治理、Modbus 线格式 codec。HTTP/WebSocket codec 占位按需求推进。
3. 之后：可选 gnet backend 与基准；按需 GBK/GB18030。
4. v1.0：API 稳定、一致性与 soak 测试。
