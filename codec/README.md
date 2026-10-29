# Protocol codec kit

Wire-format codecs for reusable protocols. **Codec packages contain no
protocol state machines and no business semantics.**

## Layout (planned)

```
codec/
  mqtt/       MQTT 3.1.1/5 packet encode/decode (CONNECT/PUBLISH/SUBSCRIBE/...)
  coap/       CoAP message/option encode/decode
  http/       HTTP/1.1 request/response codec
  websocket/  WebSocket frame codec (when not using net/http)
```

## Contract

- Stream protocols implement `gnety.Codec` (`Decode(*bufio.Reader)`, `Encode(io.Writer, any)`).
- Datagram protocols implement `gnety.DatagramCodec`.
- Codec packages must not import protocol libraries or application code.
- Protocol state machines (MQTT QoS, CoAP observe, LwM2M registration) live in
  the protocol libraries that depend on gnety.

Frame codecs (`LengthFieldCodec`, `DelimiterCodec`, `LineCodec`,
`FixedLengthCodec`) currently live in the root package for compatibility; new
protocol codecs should live under `codec/<protocol>`.
