# codec/coap

CoAP (RFC 7252) **wire-format codec** for gnety.

- Message encode/decode: version/type/code/message ID/token/options/payload
- Option delta/length encoding (including extended 13/14 forms)
- Common option/code constants
- **No** retransmission, observe, block-wise or registration state machines

Those state machines belong to protocol libraries (gcoap/glwm2m) built on gnety.

```go
msg := coap.Message{Type: coap.Confirmable, Code: coap.CodeGET, MessageID: 1,
    Options: []coap.Option{{Number: coap.OptionURIPath, Value: []byte("sensors")}}}
raw, _ := msg.Marshal()
got, _ := coap.Parse(raw)
```
