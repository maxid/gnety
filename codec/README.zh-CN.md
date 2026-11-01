[English](README.md) | 简体中文

# 协议 codec kit

可复用协议的线格式 codec。**codec 包不包含协议状态机，也不包含业务语义。**

## 布局（规划）

```
codec/
  mqtt/             MQTT 3.1/3.1.1/5 packet codec          [已实现]
  coap/             CoAP message/option codec             [已实现]
  modbus/           Modbus TCP MBAP + PDU codec           [已实现]
  tlv/              LwM2M TLV entry codec                 [已实现]
  http/             HTTP/1.1 Upgrade 辅助                 [已实现]
  http/websocket/   RFC 6455 handshake/frame/message/conn [已实现]
```

## 契约

- Stream 协议实现 `gnety.Codec`（`Decode(*bufio.Reader)`、`Encode(io.Writer, any)`）。
- Datagram 协议实现 `gnety.DatagramCodec`。
- Codec 包不得 import 协议库或应用代码。
- 协议状态机（MQTT QoS、CoAP observe、LwM2M registration）位于依赖 gnety 的协议库。

Frame codec（`LengthFieldCodec`、`DelimiterCodec`、`LineCodec`、`FixedLengthCodec`）目前为兼容放在根包；新协议 codec 应放在 `codec/<protocol>`。
