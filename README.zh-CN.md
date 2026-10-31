[English](README.md) | 简体中文

# gnety

Go 的可组合**网络应用框架**：服务端/客户端引导、TCP/UDP/TLS/WebSocket 传输、
处理器流水线、分帧编解码器，以及一套**协议编解码套件**。

目标是高易用性与可复用的协议基座——而不只是裸 socket。线格式编解码器位于 gnety；
协议状态机位于构建在其之上的协议库中。

> 状态：**开发快照（未打标签）** — TCP/UDP/TLS/WS/WSS/DTLS 服务端与客户端、
> 流水线中间件、分帧 + MQTT/CoAP/Modbus 编解码器、会话、
> 请求/响应关联、心跳、指标钩子与有界写队列；
> HTTP/WebSocket 编解码包目前是占位实现。
> 在稳定版本发布前不会打标签；请使用 commit 伪版本（pseudo-version）。
> 许可证：MIT。设计说明：[docs/DESIGN.md](docs/DESIGN.md)。

## 定位

gnety 是一个**框架**，以易用性和协议基座为定位：

| gnety 提供 | 协议库提供 |
|---|---|
| 服务端/客户端引导与生命周期 | MQTT QoS/保留消息/会话状态机 |
| 处理器流水线 + 中间件 | CoAP observe/块传输状态机 |
| TCP/UDP/TLS/WebSocket/DTLS 传输 | LwM2M 注册/observe 状态机 |
| 分帧编解码器（长度、分隔符、行、固定长度、自定义） | 设备鉴权、TSL、业务规则 |
| 协议编解码套件（线格式：HTTP/1.1、WebSocket、MQTT、CoAP……路线图） | 产品语义 |
| 会话、心跳/空闲、请求/响应关联 | — |
| 指标钩子、广播、测试工具 | — |

规则：

- 线格式编解码器属于 gnety（可跨应用复用）。
- 协议状态机与业务语义属于构建在 gnety 之上的协议库。
- gnety 绝不能 import 协议库。

## 范围

**计划中 / 进行中**

- 限制/超时：`MaxSessions`、`MaxPendingRequests`、`WriteTimeout`、`ShutdownTimeout`；显式的 DTLS `InsecureSkipVerify`
- 服务端引导（已完成）；客户端连接器（TCP/UDP/TCP-TLS/WS/WSS/DTLS，已完成）；重连/背压（路线图）
- 可组合的流水线/中间件（`Chain`、`Recover` 等）
- TLS / WebSocket / WSS / DTLS（服务端 + 客户端）
- 协议编解码套件（`codec/`）：共享字节原语（`codec/wire`）、HTTP/1.1、WebSocket、MQTT、CoAP、Modbus MBAP+PDU、LwM2M TLV（仅线格式）
- 指标钩子、事件循环后端选项
- 广播与测试工具

**不在范围内**

- 协议状态机与业务语义（MQTT broker 逻辑、LwM2M 注册表、CoAP observe）
- 注解/反射魔法；Go 惯用的显式流水线

## 快速开始（TCP 回显）

```go
srv, _ := gnety.New(gnety.Config{
    Network: "tcp",
    Addr:    ":9000",
    Codec:   gnety.LengthFieldCodec{},
    Handler: gnety.HandlerFuncs{
        Message: func(s *gnety.Session, msg any) { _ = s.Send(msg) },
    },
})
_ = srv.Start(context.Background())
defer srv.Stop(context.Background())
```

UDP：

```go
srv, _ := gnety.New(gnety.Config{
    Network:       "udp",
    Addr:          ":9001",
    DatagramCodec: gnety.BytesDatagramCodec{},
    Handler:       gnety.HandlerFuncs{Message: func(s *gnety.Session, msg any) { _ = s.Send(msg) }},
})
```

TLS / WebSocket：

```go
// TLS
srv, _ := gnety.New(gnety.Config{
    Network: "tcp-tls", Addr: ":9443", Codec: codec,
    TLS: gnety.TLSConfig{CertFile: "cert.pem", KeyFile: "key.pem"},
})
// WebSocket
srv, _ = gnety.New(gnety.Config{
    Network: "ws", Addr: ":9002", Path: "/echo", Subprotocol: "gnety", Codec: codec,
})
```

## Socket 配置

`Config.Socket` 与 `ClientConfig.Socket` 暴露 channel-style的 listener、accept 后
连接和 client dial 选项：

| 选项 | 内核设置 |
|---|---|
| `ReuseAddr` | `SO_REUSEADDR` |
| `ReusePort` | `SO_REUSEPORT`（Linux/macOS/BSD；Windows 明确拒绝） |
| `KeepAlive` | `SO_KEEPALIVE`；>0 设周期，<0 关闭 |
| `NoDelay` | `TCP_NODELAY`（`nil` 保留 Go 默认） |
| `Linger` | `SO_LINGER`；<0 关闭，>0 秒 |
| `ReadBuffer` / `WriteBuffer` | `SO_RCVBUF` / `SO_SNDBUF` |
| `Control` | 任意 `syscall.RawConn` 选项，如 `TCP_FASTOPEN` |

