package mqtt

import (
	"encoding/binary"
	"unicode/utf8"

	"github.com/maxid/gnety/codec/wire"
)

// MQTT 5 property identifiers, from the specification table of property
// identifiers. The identifiers, not the property names, are what travels on the
// wire.
const (
	propPayloadFormat               byte = 0x01
	propMessageExpiry               byte = 0x02
	propContentType                 byte = 0x03
	propResponseTopic               byte = 0x08
	propCorrelationData             byte = 0x09
	propSubscriptionIdentifier      byte = 0x0b
	propSessionExpiryInterval       byte = 0x11
	propAssignedClientID            byte = 0x12
	propServerKeepAlive             byte = 0x13
	propAuthenticationMethod        byte = 0x15
	propAuthenticationData          byte = 0x16
	propRequestProblemInfo          byte = 0x17
	propWillDelayInterval           byte = 0x18
	propRequestResponseInfo         byte = 0x19
	propResponseInfo                byte = 0x1a
	propServerReference             byte = 0x1c
	propReasonString                byte = 0x1f
	propReceiveMaximum              byte = 0x21
	propTopicAliasMaximum           byte = 0x22
	propTopicAlias                  byte = 0x23
	propMaximumQoS                  byte = 0x24
	propRetainAvailable             byte = 0x25
	propUserProperty                byte = 0x26
	propMaximumPacketSize           byte = 0x27
	propWildcardSubscriptionAvail   byte = 0x28
	propSubscriptionIdentifierAvail byte = 0x29
	propSharedSubscriptionAvail     byte = 0x2a
)

// UserProperty is one user-defined key/value pair. MQTT 5 allows any number of
// them on most packets and leaves their meaning to the application.
type UserProperty struct {
	// Key is the UTF-8 property name.
	Key string
	// Value is the UTF-8 property value.
	Value string
}

// Properties is the MQTT 5 property set of one packet.
//
// A property is present when its field is set: a non-zero number, a non-empty
// string or slice, or a non-nil pointer. The zero value therefore means "no
// properties", and a property that carries its default value (a zero Session
// Expiry Interval for example) cannot be expressed. Decoding fills only the
// properties that were on the wire, so re-encoding a decoded packet reproduces
// them, except for such default-valued properties, which are dropped.
//
// MQTT 3.1.1 has no properties: the field is empty and ignored on those
// packets.
type Properties struct {
	// SessionExpiryInterval is the session lifetime in seconds (CONNECT,
	// CONNACK, DISCONNECT).
	SessionExpiryInterval uint32
	// ReceiveMaximum is the flow-control window: how many unacknowledged QoS 1
	// and 2 publications the sender accepts (CONNECT, CONNACK).
	ReceiveMaximum uint16
	// MaximumQoS is the highest QoS the server supports, 0 or 1 (CONNACK).
	MaximumQoS *byte
	// RetainAvailable reports whether the server supports retained messages
	// (CONNACK).
	RetainAvailable *bool
	// MaximumPacketSize is the largest packet the sender accepts (CONNECT,
	// CONNACK).
	MaximumPacketSize uint32
	// TopicAliasMaximum is the highest topic alias the sender accepts (CONNECT,
	// CONNACK).
	TopicAliasMaximum uint16
	// TopicAlias maps a publication to a topic the peer already knows
	// (PUBLISH).
	TopicAlias uint16
	// AssignedClientID is the client identifier the server chose for an empty
	// one (CONNACK).
	AssignedClientID string
	// MessageExpiryInterval is the lifetime of the publication in seconds
	// (PUBLISH, will).
	MessageExpiryInterval uint32
	// ContentType describes the payload format (PUBLISH, will).
	ContentType string
	// ResponseTopic names the topic a responder publishes to (PUBLISH, will).
	ResponseTopic string
	// CorrelationData identifies a request/response pair (PUBLISH, will).
	CorrelationData []byte
	// SubscriptionIdentifiers tag a publication with the subscriptions that
	// matched it (PUBLISH) or identify the SUBSCRIBE being registered.
	SubscriptionIdentifiers []uint32
	// UserProperties are the application-defined key/value pairs.
	UserProperties []UserProperty
	// WillDelayInterval is the pause between the connection ending and the
	// will being published (will).
	WillDelayInterval uint32
	// RequestProblemInfo asks the server for a Reason String on failures
	// (CONNECT).
	RequestProblemInfo *bool
	// RequestResponseInfo asks the server for Response Information (CONNECT).
	RequestResponseInfo *bool
	// ResponseInfo is the server's Response Information (CONNACK).
	ResponseInfo string
	// ServerReference points at another server to use (CONNACK, DISCONNECT).
	ServerReference string
	// ReasonString is human-readable detail for a reason code (CONNACK, the
	// PUBACK family, SUBACK, UNSUBACK, DISCONNECT).
	ReasonString string
	// AuthenticationMethod names the extended authentication method (CONNECT,
	// CONNACK, AUTH).
	AuthenticationMethod string
	// AuthenticationData carries the extended authentication payload (CONNECT,
	// CONNACK, AUTH).
	AuthenticationData []byte

	// PayloadFormat is the payload format indicator, 0 for unspecified bytes
	// and 1 for UTF-8 text (PUBLISH, will).
	PayloadFormat *byte
	// ServerKeepAlive overrides the keep-alive interval of a CONNECT (CONNACK).
	ServerKeepAlive *uint16
	// WildcardSubscriptionAvailable reports whether wildcard subscriptions are
	// supported (CONNACK).
	WildcardSubscriptionAvailable *bool
	// SubscriptionIdentifierAvailable reports whether subscription identifiers
	// are supported (CONNACK).
	SubscriptionIdentifierAvailable *bool
	// SharedSubscriptionAvailable reports whether shared subscriptions are
	// supported (CONNACK).
	SharedSubscriptionAvailable *bool
}

