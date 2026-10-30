# codec/coap

CoAP (RFC 7252) **wire format** only: the four-byte header, token, option stream
and payload of one datagram, plus typed option accessors, code constants and a
bounded decoder.

Explicitly out of scope: retransmission, message-id deduplication, exchange
lifetimes, observe registries and block-wise transfer engines. Those are
protocol state machines and live in the CoAP protocol library (gcoap) and
`glwm2m` that use this package.

## Two layers

`Parse` and `Marshal` are the original whole-datagram pair and stay
byte-compatible with the code that predates the typed layer (gcoap uses them):

```go
msg, err := coap.Parse(datagram) // Message or a classified error
raw, err := msg.Marshal()        // options sorted by number
```

`Decoder`, `ParseLimit` and `Encode` are the typed entry points a server wants:

```go
msg, err := (&coap.Decoder{MaxSize: 1152}).Decode(datagram) // size checked first
msg, err := coap.ParseLimit(datagram, 1152)                 // same check, no receiver
raw, err := coap.Encode(msg)                                // strict validation
```

- `Decode`/`ParseLimit` reject an oversized datagram before any field is copied.
- A version other than 1 reports `wire.KindUnsupported`; reserved token lengths 9 to 15,
  a payload marker with no payload, reserved option nibbles and truncated
  datagrams report `wire.KindMalformed`; an oversized datagram reports
  `wire.KindTooLarge`. Every error also satisfies `errors.Is(err, coap.ErrMalformed)`.
- A datagram is a complete frame: a datagram that ends inside a field is
  malformed, not incomplete, because there are no more bytes to wait for.
- Decoded tokens, option values and payloads are freshly allocated and never
  alias the caller's datagram buffer.
- `Encode` additionally requires options sorted by number and rejects a repeated
  option that is not repeatable, and an option value longer than its definition
  allows. `Marshal` keeps the permissive behavior: it sorts before encoding and
  leaves repetition to the caller.
- `Codec` wraps the same functions in gnety's `DatagramCodec` shape
  (`Decode([]byte) (any, error)`, `Encode(any) ([]byte, error)`).

## Typed messages and options

`Message` carries `Version`, `Type`, `Code`, `MessageID`, `Token`, `Options` and
`Payload`. `Options` is wire-faithful: a number and the raw value bytes, so an
extension the package does not model survives a round trip unchanged.

The typed accessors read and write the same list and keep it sorted:

| Option | Accessors |
| --- | --- |
| Uri-Path (11) | `URIPath`, `SetURIPath` (one value per segment) |
| Uri-Query (15) | `URIQuery`, `SetURIQuery` (one value per argument) |
| Content-Format (12) | `ContentFormat`, `SetContentFormat` (`TextPlain`, `ApplicationJSON`, `ApplicationLinkFormat`, `ApplicationOctetStream`, ...) |
| Accept (17) | `Accept`, `SetAccept` |
| Max-Age (14) | `MaxAge`, `SetMaxAge` |
| ETag (4) | `ETags`, `AddETag`, `SetETag` |
| If-Match (1) | `IfMatches`, `AddIfMatch` |
| If-None-Match (5) | `IfNoneMatch`, `SetIfNoneMatch` |
| Location-Path (8) | `LocationPath`, `SetLocationPath` |
| Location-Query (20) | `LocationQuery`, `SetLocationQuery` |
| Observe (6) | `Observe`, `SetObserve` (`ObserveRegister`, `ObserveDeregister`) |
| Block1 (27), Block2 (23) | `Block1`/`SetBlock1`, `Block2`/`SetBlock2` |
| Size1 (60), Size2 (28) | `Size1`/`SetSize1`, `Size2`/`SetSize2` |

Unknown or unmodelled numbers go through `OptionValue`, `SetOption` and
`AddOption`, or through `Options` directly.

`Block` models a Block1/Block2 value:

```go
b := coap.Block{Num: 2, More: true, SizeExp: 2}
raw, err := b.Encode()            // 3-byte option value: M bit, SZX, block number
b, err = coap.DecodeBlock(raw)    // accepts the 0 to 3 byte forms of RFC 7959
size := b.Size()                  // 1 << (SizeExp+4), 0 for a reserved SZX
```

SZX 7 is reserved and rejected; the block number field is 20 bits wide.

Code constants cover the methods a server routes (`CodeGET`, `CodePOST`,
`CodePUT`, `CodeDELETE`, `CodeFETCH`, `CodePATCH`) and the responses it sends
(`CodeCreated` through `CodeContent`, `CodeBadRequest` through
`CodeUnsupportedContentFormat`, `CodeInternalServerError` through
`CodeServiceUnavailable`). `Code.IsRequest` and `Code.IsResponse` classify a
code without matching constants.