```go
noDelay := false
srv, _ := gnety.New(gnety.Config{
    Network: "tcp", Addr: ":9000", Codec: gnety.LengthFieldCodec{},
    Socket: gnety.SocketOptions{
        ReuseAddr:   true,
        ReusePort:   true,
        KeepAlive:   30 * time.Second,
        NoDelay:     &noDelay,
        Linger:      -1,
        ReadBuffer:  64 << 10,
        WriteBuffer: 64 << 10,
    },
})

client, _ := gnety.Dial(ctx, gnety.ClientConfig{
    Network: "tcp", Addr: "127.0.0.1:9000", Codec: gnety.LengthFieldCodec{},
    LocalAddr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1")},
    Socket: gnety.SocketOptions{KeepAlive: 30 * time.Second},
})
```

`SO_REUSEPORT` 是显式开启的；共享同一端口的 listener 都要开启。
`SO_REUSEADDR` 在多数 Unix 上 Go 已经给 listener 设置，主要用于显式重启和 Windows。
TCP 专属选项在 UDP 上会被拒绝；DTLS 当前不暴露 socket 选项。

## 自定义 socket

应用自己持有传输层，或需要 gnety 无法创建的 socket 时，直接传入已 bind 的 socket：

```go
ln, _ := net.Listen("tcp", ":9000")
srv, _ := gnety.New(gnety.Config{
    Network:  "tcp",
    Listener: ln, // Stop 时由 gnety 关闭
    Codec:    gnety.LengthFieldCodec{},
})

packet, _ := net.ListenPacket("udp", ":9001")
udp, _ := gnety.New(gnety.Config{
    Network: "udp", PacketConn: packet, DatagramCodec: gnety.BytesDatagramCodec{},
})
```

`Listener` 是 transport-ready：`tcp-tls`/`wss` 必须已经是 TLS listener（gnety 不再包一层），
`ws` 则是原始 TCP listener。这样应用可以自己持有 Unix socket、SO_REUSEPORT listener
或自定义 `net.Listener`。`PacketConn` 只用于 `udp`。

客户端用 `NewClient` 包装任意 `net.Conn`：

```go
conn, _ := net.Dial("unix", "/tmp/app.sock")
client, _ := gnety.NewClient(conn, gnety.ClientConfig{
    Network: "tcp", Codec: gnety.LengthFieldCodec{},
})
```

`NewClient` 不负责 dial；连接由调用方持有，直到 `Client.Close`。

请求/响应：

```go
policy := gnety.RequestKeyPolicyFuncs{
    Request:  func(msg any) (string, bool) { return keyOf(msg), true },
    Response: func(msg any) (string, bool) { return keyOf(msg), true },
}
srv, _ := gnety.New(gnety.Config{Network: "tcp", Addr: ":9000", Codec: codec,
    RequestKeyPolicy: policy, Handler: handler})

// inside a handler:
resp, err := session.Request(ctx, request)
```

流水线：

```go
handler := gnety.Chain(baseHandler,
    gnety.Recover(logger),
    myAuthMiddleware,
    myMetricsMiddleware,
)
srv, _ := gnety.New(gnety.Config{Network: "tcp", Addr: ":9000", Codec: codec, Handler: handler})
```

广播：

```go
n := srv.Broadcast([]byte("{\"type\":\"ping\"}"))
```

## 示例

`examples/` 下有两个可运行的回显服务端，分别使用上文快速开始中的长度前缀 TCP
编解码器与数据报编解码器：

```sh
go run ./examples/tcp-echo   # 监听 :9000
go run ./examples/udp-echo   # 监听 :9001
```

两者绑定文档中记录的端口并打印实际监听地址；Ctrl-C 停止。

## 设计选择

| 决策 | 理由 |
|---|---|
| 优先使用标准库 `net` 后端（每个 TCP 连接一个 goroutine） | 正确、可移植、依赖少；IoT 枢纽通常需要数万连接，Go 能很好地处理 |
| 仅用标准库实现 RFC 6455 WebSocket | 不依赖外部或历史遗留的 WebSocket 实现；独立的手握、帧编解码与消息/流阶段支持分片、控制帧和有界读取 |
| 可选的 `gnet` 后端（路线图） | 在不改变公开 API 的前提下，为 10 万+ 连接提供事件循环性能 |
| 显式的 `Codec` / `DatagramCodec` | Go 没有注解反射流水线；显式编解码器易于测试且快速 |
| 会话 + `Handler`（`OnOpen/OnMessage/OnClose`） | 显式生命周期，无需框架特有的流水线 |
| `IdleTimeout` + `Touch()` | 在传输层覆盖心跳/保活 |

## 参考资料

- [panjf2000/gnet](https://github.com/panjf2000/gnet) — 高性能事件循环网络库（Apache-2.0）；计划作为可选后端。

## 版本管理

见 [VERSIONING.md](VERSIONING.md)。

## 许可证

MIT。见 [LICENSE](LICENSE)。