// propCtx is the set of packet contexts a property may appear in.
type propCtx uint16

// Property packet contexts. Each MQTT 5 packet type maps to at most one context,
// and the will property block (which is not a packet of its own) has one too.
const (
	ctxConnect propCtx = 1 << iota
	ctxConnack
	ctxPublish
	ctxWill
	ctxPuback // PUBACK, PUBREC, PUBREL, PUBCOMP
	ctxSubscribe
	ctxSuback
	ctxUnsubscribe
	ctxUnsuback
	ctxDisconnect
	ctxAuth
)

// propertyDef is the specification entry of one property identifier.
type propertyDef struct {
	// name is the specification name, used in error messages.
	name string
	// where is the set of packet contexts that may carry the property.
	where propCtx
	// multi marks properties that may appear more than once.
	multi bool
}

// propertyDefs maps every property identifier this package understands to its
// specification entry. Identifiers missing here are unknown and rejected.
var propertyDefs = map[byte]propertyDef{
	propPayloadFormat:               {"payload format indicator", ctxPublish | ctxWill, false},
	propMessageExpiry:               {"message expiry interval", ctxPublish | ctxWill, false},
	propContentType:                 {"content type", ctxPublish | ctxWill, false},
	propResponseTopic:               {"response topic", ctxPublish | ctxWill, false},
	propCorrelationData:             {"correlation data", ctxPublish | ctxWill, false},
	propSubscriptionIdentifier:      {"subscription identifier", ctxPublish | ctxSubscribe, true},
	propSessionExpiryInterval:       {"session expiry interval", ctxConnect | ctxConnack | ctxDisconnect, false},
	propAssignedClientID:            {"assigned client identifier", ctxConnack, false},
	propServerKeepAlive:             {"server keep alive", ctxConnack, false},
	propAuthenticationMethod:        {"authentication method", ctxConnect | ctxConnack | ctxAuth, false},
	propAuthenticationData:          {"authentication data", ctxConnect | ctxConnack | ctxAuth, false},
	propRequestProblemInfo:          {"request problem information", ctxConnect, false},
	propWillDelayInterval:           {"will delay interval", ctxWill, false},
	propRequestResponseInfo:         {"request response information", ctxConnect, false},
	propResponseInfo:                {"response information", ctxConnack, false},
	propServerReference:             {"server reference", ctxConnack | ctxDisconnect, false},
	propReasonString:                {"reason string", ctxConnack | ctxPuback | ctxSuback | ctxUnsuback | ctxDisconnect, false},
	propReceiveMaximum:              {"receive maximum", ctxConnect | ctxConnack, false},
	propTopicAliasMaximum:           {"topic alias maximum", ctxConnect | ctxConnack, false},
	propTopicAlias:                  {"topic alias", ctxPublish, false},
	propMaximumQoS:                  {"maximum QoS", ctxConnack, false},
	propRetainAvailable:             {"retain available", ctxConnack, false},
	propUserProperty:                {"user property", ctxConnect | ctxConnack | ctxPublish | ctxWill | ctxPuback | ctxSubscribe | ctxSuback | ctxUnsubscribe | ctxUnsuback | ctxDisconnect, true},
	propMaximumPacketSize:           {"maximum packet size", ctxConnect | ctxConnack, false},
	propWildcardSubscriptionAvail:   {"wildcard subscription available", ctxConnack, false},
	propSubscriptionIdentifierAvail: {"subscription identifier available", ctxConnack, false},
	propSharedSubscriptionAvail:     {"shared subscription available", ctxConnack, false},
}

