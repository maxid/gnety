[English](README.md) | 简体中文

# codec/modbus

仅 Modbus TCP **线格式**：MBAP header、平台使用的 function code 类型化 PDU，以及 exception response。

明确不在范围：事务匹配、重试、polling schedule、register-address map、设备模型、把 exception code 映射为设备错误。这些属于使用本包的 Modbus client 库（gmodbus）。

## 两层

`Frame`、`Marshal`、`Read`、`Codec` 是最初的原始 PDU MBAP framing，保持不变：

```go
frame, err := modbus.Read(reader, 0)     // transaction id, unit id, PDU bytes
raw, err := frame.Marshal()              // MBAP header plus PDU
```

类型层为每个 function code 增加 struct、一条 decode 路径、一条 encode 路径：

```go
req := &modbus.ReadHoldingRegistersRequest{Address: 0x006B, Quantity: 3}
pdu, err := req.PDU()                    // 03 00 6B 00 03
err = modbus.EncodePDU(writer, req)      // the same bytes, one Write call

msg, err := modbus.DecodePDU(pdu)         // request orientation
msg, err = modbus.DecodeResponse(req, responsePDU)
```

`TypedFrame` 及其 `Marshal`/`Frame` 带类型化消息过 MBAP；`ReadTyped`/`ReadTypedResponse` 从流读取一个类型化帧：

```go
frame, err := modbus.ReadTypedResponse(reader, 0, req)
```

## 消息

| Function | Request | Response |
| --- | --- | --- |
| 0x01 Read Coils | `ReadCoilsRequest` | `ReadCoilsResponse`（`Bits []bool`） |
| 0x02 Read Discrete Inputs | `ReadDiscreteInputsRequest` | `ReadDiscreteInputsResponse`（`Bits []bool`） |
| 0x03 Read Holding Registers | `ReadHoldingRegistersRequest` | `ReadHoldingRegistersResponse`（`Registers []uint16`） |
| 0x04 Read Input Registers | `ReadInputRegistersRequest` | `ReadInputRegistersResponse`（`Registers []uint16`） |
| 0x05 Write Single Coil | `WriteSingleCoil` | `WriteSingleCoil`（echo） |
| 0x06 Write Single Register | `WriteSingleRegister` | `WriteSingleRegister`（echo） |
| 0x0F Write Multiple Coils | `WriteMultipleCoilsRequest` | `WriteMultipleCoilsResponse` |
| 0x10 Write Multiple Registers | `WriteMultipleRegistersRequest` | `WriteMultipleRegistersResponse` |

读请求携带 `Address` 与 `Quantity`；多写请求携带 `Address` 与 `Values`，quantity/byte count 由 slice 推导。

Exception 响应解码为 `*Exception`（请求 function code + `ExceptionCode`）。`Exception` 实现 `error`：`errors.As` 取类型，`errors.Is(err, modbus.ErrException)` 匹配任意 code。exception 是合法应答，因此解码返回 message 与 nil error。

## PDU 布局决策

- bit 数据每 coil/input 一位，从第一个数据字节最低位开始；最后一个字节超出声明 quantity 的位编码为 0，decoder 忽略，`DecodeResponse` 按请求 quantity 裁剪。
- register 值是大端 16-bit word。
- 读请求五字节：function code、起始地址、quantity。
- 读响应：function code、一字节 count、数据；bit 读 count=`ceil(quantity/8)`，register 读 count=`2*quantity`。
- 多写请求：function code、地址、quantity、一字节 count、数据，count 规则同上。
- 多写响应五字节回显地址与 quantity。
- `DecodePDU` 解码请求（与 exception response）。读响应没有方向标志，byte count 会被误读为地址的一部分，因此响应由 `DecodeResponse` 解码，它接收所应答的请求。

## 校验

规范中的 quantity 边界全部执行：读 1–2000 bit、1–125 register；多写 1–1968 coil、1–123 register。起始地址是 `uint16`，register value 也是 `uint16`。Write Single Coil 必须是 0xFF00 或 0x0000；byte count 必须与 value count 匹配；尾随/缺失字节拒绝。

`DecodeResponse` 还校验响应契约：function code 必须匹配请求，读响应数量必须等于请求数量，写单响应必须回显地址与值，多写响应必须报告请求的地址与数量；exception 响应对请求 function code 接受任意 exception code。

截断 PDU 是 `wire.KindMalformed`，不是 `wire.KindIncomplete`：MBAP length 已界定；未知 function code 是 `wire.KindUnsupported`；超过 `MaxPDU` 是 `wire.KindTooLarge`。
