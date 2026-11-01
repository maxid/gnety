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

## PUBLISH 复用路径

broker 会把同一种包读上百万次，因此 PUBLISH 也可以解码进调用方提供的缓冲区，而不是新分配内存：

```go
var buf []byte // 每连接一份：只增长一次，之后复用
view, err := mqtt.ReadPublishInto(reader, mqtt.Version311, maxPacketSize, buf)
switch {
case errors.Is(err, mqtt.ErrNotPublish):
	msg, err := d.Decode(reader) // 未消费任何字节：走类型化路径
case err != nil:
	// 错误与 Decode 相同
}
use(view.Topic, view.Payload, view.QoS, view.PacketID, view.Properties)
buf = view.Buffer
```

`ReadPublishInto` 对 PUBLISH 施加与 `Decoder.Decode` 完全相同的校验——读任何字节前按上限检查 remaining length、QoS > 2、QoS 0 带 DUP、topic 的 UTF-8 与 U+0000 规则、packet id 0、3.1.1 无 property block、5.0 的 property block 用同一张表校验——并返回相同的错误。server 侧施加的 topic name 规则（非空、不含通配符、MQTT 5 中 topic alias 可替代空名）仍属于协议层，和 `Decode` 一样；因此 topic 以字节形式返回，调用方可以在分配字符串之前先做检查。

生命周期：`Topic` 与 `Payload` 是 `Buffer` 的子切片（`Buffer` 就是传入的 `dst`，`dst` 不够大时是扩容后的新缓冲区），只在下次使用同一缓冲区的读取之前有效。保留 `view.Buffer` 并作为下次的 `dst` 传回；它只增长。需要跨读取保留 payload 的调用方自行复制。

分配：MQTT 3.1.1 的 PUBLISH（以及 property block 为空的 MQTT 5 PUBLISH）解码零分配。携带属性的 MQTT 5 property block 会分配：它被复制出缓冲区后交给 `Decode` 用的同一张属性表解码，属性规则因此只有一份实现。