// contextFor maps a control packet type to the property context it carries.
func contextFor(packetType byte) (propCtx, bool) {
	switch packetType {
	case TypeConnect:
		return ctxConnect, true
	case TypeConnack:
		return ctxConnack, true
	case TypePublish:
		return ctxPublish, true
	case TypePuback, TypePubrec, TypePubrel, TypePubcomp:
		return ctxPuback, true
	case TypeSubscribe:
		return ctxSubscribe, true
	case TypeSuback:
		return ctxSuback, true
	case TypeUnsubscribe:
		return ctxUnsubscribe, true
	case TypeUnsuback:
		return ctxUnsuback, true
	case TypeDisconnect:
		return ctxDisconnect, true
	case TypeAuth:
		return ctxAuth, true
	default:
		return 0, false
	}
}

// decodeProperties decodes one MQTT 5 property block: the property length
// followed by that many property bytes. It returns the properties and the bytes
// after the block.
//
// packetType selects the property table: a property that is unknown, repeated
// when the specification allows a single instance, or not defined for that
// packet type is reported as ErrMalformedPacket. The returned properties do not
// alias b.
//
// Only MQTT 5 packets carry properties; callers must not call this for a 3.1.1
// packet, whose bytes at this position mean something else. The will property
// block has no packet type of its own: Decode reads it as part of CONNECT.
func decodeProperties(b []byte, packetType byte) (Properties, []byte, error) {
	where := packetName(packetType)
	ctx, ok := contextFor(packetType)
	if !ok {
		return Properties{}, nil, malformed("%s carries no property block", where)
	}
	var p Properties
	n, rest, err := takeVarint(b)
	if err != nil {
		return p, nil, err
	}
	if len(rest) < n {
		return p, nil, malformed("%s declares %d property bytes but carries %d", where, n, len(rest))
	}
	// Copy the block so no decoded field aliases the caller's buffer.
	block := make([]byte, n)
	copy(block, rest[:n])
	p, err = decodePropertyBlock(block, ctx, where)
	if err != nil {
		return Properties{}, nil, err
	}
	return p, rest[n:], nil
}

