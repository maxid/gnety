package gnety

import (
	"errors"
	"fmt"
)

// EncodeBCD encodes a decimal string into packed BCD. An odd number of digits
// is padded with a leading zero nibble.
func EncodeBCD(s string) ([]byte, error) {
	if s == "" {
		return nil, errors.New("gnety: empty BCD input")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return nil, fmt.Errorf("gnety: invalid BCD digit %q", r)
		}
	}
	if len(s)%2 == 1 {
		s = "0" + s
	}
	out := make([]byte, len(s)/2)
	for i := 0; i < len(out); i++ {
		hi := s[2*i] - '0'
		lo := s[2*i+1] - '0'
		out[i] = hi<<4 | lo
	}
	return out, nil
}

// DecodeBCD decodes packed BCD into a decimal string.
func DecodeBCD(b []byte) (string, error) {
	out := make([]byte, 0, len(b)*2)
	for _, v := range b {
		hi, lo := v>>4, v&0x0f
		if hi > 9 || lo > 9 {
			return "", fmt.Errorf("gnety: invalid BCD byte 0x%02x", v)
		}
		out = append(out, '0'+hi, '0'+lo)
	}
	return string(out), nil
}
