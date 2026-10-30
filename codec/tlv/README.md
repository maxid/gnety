# codec/tlv

LwM2M TLV **wire format** only: the type nibble, the 8- or 16-bit identifier,
the inline or 8/16/24-bit length and the value or nested entries that follow.

Explicitly out of scope: the LwM2M object model, resource multiplicity and
permissions, registration/update state, execute semantics and observe state.
Those are the protocol library's job (glwm2m), which consumes this package.

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

Containers are `ObjectInstance` (type 00) and `MultipleResource` (type 10); leaves
are `ResourceValue` (type 11) and `ResourceInstance` (type 01). The type values
are the two-bit wire field, so a decoded type is a valid constant. `Type.Valid`
and `Type.IsContainer` classify a type without matching constants.

LwM2M uses two nesting levels (object instance over resources, multiple resource
over resource instances). The format, and this package, allow up to `MaxDepth`
(16) scopes.

## Contract

The package follows the same rules as the rest of gnety's codec kit
(`codec/wire`):

- The declared length of an entry is checked against the bytes its parent
  carries and against the datagram budget *before* anything is allocated from
  it, so a peer cannot make a decoder reserve memory it never sent.
- Decoded values are freshly allocated and never alias the input buffer, and
  `Marshal` copies values instead of retaining them, so both sides own their
  slices. `Marshal` also never reorders the slice it was given.
- A TLV payload is a datagram: it is complete when it arrives. A payload that
  ends inside an entry reports `wire.KindMalformed`, not `wire.ErrIncomplete` —
  there is nothing to wait for.
- Errors are classified through `codec/wire`: malformed bytes are
  `wire.KindMalformed`, a value or payload over a limit is `wire.KindTooLarge`
  and a type the format does not define is `wire.KindUnsupported`. Classify with
  `wire.KindOf`, never by matching strings.

## Determinism

`Marshal` emits the entries of one scope in ascending identifier order, at every
level. Duplicate identifiers in one scope are rejected, so that order is total:
the same tree always produces the same bytes, no matter in what order the caller
built or appended it. `Unmarshal` preserves the order the payload carries, the
same split `codec/coap` makes between a permissive parser and a canonical
encoder.

## Stricter than a permissive decoder

Three payloads that a naive decoder can read are rejected here, because their
meaning is ambiguous:

| Payload | Why it is rejected |
| --- | --- |
| two entries with the same identifier in one scope | the tree would lose one of them |
| more than `MaxDepth` nested scopes | a small payload would drive unbounded recursion |
| a container with a `Value`, a leaf with `Children`, a `Type` outside 0 to 3 | the surplus half would be silently dropped |

The bytes the old encoder produced are unchanged: for the same tree in
identifier order, this package emits exactly the same output.