// decodePropertyBlock parses the contents of one property block for the given
// context; the property length has already been consumed. where names the packet
// for error messages. String and binary fields alias b, so the caller owns it and
// must not reuse it while the Properties value lives.
func decodePropertyBlock(b []byte, ctx propCtx, where string) (Properties, error) {
	var p Properties
	block := b
	var err error

	var seen [256]bool
	for len(block) > 0 {
		id := block[0]
		block = block[1:]
		def, ok := propertyDefs[id]
		if !ok {
			return p, malformed("%s carries unknown property identifier 0x%02x", where, id)
		}
		if def.where&ctx == 0 {
			return p, malformed("%s property 0x%02x (%s) is not allowed here", where, id, def.name)
		}
		if def.multi && id == propSubscriptionIdentifier {
			// Only PUBLISH may repeat a subscription identifier; a SUBSCRIBE
			// carries exactly one.
			def.multi = ctx == ctxPublish
		}
		if !def.multi && seen[id] {
			return p, malformed("%s repeats property 0x%02x (%s)", where, id, def.name)
		}
		seen[id] = true

		switch id {
		case propPayloadFormat:
			var v byte
			if v, block, err = takeByte(block); err != nil {
				return p, err
			}
			if v > 1 {
				return p, malformed("%s payload format indicator %d", where, v)
			}
			p.PayloadFormat = &v
		case propMessageExpiry:
			if p.MessageExpiryInterval, block, err = takeUint32(block); err != nil {
				return p, err
			}
		case propContentType:
			if p.ContentType, block, err = takeString(block); err != nil {
				return p, err
			}
		case propResponseTopic:
			if p.ResponseTopic, block, err = takeString(block); err != nil {
				return p, err
			}
		case propCorrelationData:
			if p.CorrelationData, block, err = takeBinary(block); err != nil {
				return p, err
			}
		case propSubscriptionIdentifier:
			var id int
			if id, block, err = takeVarint(block); err != nil {
				return p, err
			}
			if id == 0 {
				return p, malformed("%s subscription identifier is zero", where)
			}
			p.SubscriptionIdentifiers = append(p.SubscriptionIdentifiers, uint32(id))
		case propSessionExpiryInterval:
			if p.SessionExpiryInterval, block, err = takeUint32(block); err != nil {
				return p, err
			}
		case propAssignedClientID:
			if p.AssignedClientID, block, err = takeString(block); err != nil {
				return p, err
			}
		case propServerKeepAlive:
			var v uint16
			if v, block, err = takeUint16(block); err != nil {
				return p, err
			}
			p.ServerKeepAlive = &v
		case propAuthenticationMethod:
			if p.AuthenticationMethod, block, err = takeString(block); err != nil {
				return p, err
			}
		case propAuthenticationData:
			if p.AuthenticationData, block, err = takeBinary(block); err != nil {
				return p, err
			}
		case propRequestProblemInfo:
			var v bool
			if v, block, err = takeBool(block); err != nil {
				return p, err
			}
			p.RequestProblemInfo = &v
		case propWillDelayInterval:
			if p.WillDelayInterval, block, err = takeUint32(block); err != nil {
				return p, err
			}
		case propRequestResponseInfo:
			var v bool
			if v, block, err = takeBool(block); err != nil {
				return p, err
			}
			p.RequestResponseInfo = &v
		case propResponseInfo:
			if p.ResponseInfo, block, err = takeString(block); err != nil {
				return p, err
			}
		case propServerReference:
			if p.ServerReference, block, err = takeString(block); err != nil {
				return p, err
			}
		case propReasonString:
			if p.ReasonString, block, err = takeString(block); err != nil {
				return p, err
			}
		case propReceiveMaximum:
			if p.ReceiveMaximum, block, err = takeUint16(block); err != nil {
				return p, err
			}
			if p.ReceiveMaximum == 0 {
				return p, malformed("%s receive maximum is zero", where)
			}
		case propTopicAliasMaximum:
			if p.TopicAliasMaximum, block, err = takeUint16(block); err != nil {
				return p, err
			}
		case propTopicAlias:
			if p.TopicAlias, block, err = takeUint16(block); err != nil {
				return p, err
			}
			if p.TopicAlias == 0 {
				return p, malformed("%s topic alias is zero", where)
			}
		case propMaximumQoS:
			var v byte
			if v, block, err = takeByte(block); err != nil {
				return p, err
			}
			if v > 1 {
				return p, malformed("%s maximum QoS %d", where, v)
			}
			p.MaximumQoS = &v
		case propRetainAvailable:
			var v bool
			if v, block, err = takeBool(block); err != nil {
				return p, err
			}
			p.RetainAvailable = &v
		case propUserProperty:
			var key, value string
			if key, block, err = takeString(block); err != nil {
				return p, err
			}
			if value, block, err = takeString(block); err != nil {
				return p, err
			}
			p.UserProperties = append(p.UserProperties, UserProperty{Key: key, Value: value})
		case propMaximumPacketSize:
			if p.MaximumPacketSize, block, err = takeUint32(block); err != nil {
				return p, err
			}
			if p.MaximumPacketSize == 0 {
				return p, malformed("%s maximum packet size is zero", where)
			}
		case propWildcardSubscriptionAvail:
			var v bool
			if v, block, err = takeBool(block); err != nil {
				return p, err
			}
			p.WildcardSubscriptionAvailable = &v
		case propSubscriptionIdentifierAvail:
			var v bool
			if v, block, err = takeBool(block); err != nil {
				return p, err
			}
			p.SubscriptionIdentifierAvailable = &v
		case propSharedSubscriptionAvail:
			var v bool
			if v, block, err = takeBool(block); err != nil {
				return p, err
			}
			p.SharedSubscriptionAvailable = &v
		}
	}
	return p, nil
}

