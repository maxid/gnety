# codec/modbus

Modbus **wire format** only: the MBAP header of Modbus TCP, the RTU frame of
Modbus serial, the typed PDUs for the function codes the platform uses, the
exception response and a registry for vendor function codes.

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

`Parse` is the whole-packet entry point for a datagram transport: it requires one
complete MBAP frame and rejects trailing bytes.

## RTU framing

An RTU application data unit is the unit id, the PDU and a CRC16. There is no
length field, so the reader derives the frame length from the function code (and
the byte count inside variable-size PDUs):

```go
frame := modbus.RTUFrame{UnitID: 1, PDU: pdu}
raw, err := frame.Marshal()                    // unit id, PDU, CRC16 (low byte first)

frame, err = modbus.RTUOptions{}.Parse(raw, 0) // whole datagram
req, err := modbus.RTUOptions{}.ReadTypedRequest(reader, 0, nil)
resp, err := modbus.RTUOptions{}.ReadTypedResponse(reader, 0, nil, request)
```

`CRC16` implements the specification polynomial (0xA001) and returns the value in
host order; the wire order is low byte then high byte. A truncated frame is
`wire.ErrIncomplete`; a bad CRC, a frame longer than 256 bytes or a function code
without a framing rule is rejected before a PDU is allocated.

`RTUOptions{NoCRC: true}` omits and skips the checksum. It exists for gateways
that carry RTU frames without a CRC over TCP or UDP; serial RTU must keep it.

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
| 0x16 Mask Write Register | `MaskWriteRegister` | `MaskWriteRegister` (echo) |
| 0x17 Read/Write Multiple Registers | `ReadWriteMultipleRegistersRequest` | `ReadWriteMultipleRegistersResponse` |
| 0x2B/0x0E Read Device Identification | `ReadDeviceIdentificationRequest` | `ReadDeviceIdentificationResponse` |

A `ReadCoilsRequest`, `ReadDiscreteInputsRequest`,
`ReadHoldingRegistersRequest` or `ReadInputRegistersRequest` carries the starting
`Address` and the `Quantity`. A multiple-write request carries `Address` and the
`Values`; its quantity and byte count are derived from the slice.

A read/write-multiple request carries both ranges and the write values; its write
quantity and byte count are derived from the slice, and the write side is capped
at 121 registers because the read side shares the PDU. A read-device-
identification response carries the objects the server returned plus
`MoreFollows`/`NextObjectID`, so a caller can continue with another request.

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

## Vendor function codes

`Functions` registers a non-standard function code, or overrides one of the
built-in codes, without changing this package:

```go
funcs := modbus.Functions{
    0x64: {
        DecodeRequest:  func(pdu []byte) (modbus.Message, error) { ... },
        DecodeResponse: func(req modbus.Message, pdu []byte) (modbus.Message, error) { ... },
        RequestLength:  func(frame []byte) (int, error) { ... },  // RTU streams only
        ResponseLength: func(req modbus.Message, frame []byte) (int, error) { ... },
    },
}
msg, err := funcs.DecodePDU(pdu)            // registry first, then built-in
msg, err = funcs.DecodeResponse(req, pdu)
```

The registry is consulted before the built-in decoder, so a vendor that redefines
a standard function code's payload can override it. An exception response is
always decoded by the built-in decoder. `RequestLength` and `ResponseLength`
return the PDU length from the bytes read so far (0 asks for one more byte); they
are only needed when the code runs over RTU serial or RTU-over-TCP, which have no
length field. A `nil` hook leaves that direction unsupported on those transports;
MBAP and whole-datagram paths do not need it.

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
