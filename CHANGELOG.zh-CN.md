[English](CHANGELOG.md) | 简体中文

# 变更日志

> 开发快照不打 tag；以下条目按时间顺序排列。

## [Unreleased]

### 变更

- **指标契约移到 `gnety/hook`。** `Metrics`、`NoopMetrics` 与会话标签（`Reason*`）定义在该包，
  根包以类型别名重新导出，已有的 import、名称与接口断言全部照旧。`gnety/hook` 不 import 根包。

### 新增

- `hook.MetricsFuncs`：用可选函数字段实现六个指标方法，nil 字段直接跳过。

- `codec/coap`：新增 `CodeRequestEntityIncomplete`（4.08）与 `CodeConflict`（4.09）；前者用于应答分块请求体中缺块或乱序的 Block1 上传（RFC 7959）。

- `codec/modbus`：新增 Modbus RTU 线格式——`RTUFrame`、`CRC16`、整包 `Parse`、按功能码推导帧长的方向感知流式读取（`Read`/`ReadResponse`）、类型化 `ReadTypedRequest`/`ReadTypedResponse`，以及为去 CRC 网关准备的 `RTUOptions{NoCRC}`；另补 MBAP 整包 `Frame.Parse`。
- `codec/modbus`：新增 0x16 掩码写、0x17 读写多寄存器、0x2B/0x0E 读设备标识的类型化 PDU 与对应响应校验。
- `codec/modbus`：新增 `Functions` 功能码注册表，先于内置解码器解码厂商功能码（或替换标准码载荷），并含 RTU 流式读取所需的 PDU 长度回调。
- 传输：`ClientConfig.TLSConfig`/`DTLSConfig` 与 `Config.TLSConfig`/`DTLSConfig` 接受内存态安全配置，不再只能读证书文件。
- `Client.SetDeadline` 可约束或中止进行中的 `Send`/`Receive`，协议库据此实现单次尝试超时与取消。

- `codec/mqtt`：新增 `ReadPublishInto`，把 PUBLISH 解码进调用方提供的缓冲区而非分配。返回的 `PublishView` 暴露 fixed-header flags、remaining length、`[]byte` 形式的 topic、packet id、MQTT 5 property block 的偏移与长度、解码后的 `Properties`（没有 property block 时为 nil）与 payload；所有切片都别名调用方缓冲区，只在下次使用同一缓冲区的读取之前有效。校验与 `Decoder.Decode` 对 PUBLISH 完全一致（读取任何字节前检查大小上限、QoS > 2、QoS 0 带 DUP、UTF-8 与 U+0000 规则、packet id 0、3.1.1 无 property block、5.0 用同一张属性表），错误也一致；`publish_test.go` 用同一组帧走两条路径，要求接受/拒绝判定与字段值完全相同。MQTT 3.1.1 的 PUBLISH（以及 property block 为空的 MQTT 5 PUBLISH）零分配；携带属性的 property block 会被复制出缓冲区交给共享属性表解码，因此会分配，属性规则只有一份实现。`ErrNotPublish` 表示下一个包不是 PUBLISH 且未消费任何字节，调用方可以回退到 `Decode`。
- 服务端/客户端新增 `SocketOptions`：`SO_REUSEADDR`、`SO_REUSEPORT`、TCP keepalive/nodelay/linger、读写缓冲区与 `Control` hook。DTLS 暂不暴露 socket 选项。
- 新增自定义 socket 注入：服务端 `Config.Listener`/`Config.PacketConn`，客户端 `NewClient(net.Conn, ClientConfig)`。
- 新增 TCP/UDP 端口工具：bind-probe 占用探测、下一个空闲端口、相邻端口的共享分配集合。
- 新增 TCP+UDP 自适应端口：`PortTCPAndUDP`、`AvailablePortBoth`/`AvailablePortBothOn`、`AvailablePortFor`。

- `codec/tlv`：OMA LwM2M TLV 线格式（type nibble、8/16-bit identifier、inline 与 8/16/24-bit length、嵌套 object instance / multiple resource），基于 `codec/wire`。绝不按未校验的声明长度分配；嵌套限制 `MaxDepth`；同一 scope 重复 identifier 拒绝；截断 payload 视为 malformed。`Marshal` 按 identifier 排序每个 scope，同一棵树总产生相同字节；双向都不保留对方 buffer；`ParseLimit` 是大小受限入口。
- N1 metrics 与写背压：`Config.Metrics` 接收低基数 transport 事件（session open/close/reject、message in/out、write failure），默认 `NoopMetrics`；`Config.MaxQueuedWrites` 限制每 session 等待写者数（默认 8，负数关闭），超出返回 `ErrWriteBusy`。
- `codec/wire`：协议 codec 共享的字节原语——有界 reader/writer、长度前缀、base-128 varint、有界 UTF-8、chunk 重组与分类错误（`ErrIncomplete`/`ErrMalformed`/`ErrTooLarge`/`ErrUnsupported`）。解码绝不按未校验声明长度分配，也不 alias 读 buffer。
- `codec/modbus`：基于 `codec/wire` 的 Modbus TCP 线格式（MBAP header + PDU、function code 常量、exception flag）；polling、transaction matching 与 exception mapping 留在 Modbus client 库。
- N1 limits/timeouts：MaxSessions 准入、per-session pending 上限、write deadline、shutdown timeout；DTLS client 要求 CA 或显式 InsecureSkipVerify。
- RFC 6455 WebSocket handshake/frame/message/Conn 移入 `codec/http/websocket`；transport/server/client 只接线 codec。公开 Config/行为不变。
- WebSocket API：Origin allowlist、Ping/Pong、`CloseWithStatus`（close code/reason）、`SetReadLimit`、Subprotocol 访问器；UTF-8 DFA 校验与 close handshake。
- 原生 RFC 6455 WebSocket server/client（仅标准库）：handshake、frame codec、fragmentation/control frame/UTF-8 校验、有界读取。
- 基于 pion/dtls 的 UDP DTLS server/client（certificate/CA/mTLS）。
- TCP/UDP/TCP-TLS client connector，支持 Codec/DatagramCodec。

## [0.3.0-dev.1] - 2026-10-29

- TLS/WebSocket/WSS listener（`tcp-tls`、`ws`、`wss`），支持 path/subprotocol/TLS 选项。
- 面向自有 framing/状态机协议库的 `RawHandler` 逃生口。
- Pipeline 中间件（`Chain`/`Recover`）、`Broadcast`、line/fixed-length codec。
- `codec/coap`（RFC 7252）线格式 codec 与协议 codec 包布局。

## [0.2.2] - 2026-10-29

- 修复 accept-vs-stop 竞争：关闭后 accept 到的连接直接拒绝，Stop 不会等待未跟踪 session。

## [0.2.1] - 2026-10-29

- 修复 shutdown 竞争：跟踪 accepted connection 并在 Stop 关闭，尚未注册 session 的连接不能阻塞 shutdown。

## [0.2.0] - 2026-10-29

- 通过可插拔 `RequestKeyPolicy` 做请求/响应关联。
- Heartbeat 消息、heartbeat/idle timeout。
- 可配置 length-field codec：magic、offset、1/2/4-byte length、endianness、header-included length。
- BCD 编解码辅助。
- correlation、heartbeat、advanced framing、BCD 测试。

## [0.1.1] - 2026-10-29

- 文档清理：产品中性、自包含。

## [0.1.0] - 2026-10-29

- 初始 TCP/UDP server framework：codec 接口、length-field/delimiter codec、byte datagram codec、带 attributes 的 session、lifecycle handler、idle timeout、graceful shutdown、示例与测试。