// encodeProperties appends one MQTT 5 property block (property length plus the
// property bytes) to dst.
//
// packetType selects the property table, exactly as in decodeProperties: a
// property that is not defined for that packet type is an error rather than a
// silently dropped field. Properties are written in ascending identifier order,
// which is the order the specification recommends, so the same Properties value
// always produces the same bytes. The will property block has no packet type of
// its own: Encode writes it as part of CONNECT.
func encodeProperties(dst []byte, p Properties, packetType byte) ([]byte, error) {
	where := packetName(packetType)
	ctx, ok := contextFor(packetType)
	if !ok {
		return nil, malformed("%s carries no property block", where)
	}
	return appendProperties(dst, p, ctx, where)
}

// appendProperties appends the property block for ctx to dst.
func appendProperties(dst []byte, p Properties, ctx propCtx, where string) ([]byte, error) {
	allow := func(id byte) error {
		def, ok := propertyDefs[id]
		if !ok {
			return malformed("%s carries unknown property identifier 0x%02x", where, id)
		}
		if def.where&ctx == 0 {
			return malformed("%s property 0x%02x (%s) is not allowed here", where, id, def.name)
		}
		return nil
	}

	block := make([]byte, 0, 32)
	var err error

	if p.PayloadFormat != nil {
		if err := allow(propPayloadFormat); err != nil {
			return nil, err
		}
		if *p.PayloadFormat > 1 {
			return nil, malformed("%s payload format indicator %d", where, *p.PayloadFormat)
		}
		block = append(block, propPayloadFormat, *p.PayloadFormat)
	}
	if p.MessageExpiryInterval != 0 {
		if err := allow(propMessageExpiry); err != nil {
			return nil, err
		}
		block = appendUint32Property(block, propMessageExpiry, p.MessageExpiryInterval)
	}
	if p.ContentType != "" {
		if err := allow(propContentType); err != nil {
			return nil, err
		}
		if block, err = appendStringProperty(block, propContentType, p.ContentType); err != nil {
			return nil, err
		}
	}
	if p.ResponseTopic != "" {
		if err := allow(propResponseTopic); err != nil {
			return nil, err
		}
		if block, err = appendStringProperty(block, propResponseTopic, p.ResponseTopic); err != nil {
			return nil, err
		}
	}
	if p.CorrelationData != nil {
		if err := allow(propCorrelationData); err != nil {
			return nil, err
		}
		if block, err = appendBinaryProperty(block, propCorrelationData, p.CorrelationData); err != nil {
			return nil, err
		}
	}
	if len(p.SubscriptionIdentifiers) > 0 {
		if err := allow(propSubscriptionIdentifier); err != nil {
			return nil, err
		}
		for _, id := range p.SubscriptionIdentifiers {
			if id == 0 || id > MaxRemainingLength {
				return nil, malformed("%s subscription identifier %d", where, id)
			}
			block = append(block, propSubscriptionIdentifier)
			block = wire.AppendVarint(block, uint64(id))
		}
	}
	if p.SessionExpiryInterval != 0 {
		if err := allow(propSessionExpiryInterval); err != nil {
			return nil, err
		}
		block = appendUint32Property(block, propSessionExpiryInterval, p.SessionExpiryInterval)
	}
	if p.AssignedClientID != "" {
		if err := allow(propAssignedClientID); err != nil {
			return nil, err
		}
		if block, err = appendStringProperty(block, propAssignedClientID, p.AssignedClientID); err != nil {
			return nil, err
		}
	}
	if p.ServerKeepAlive != nil {
		if err := allow(propServerKeepAlive); err != nil {
			return nil, err
		}
		block = appendUint16Property(block, propServerKeepAlive, *p.ServerKeepAlive)
	}
	if p.AuthenticationMethod != "" {
		if err := allow(propAuthenticationMethod); err != nil {
			return nil, err
		}
		if block, err = appendStringProperty(block, propAuthenticationMethod, p.AuthenticationMethod); err != nil {
			return nil, err
		}
	}
	if p.AuthenticationData != nil {
		if err := allow(propAuthenticationData); err != nil {
			return nil, err
		}
		if block, err = appendBinaryProperty(block, propAuthenticationData, p.AuthenticationData); err != nil {
			return nil, err
		}
	}
	if p.RequestProblemInfo != nil {
		if err := allow(propRequestProblemInfo); err != nil {
			return nil, err
		}
		block = append(block, propRequestProblemInfo, boolByte(*p.RequestProblemInfo))
	}
	if p.WillDelayInterval != 0 {
		if err := allow(propWillDelayInterval); err != nil {
			return nil, err
		}
		block = appendUint32Property(block, propWillDelayInterval, p.WillDelayInterval)
	}
	if p.RequestResponseInfo != nil {
		if err := allow(propRequestResponseInfo); err != nil {
			return nil, err
		}
		block = append(block, propRequestResponseInfo, boolByte(*p.RequestResponseInfo))
	}
	if p.ResponseInfo != "" {
		if err := allow(propResponseInfo); err != nil {
			return nil, err
		}
		if block, err = appendStringProperty(block, propResponseInfo, p.ResponseInfo); err != nil {
			return nil, err
		}
	}
	if p.ServerReference != "" {
		if err := allow(propServerReference); err != nil {
			return nil, err
		}
		if block, err = appendStringProperty(block, propServerReference, p.ServerReference); err != nil {
			return nil, err
		}
	}
	if p.ReasonString != "" {
		if err := allow(propReasonString); err != nil {
			return nil, err
		}
		if block, err = appendStringProperty(block, propReasonString, p.ReasonString); err != nil {
			return nil, err
		}
	}
	if p.ReceiveMaximum != 0 {
		if err := allow(propReceiveMaximum); err != nil {
			return nil, err
		}
		block = appendUint16Property(block, propReceiveMaximum, p.ReceiveMaximum)
	}
	if p.TopicAliasMaximum != 0 {
		if err := allow(propTopicAliasMaximum); err != nil {
			return nil, err
		}
		block = appendUint16Property(block, propTopicAliasMaximum, p.TopicAliasMaximum)
	}
	if p.TopicAlias != 0 {
		if err := allow(propTopicAlias); err != nil {
			return nil, err
		}
		block = appendUint16Property(block, propTopicAlias, p.TopicAlias)
	}
	if p.MaximumQoS != nil {
		if err := allow(propMaximumQoS); err != nil {
			return nil, err
		}
		if *p.MaximumQoS > 1 {
			return nil, malformed("%s maximum QoS %d", where, *p.MaximumQoS)
		}
		block = append(block, propMaximumQoS, *p.MaximumQoS)
	}
	if p.RetainAvailable != nil {
		if err := allow(propRetainAvailable); err != nil {
			return nil, err
		}
		block = append(block, propRetainAvailable, boolByte(*p.RetainAvailable))
	}
	if len(p.UserProperties) > 0 {
		if err := allow(propUserProperty); err != nil {
			return nil, err
		}
		for _, up := range p.UserProperties {
			block = append(block, propUserProperty)
			if block, err = appendUTF16(block, up.Key); err != nil {
				return nil, err
			}
			if block, err = appendUTF16(block, up.Value); err != nil {
				return nil, err
			}
		}
	}
	if p.MaximumPacketSize != 0 {
		if err := allow(propMaximumPacketSize); err != nil {
			return nil, err
		}
		block = appendUint32Property(block, propMaximumPacketSize, p.MaximumPacketSize)
	}
	if p.WildcardSubscriptionAvailable != nil {
		if err := allow(propWildcardSubscriptionAvail); err != nil {
			return nil, err
		}
		block = append(block, propWildcardSubscriptionAvail, boolByte(*p.WildcardSubscriptionAvailable))
	}
	if p.SubscriptionIdentifierAvailable != nil {
		if err := allow(propSubscriptionIdentifierAvail); err != nil {
			return nil, err
		}
		block = append(block, propSubscriptionIdentifierAvail, boolByte(*p.SubscriptionIdentifierAvailable))
	}
	if p.SharedSubscriptionAvailable != nil {
		if err := allow(propSharedSubscriptionAvail); err != nil {
			return nil, err
		}
		block = append(block, propSharedSubscriptionAvail, boolByte(*p.SharedSubscriptionAvailable))
	}

	if len(block) > MaxRemainingLength {
		return nil, tooLarge("%s property block of %d bytes", where, len(block))
	}
	dst = AppendRemainingLength(dst, len(block))
	return append(dst, block...), nil
}

