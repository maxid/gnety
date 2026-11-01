[English](README.md) | 简体中文

# codec/tlv

仅 LwM2M TLV **线格式**：type nibble、8/16-bit identifier、inline 或 8/16/24-bit length，以及后面的 value 或嵌套 entry。

明确不在范围：LwM2M object model、resource multiplicity 与权限、registration/update 状态、execute 语义、observe 状态。这些是使用本包的协议库（glwm2m）的职责。

## API

```go
raw, err := tlv.Marshal([]tlv.Entry{...})    // canonical bytes, entries sorted by id
entries, err := tlv.Unmarshal(raw)           // bounded by DefaultMaxSize
entries, err := tlv.ParseLimit(raw, 1 << 20) // bounded by the caller's budget
```

```go
type Type uint8 // ObjectInstance, ResourceInstance, MultipleResource, ResourceValue

type Entry struct {
    Type     Type
    ID       uint16
    Value    []byte   // leaf payload
    Children []Entry  // container payload
}
```

Container 是 `ObjectInstance`（type 00）与 `MultipleResource`（type 10）；leaf 是 `ResourceValue`（11）与 `ResourceInstance`（01）。type 值就是 2-bit 线字段，decoded type 一定是合法常量。`Type.Valid`/`Type.IsContainer` 无需匹配常量即可分类。

LwM2M 使用两层嵌套（object instance 套 resource，multiple resource 套 resource instance）；格式与本包允许最多 `MaxDepth`（16）层。

## 契约

本包遵循 gnety codec kit 的同一规则（`codec/wire`）：

- 声明长度在分配前按父级携带字节数与 datagram budget 检查，peer 不能迫使 decoder 预留未发送的内存。
- decoded value 新分配，不 alias 输入 buffer；`Marshal` 复制 value 而不保留，双方拥有自己的 slice；`Marshal` 也不重排传入 slice。
- TLV payload 是 datagram：到达即完整；在 entry 中间结束返回 `wire.KindMalformed`，不是 `wire.ErrIncomplete`。
- 错误经 `codec/wire` 分类：malformed、too large、unsupported；用 `wire.KindOf`，不要匹配字符串。

## 确定性

`Marshal` 在每一层按 identifier 升序输出同一 scope 的 entry；同一 scope 重复 identifier 拒绝，因此顺序是全序：同一棵树总产生相同字节，与调用方构建顺序无关。`Unmarshal` 保留 payload 携带的顺序，这与 `codec/coap` 对宽松 parser 与 canonical encoder 的拆分一致。

## 比宽松 decoder 更严格

三类 naive decoder 能读的 payload 会被拒绝，因为语义有歧义：

| Payload | 拒绝原因 |
| --- | --- |
| 同一 scope 两个相同 identifier | 树会丢一个 |
| 嵌套超过 `MaxDepth` | 小 payload 可驱动无界递归 |
| container 带 `Value`、leaf 带 `Children`、`Type` 超出 0–3 | 多余的一半会被静默丢弃 |

旧 encoder 产生的字节不变：同一棵树按 identifier 顺序，本包输出完全一致。
