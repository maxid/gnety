# codec/mqtt

MQTT 3.1.1 / 5 **wire format** only:
fixed-header + remaining-length framing, UTF-8 strings, binary blobs and
variable-byte integers.

Explicitly out of scope: QoS, retained messages, will, sessions, topic aliases,
subscriptions and any device/product semantics. Those live in the MQTT protocol
library (gmqx) that uses this package.

```go
header, body, err := mqtt.ReadPacket(reader, maxPacketSize)
err = mqtt.WritePacket(writer, 0x30, body)
```