// takeByte reads one byte and returns the remaining bytes.
func takeByte(b []byte) (byte, []byte, error) {
	if len(b) < 1 {
		return 0, nil, malformed("truncated property value")
	}
	return b[0], b[1:], nil
}

// takeUint16 reads a big-endian uint16 and returns the remaining bytes.
func takeUint16(b []byte) (uint16, []byte, error) {
	if len(b) < 2 {
		return 0, nil, malformed("truncated property value")
	}
	return binary.BigEndian.Uint16(b), b[2:], nil
}

// takeUint32 reads a big-endian uint32 and returns the remaining bytes.
func takeUint32(b []byte) (uint32, []byte, error) {
	if len(b) < 4 {
		return 0, nil, malformed("truncated property value")
	}
	return binary.BigEndian.Uint32(b), b[4:], nil
}

// takeBool reads a 0/1 property byte and returns the remaining bytes.
func takeBool(b []byte) (bool, []byte, error) {
	v, rest, err := takeByte(b)
	if err != nil {
		return false, nil, err
	}
	if v > 1 {
		return false, nil, malformed("boolean property value %d", v)
	}
	return v == 1, rest, nil
}

// takeString reads a length-prefixed UTF-8 string and returns the remaining
// bytes.
func takeString(b []byte) (string, []byte, error) {
	raw, rest, err := takeBinary(b)
	if err != nil {
		return "", nil, err
	}
	s := string(raw)
	if err := checkString(s); err != nil {
		return "", nil, err
	}
	return s, rest, nil
}

