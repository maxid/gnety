# codec/modbus

Modbus TCP **wire format** only: the MBAP header, the typed PDUs for the
function codes the platform uses, and the exception response.

Explicitly out of scope: transaction matching, retries, polling schedules,
register-address maps, device models and mapping an exception code to a device
error. Those belong to the Modbus client library (gmodbus) that consumes this
package.

## Two layers

`Frame`, `Marshal`, `Read` and `Codec` are the original MBAP framing over raw
PDU bytes and are unchanged:

```go
frame, err := modbus.Read(reader, 0)     // transaction id, unit id, PDU bytes
raw, err := frame.Marshal()              // MBAP header plus PDU
```

The typed layer adds one struct per function code, one decode path and one
encode path:

```go
req := &modbus.ReadHoldingRegistersRequest{Address: 0x006B, Quantity: 3}
pdu, err := req.PDU()                    // 03 00 6B 00 03
err = modbus.EncodePDU(writer, req)      // the same bytes, one Write call

msg, err := modbus.DecodePDU(pdu)         // request orientation
msg, err = modbus.DecodeResponse(req, responsePDU)
```

`TypedFrame` and its `Marshal`/`Frame` methods carry a typed message through the
MBAP header, and `ReadTyped`/`ReadTypedResponse` read one typed frame from a
stream:

```go
frame, err := modbus.ReadTypedResponse(reader, 0, req)
```

## Messages

| Function | Request | Response |
| --- | --- | --- |
| 0x01 Read Coils | `ReadCoilsRequest` | `ReadCoilsResponse` (`Bits []bool`) |
| 0x02 Read Discrete Inputs | `ReadDiscreteInputsRequest` | `ReadDiscreteInputsResponse` (`Bits []bool`) |
| 0x03 Read Holding Registers | `ReadHoldingRegistersRequest` | `ReadHoldingRegistersResponse` (`Registers []uint16`) |
| 0x04 Read Input Registers | `ReadInputRegistersRequest` | `ReadInputRegistersResponse` (`Registers []uint16`) |
| 0x05 Write Single Coil | `WriteSingleCoil` | `WriteSingleCoil` (echo) |
| 0x06 Write Single Register | `WriteSingleRegister` | `WriteSingleRegister` (echo) |
| 0x0F Write Multiple Coils | `WriteMultipleCoilsRequest` | `WriteMultipleCoilsResponse` |
| 0x10 Write Multiple Registers | `WriteMultipleRegistersRequest` | `WriteMultipleRegistersResponse` |

A `ReadCoilsRequest`, `ReadDiscreteInputsRequest`,
`ReadHoldingRegistersRequest` or `ReadInputRegistersRequest` carries the starting
`Address` and the `Quantity`. A multiple-write request carries `Address` and the
`Values`; its quantity and byte count are derived from the slice.

Exception responses decode to `*Exception` with the request function code and an
`ExceptionCode`. `Exception` implements `error`: `errors.As` recovers the typed
value and `errors.Is(err, modbus.ErrException)` matches every code. An exception
response is a valid reply, so decoding it returns a message and a nil error.

## PDU layout decisions

- Bit-oriented data is packed one bit per coil or input, least significant bit
  of the first data byte first. Bits past the declared quantity in the final
  byte are encoded as zero; a decoder ignores them and `DecodeResponse` trims
  the result to the requested quantity.
- Register values are 16-bit big-endian words.
- A read request is five bytes: function code, starting address, quantity.
- A read response is a function code, a one-byte count and the data. For reads
  of bits the count is `ceil(quantity/8)`; for reads of registers it is
  `2*quantity`.
- A write-multiple request is a function code, address, quantity, a one-byte
  count and the data, with the same count rules as a read response.
- A write-multiple response echoes the address and quantity in five bytes.
- `DecodePDU` decodes requests (and exception responses). A read response has no
  direction flag and its byte count would be misread as part of the address, so
  responses are decoded by `DecodeResponse`, which takes the request they answer.

## Validation

Every quantity bound from the specification is enforced: 1 to 2000 bits and 1
to 125 registers for reads, 1 to 1968 coils and 1 to 123 registers for multiple
writes. A starting address is a `uint16`, so it is always in 0 to 0xFFFF, and a
register value is a `uint16` by type. A write-single-coil PDU must carry 0xFF00
or 0x0000. Byte counts must match the value count, and a PDU with trailing or
missing bytes is rejected.

`DecodeResponse` also enforces the response contract: the function code must
match the request, a read response must carry exactly the requested quantity, a
write-single response must echo the address and value, and a write-multiple
response must report the requested address and quantity. An exception response
is accepted for the request function code whatever its exception code.

A truncated PDU is `wire.KindMalformed`, not `wire.KindIncomplete`: the MBAP
length field already delimited it. An unknown function code is
`wire.KindUnsupported`, and a PDU longer than `MaxPDU` is `wire.KindTooLarge`.
