# codec/mqtt

MQTT 3.1.1 / 5 **wire format** only:
fixed-header + remaining-length framing, UTF-8 strings, binary blobs,
variable-byte integers, typed control packets and MQTT 5 property blocks.

Explicitly out of scope: QoS in-flight state, retained messages, sessions, topic
aliases, subscription matching and any device/product semantics. Those live in
the MQTT protocol library (gmqx) that uses this package.

## Typed codec

```go
d := &mqtt.Decoder{MaxPacketSize: 1 << 20} // zero Version means 3.1.1
msg, err := d.Decode(reader)               // *mqtt.Connect, *mqtt.Publish, ...

err = mqtt.Encode(writer, msg)             // exactly one Write per packet
```

`Decoder.Decode` reads one control packet per call:

- the remaining length is checked against `MaxPacketSize` before any body buffer
  exists, so a peer cannot make the decoder reserve memory;
- reserved fixed-header flags, QoS > 2, DUP with QoS 0, packet id 0 and a body
  that does not consume its declared remaining length are all rejected;
- `Decoder.Version` is refreshed from CONNECT, which is how a server switches
  to the MQTT 5 layout; a client sets `Version` before decoding CONNACK. AUTH is
  MQTT 5 only, so decoding or encoding it as 3.1.1 reports
  `wire.KindUnsupported`;
- truncated input reports `wire.ErrIncomplete` (and still matches `io.EOF`),
  garbage reports `ErrMalformedPacket`, and `wire.KindOf` classifies both.

`Encode` takes the message's `Version()`: MQTT 5 packets carry property blocks
and optional reason codes, MQTT 3.1.1 packets carry neither and drop the
`Properties` field and the MQTT 5 subscription options.

`Codec` wraps the same functions in gnety's stream codec shape
(`Decode(*bufio.Reader) (any, error)`, `Encode(io.Writer, any) error`).

Messages are plain structs: `Connect`, `Connack`, `Publish`, `Puback`, `Pubrec`,
`Pubrel`, `Pubcomp`, `Subscribe`, `Suback`, `Unsubscribe`, `Unsuback`, `Pingreq`,
`Pingresp`, `Disconnect`, `Auth`. AUTH is the MQTT 5 extended-authentication
step: a zero-length body is success with no properties, a one-byte body is a
bare reason code (0x00 success, 0x18 continue authentication, 0x19
re-authenticate) and a longer body adds a property block, which may carry only
the authentication method and authentication data. `Properties` models the MQTT 5
property set and is decoded/encoded as part of each packet; unknown, repeated or
packet-incompatible property identifiers are rejected.

## Framing helpers

```go
header, body, err := mqtt.ReadPacket(reader, maxPacketSize)
err = mqtt.WritePacket(writer, 0x30, body)
```

These stay for callers that parse their own packet bodies.

## Reuse path for PUBLISH

A broker reads the same packet shape millions of times, so a PUBLISH can also be
decoded into a caller-supplied buffer instead of into new memory:

```go
var buf []byte // per connection: grown once, then reused
view, err := mqtt.ReadPublishInto(reader, mqtt.Version311, maxPacketSize, buf)
switch {
case errors.Is(err, mqtt.ErrNotPublish):
	msg, err := d.Decode(reader) // nothing was consumed: read it the typed way
case err != nil:
	// the errors are Decode's own
}
use(view.Topic, view.Payload, view.QoS, view.PacketID, view.Properties)
buf = view.Buffer
```

`ReadPublishInto` applies exactly the validation `Decoder.Decode` applies to a
PUBLISH — remaining length against the limit before any byte is read, QoS > 2,
DUP with QoS 0, the UTF-8 and U+0000 rules of the topic, packet id 0, no property
block at 3.1.1, and the property table for a 5.0 block — and returns the same
errors. The topic-name rules a server enforces (a non-empty name without
wildcards, an MQTT 5 topic alias standing in for an empty one) stay with the
protocol layer, exactly as they do for `Decode`; that is why the topic comes back
as bytes, so a caller can check it before allocating a string.

Lifetime: `Topic` and `Payload` are slices of `Buffer` — the `dst` that came in,
or a larger buffer when `dst` was too small — so they are valid only until the
next read that uses the same buffer. Keep `view.Buffer` and pass it back in; it
only grows. A caller that keeps the payload beyond the next read copies it.

Allocation: an MQTT 3.1.1 PUBLISH, and an MQTT 5 PUBLISH with an empty property
block, decode without allocating. An MQTT 5 block that carries properties does
allocate: it is copied out of the buffer and decoded by the same property table
`Decode` uses, so the property rules have one implementation, not two.
