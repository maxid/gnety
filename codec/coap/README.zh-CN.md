[English](README.md) | 简体中文

# codec/coap

仅 CoAP（RFC 7252）**线格式**：一个 datagram 的四字节 header、token、option stream 与 payload，外加类型化 option 访问器、code 常量与有界 decoder。

明确不在范围：重传、message-id 去重、exchange lifetime、observe registry、block-wise transfer engine。这些是协议状态机，位于使用本包的 CoAP 协议库（gcoap）与 `glwm2m`。

## 两层

`Parse`/`Marshal` 是最初的整 datagram API，与类型层之前的代码逐字节兼容（gcoap 使用）：

```go
msg, err := coap.Parse(datagram) // Message or a classified error
raw, err := msg.Marshal()        // options sorted by number
```

`Decoder`、`ParseLimit`、`Encode` 是 server 需要的类型化入口：

```go
msg, err := (&coap.Decoder{MaxSize: 1152}).Decode(datagram) // size checked first
msg, err := coap.ParseLimit(datagram, 1152)                 // same check, no receiver
raw, err := coap.Encode(msg)                                // strict validation
```

- `Decode`/`ParseLimit` 在复制字段前拒绝超大 datagram。
- version 非 1 返回 `wire.KindUnsupported`；token length 9–15、payload marker 无 payload、reserved option nibble、截断 datagram 返回 `wire.KindMalformed`；超大 datagram 返回 `wire.KindTooLarge`。所有错误都满足 `errors.Is(err, coap.ErrMalformed)`。
- datagram 是完整帧：在字段中间结束是 malformed，不是 incomplete，因为没有更多字节可等。
- decoded token/option value/payload 都新分配，不 alias 调用方 buffer。
- `Encode` 额外要求 options 按编号排序，拒绝不可重复 option 的重复，以及超过定义长度的 value；`Marshal` 保持宽松行为：编码前排序，重复留给调用方。
- `Codec` 包装为 gnety `DatagramCodec`（`Decode([]byte) (any, error)`、`Encode(any) ([]byte, error)`）。

## 类型化消息与 options

`Message` 携带 `Version`、`Type`、`Code`、`MessageID`、`Token`、`Options`、`Payload`。`Options` 忠实于线格式：编号 + 原始 value 字节，未建模扩展可原样往返。

类型化访问器读写同一列表并保持排序：

| Option | 访问器 |
| --- | --- |
| Uri-Path (11) | `URIPath`、`SetURIPath`（每段一个值） |
| Uri-Query (15) | `URIQuery`、`SetURIQuery`（每参数一个值） |
| Content-Format (12) | `ContentFormat`、`SetContentFormat` |
| Accept (17) | `Accept`、`SetAccept` |
| Max-Age (14) | `MaxAge`、`SetMaxAge` |
| ETag (4) | `ETags`、`AddETag`、`SetETag` |
| If-Match (1) | `IfMatches`、`AddIfMatch` |
| If-None-Match (5) | `IfNoneMatch`、`SetIfNoneMatch` |
| Location-Path (8) | `LocationPath`、`SetLocationPath` |
| Location-Query (20) | `LocationQuery`、`SetLocationQuery` |
| Observe (6) | `Observe`、`SetObserve` |
| Block1 (27), Block2 (23) | `Block1`/`SetBlock1`、`Block2`/`SetBlock2` |
| Size1 (60), Size2 (28) | `Size1`/`SetSize1`、`Size2`/`SetSize2` |

未知编号使用 `OptionValue`、`SetOption`、`AddOption` 或直接操作 `Options`。

`Block` 建模 Block1/Block2 值：

```go
b := coap.Block{Num: 2, More: true, SizeExp: 2}
raw, err := b.Encode()            // 3-byte option value
b, err = coap.DecodeBlock(raw)    // accepts 0..3 byte RFC 7959 forms
size := b.Size()
```

SZX 7 保留并拒绝；block number 为 20 bit。Code 常量覆盖 server 路由的方法与响应；`Code.IsRequest`/`Code.IsResponse` 不匹配常量也能分类。