// takeBinary reads a length-prefixed binary value and returns the remaining
// bytes. The returned slice aliases b.
func takeBinary(b []byte) ([]byte, []byte, error) {
	if len(b) < 2 {
		return nil, nil, malformed("truncated length prefix")
	}
	n := int(binary.BigEndian.Uint16(b))
	if len(b) < 2+n {
		return nil, nil, malformed("declared length %d exceeds the %d bytes available", n, len(b)-2)
	}
	return b[2 : 2+n], b[2+n:], nil
}

// takeVarint reads a variable byte integer and returns the remaining bytes.
//
// It applies the shortest-form rule of the MQTT encoding (a padded value such as
// 0x80 0x00 is malformed) and the four-byte width limit; a four-byte payload
// cannot exceed MaxRemainingLength, so no separate range check is needed.
func takeVarint(b []byte) (int, []byte, error) {
	value := 0
	for i := 0; i < wire.MaxVarintBytes; i++ {
		if len(b) < 1 {
			return 0, nil, malformed("truncated variable byte integer")
		}
		encoded := b[0]
		b = b[1:]
		if i > 0 && encoded == 0 {
			return 0, nil, malformed("variable byte integer is not minimally encoded")
		}
		value |= int(encoded&0x7f) << (7 * i)
		if encoded&0x80 == 0 {
			return value, b, nil
		}
	}
	return 0, nil, malformed("variable byte integer is wider than %d bytes", wire.MaxVarintBytes)
}

// appendUint16 appends a uint16 property.
func appendUint16Property(dst []byte, id byte, v uint16) []byte {
	return append(dst, id, byte(v>>8), byte(v))
}

// appendUint32 appends a uint32 property.
func appendUint32Property(dst []byte, id byte, v uint32) []byte {
	return append(dst, id, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

// appendString appends a length-prefixed UTF-8 string property. id is the
// property identifier, or 0 for the second half of a user property, where the
// caller has already written the identifier.
func appendStringProperty(dst []byte, id byte, s string) ([]byte, error) {
	if err := checkString(s); err != nil {
		return nil, err
	}
	if len(s) > 0xffff {
		return nil, tooLarge("string of %d bytes exceeds 65535", len(s))
	}
	if id != 0 {
		dst = append(dst, id)
	}
	dst = append(dst, byte(len(s)>>8), byte(len(s)))
	return append(dst, s...), nil
}

// appendBinary appends a length-prefixed binary property.
func appendBinaryProperty(dst []byte, id byte, b []byte) ([]byte, error) {
	if len(b) > 0xffff {
		return nil, tooLarge("binary property of %d bytes exceeds 65535", len(b))
	}
	dst = append(dst, id, byte(len(b)>>8), byte(len(b)))
	return append(dst, b...), nil
}

// boolByte encodes a boolean property value.
func boolByte(v bool) byte {
	if v {
		return 1
	}
	return 0
}

// checkString validates the parts of the MQTT UTF-8 rules that fit the wire
// format: the bytes must be valid UTF-8 and must not contain U+0000.
func checkString(s string) error {
	if !utf8.ValidString(s) {
		return malformed("malformed UTF-8 string")
	}
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			return malformed("UTF-8 string contains U+0000")
		}
	}
	return nil
}
