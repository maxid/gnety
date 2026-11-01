[English](README.md) | 简体中文

# codec/mqtt

仅 MQTT 3.1.1 / 5 **线格式**：fixed-header + remaining-length framing、UTF-8 字符串、二进制 blob、variable-byte integer、类型化 control packet 与 MQTT 5 property block。

明确不在范围：QoS in-flight 状态、retained 消息、session、topic alias、订阅匹配与任何设备/产品语义。这些位于使用本包的 MQTT 协议库（gmqx）。

## 类型化 codec

```go
d := &mqtt.Decoder{MaxPacketSize: 1 << 20} // zero Version means 3.1.1
msg, err := d.Decode(reader)               // *mqtt.Connect, *mqtt.Publish, ...

err = mqtt.Encode(writer, msg)             // exactly one Write per packet
```

`Decoder.Decode` 每次读一个 control packet：

- remaining length 在创建 body buffer 前按 `MaxPacketSize` 检查，peer 不能迫使 decoder 预留内存；
- reserved fixed-header flags、QoS > 2、QoS 0 带 DUP、packet id 0、body 未消费声明的 remaining length 均拒绝；
- `Decoder.Version` 从 CONNECT 刷新，server 因此切换到 MQTT 5 布局；client 在 decode CONNACK 前设置 `Version`。AUTH 仅 MQTT 5，按 3.1.1 编解码返回 `wire.KindUnsupported`；
- 截断输入返回 `wire.ErrIncomplete`（仍匹配 `io.EOF`），垃圾返回 `ErrMalformedPacket`，`wire.KindOf` 分类两者。

`Encode` 取消息的 `Version()`：MQTT 5 包带 property block 与可选 reason code；MQTT 3.1.1 包不带，丢弃 `Properties` 与 MQTT 5 subscription options。

`Codec` 把同一组函数包装成 gnety stream codec 形状（`Decode(*bufio.Reader) (any, error)`、`Encode(io.Writer, any) error`）。

消息是普通 struct：`Connect`、`Connack`、`Publish`、`Puback`、`Pubrec`、`Pubrel`、`Pubcomp`、`Subscribe`、`Suback`、`Unsubscribe`、`Unsuback`、`Pingreq`、`Pingresp`、`Disconnect`、`Auth`。AUTH 是 MQTT 5 扩展认证步骤：零长度 body 表示成功且无属性，一字节 body 是裸 reason code（0x00 success、0x18 continue authentication、0x19 re-authenticate），更长 body 加 property block（只允许 authentication method/data）。`Properties` 建模 MQTT 5 属性集，随每个包编解码；unknown/repeated/packet-incompatible property identifier 拒绝。

## Framing 辅助

```go
header, body, err := mqtt.ReadPacket(reader, maxPacketSize)
err = mqtt.WritePacket(writer, 0x30, body)
```

保留给自行解析 packet body 的调用方。
