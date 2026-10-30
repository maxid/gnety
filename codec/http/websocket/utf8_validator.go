package websocket

// Incremental UTF-8 validator using Björn Höhrmann's DFA:
// https://bjoern.hoehrmann.de/utf-8/decoder/dfa/
// The transition tables are also used by Apache-2.0 licensed implementations;
// see the project's internal source-analysis record for provenance.

const (
	utf8Accept = 0
	utf8Reject = 12
)

var utf8Types = [256]byte{
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
	9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9,
	7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7,
	7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7,
	8, 8, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2,
	2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2,
	10, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 4, 3, 3,
	11, 6, 6, 6, 5, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8,
}

var utf8States = [108]byte{
	0, 12, 24, 36, 60, 96, 84, 12, 12, 12, 48, 72, 12, 12, 12, 12,
	12, 12, 12, 12, 12, 12, 12, 12, 12, 0, 12, 12, 12, 12, 12, 0,
	12, 0, 12, 12, 12, 24, 12, 12, 12, 12, 12, 24, 12, 24, 12, 12,
	12, 12, 12, 12, 12, 12, 12, 24, 12, 12, 12, 12, 12, 24, 12, 12,
	12, 12, 12, 12, 12, 24, 12, 12, 12, 12, 12, 12, 12, 12, 12, 36,
	12, 36, 12, 12, 12, 36, 12, 12, 12, 12, 12, 36, 12, 36, 12, 12,
	12, 36, 12, 12, 12, 12, 12, 12, 12, 12, 12, 12,
}

type utf8Validator struct {
	state    uint32
	codep    uint32
	checking bool
}

func (v *utf8Validator) reset() {
	v.state = utf8Accept
	v.codep = 0
	v.checking = false
}

func (v *utf8Validator) check(p []byte) bool {
	v.checking = true
	for _, b := range p {
		typ := uint32(utf8Types[b])
		if v.state != utf8Accept {
			v.codep = uint32(b&0x3f) | v.codep<<6
		} else {
			v.codep = uint32(0xff>>typ) & uint32(b)
		}
		v.state = uint32(utf8States[v.state+typ])
		if v.state == utf8Reject {
			v.checking = false
			return false
		}
	}
	return true
}

func (v *utf8Validator) finish() bool {
	v.checking = false
	v.codep = 0
	if v.state != utf8Accept {
		v.state = utf8Accept
		return false
	}
	return true
}
