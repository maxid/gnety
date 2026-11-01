package modbus

import (
	"bufio"
	"bytes"
	"errors"
	"testing"

	"github.com/maxid/gnety/codec/wire"
)

func TestCRC16Vectors(t *testing.T) {
	// The first two are the specification's own examples; the third is the
	// classic 17 03 00 6B 00 03 frame from the application protocol guide.
	cases := []struct {
		data []byte
		want uint16
	}{
		{[]byte{0x01, 0x03, 0x00, 0x00, 0x00, 0x0a}, 0xcdc5},
		{[]byte{0x01, 0x03, 0x00, 0x00, 0x00, 0x01}, 0x0a84},
		{[]byte{0x11, 0x03, 0x00, 0x6b, 0x00, 0x03}, 0x8776},
		{nil, 0xffff},
	}
	for _, c := range cases {
		if got := CRC16(c.data); got != c.want {
			t.Errorf("CRC16(% x) = %#04x, want %#04x", c.data, got, c.want)
		}
	}
}

func TestRTUMarshalParse(t *testing.T) {
	f := RTUFrame{UnitID: 0x11, PDU: []byte{0x03, 0x00, 0x6b, 0x00, 0x03}}
	raw, err := f.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := []byte{0x11, 0x03, 0x00, 0x6b, 0x00, 0x03, 0x76, 0x87}
	if !bytes.Equal(raw, want) {
		t.Fatalf("Marshal = % x, want % x", raw, want)
	}
	got, err := RTUOptions{}.Parse(raw, 0)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.UnitID != f.UnitID || !bytes.Equal(got.PDU, f.PDU) {
		t.Fatalf("Parse = %#v, want %#v", got, f)
	}
	if got.Function() != FuncReadHoldingRegisters {
		t.Fatalf("Function = %#02x", got.Function())
	}
	// The returned PDU must not alias the input buffer.
	raw[2] = 0xff
	if got.PDU[1] == 0xff {
		t.Fatal("Parse aliased the input buffer")
	}
}

func TestRTUMarshalNoCRC(t *testing.T) {
	f := RTUFrame{UnitID: 1, PDU: []byte{0x03, 0x00, 0x00, 0x00, 0x01}}
	raw, err := RTUOptions{NoCRC: true}.Marshal(f)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if len(raw) != 6 {
		t.Fatalf("no-CRC frame is %d bytes, want 6", len(raw))
	}
	got, err := RTUOptions{NoCRC: true}.Parse(raw, 0)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !bytes.Equal(got.PDU, f.PDU) {
		t.Fatalf("PDU = % x", got.PDU)
	}
}

func TestRTUReaderFraming(t *testing.T) {
	readReq := &ReadHoldingRegistersRequest{Address: 0x006b, Quantity: 3}
	readResp := &ReadHoldingRegistersResponse{Registers: []uint16{1, 2, 3}}
	writeReq := &WriteMultipleRegistersRequest{Address: 0x0010, Values: []uint16{7, 8}}
	writeResp := &WriteMultipleRegistersResponse{Address: 0x0010, Quantity: 2}
	maskReq := rawPDU(t, 0x16, 0x00, 0x10, 0x00, 0xff, 0x00, 0x0f)
	keywords := rawPDU(t, 0x17,
		0x00, 0x00, 0x00, 0x01, // read address, quantity
		0x00, 0x10, 0x00, 0x02, // write address, quantity
		0x04, 0x00, 0x07, 0x00, 0x08) // byte count and two registers
	rwResp := rawPDU(t, 0x17, 0x02, 0x00, 0x01)
	deviceResp := rawPDU(t, 0x2b, MEIReadDeviceIdentification, 0x01, 0x01, 0x00, 0x00, 0x02,
		0x00, 0x03, 'a', 'b', 'c',
		0x01, 0x02, 'x', 'y')

	cases := []struct {
		name     string
		pdu      []byte
		response bool
		req      Message
	}{
		{"read request", mustPDU(t, readReq), false, nil},
		{"read response", mustPDU(t, readResp), true, readReq},
		{"write multiple request", mustPDU(t, writeReq), false, nil},
		{"write multiple response", mustPDU(t, writeResp), true, writeReq},
		{"mask write request", maskReq, false, nil},
		{"mask write response", maskReq, true, nil},
		{"read write request", keywords, false, nil},
		{"read write response", rwResp, true, nil},
		{"device identification response", deviceResp, true, nil},
		{"exception response", []byte{0x83, 0x02}, true, readReq},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw, err := RTUFrame{UnitID: 1, PDU: c.pdu}.Marshal()
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			r := bufio.NewReader(bytes.NewReader(raw))
			var got RTUFrame
			if c.response {
				got, err = RTUOptions{}.ReadResponse(r, 0, nil, c.req)
			} else {
				got, err = RTUOptions{}.Read(r, 0, nil)
			}
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if !bytes.Equal(got.PDU, c.pdu) {
				t.Fatalf("PDU = % x, want % x", got.PDU, c.pdu)
			}
			if r.Buffered() != 0 {
				t.Fatalf("reader still holds %d bytes", r.Buffered())
			}
		})
	}
}

func TestRTUReaderNoCRC(t *testing.T) {
	pdu := mustPDU(t, &ReadCoilsRequest{Address: 1, Quantity: 9})
	raw, err := RTUOptions{NoCRC: true}.Marshal(RTUFrame{UnitID: 3, PDU: pdu})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got, err := (RTUOptions{NoCRC: true}).Read(bufio.NewReader(bytes.NewReader(raw)), 0, nil)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(got.PDU, pdu) {
		t.Fatalf("PDU = % x", got.PDU)
	}
}

func TestRTUTypedRead(t *testing.T) {
	req := &ReadInputRegistersRequest{Address: 0x0008, Quantity: 2}
	raw, err := RTUFrame{UnitID: 5, PDU: mustPDU(t, req)}.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	frame, err := (RTUOptions{}).ReadTypedRequest(bufio.NewReader(bytes.NewReader(raw)), 0, nil)
	if err != nil {
		t.Fatalf("ReadTypedRequest: %v", err)
	}
	got, ok := frame.Message.(*ReadInputRegistersRequest)
	if !ok || got.Address != 0x0008 || got.Quantity != 2 || frame.UnitID != 5 {
		t.Fatalf("decoded %#v unit %d", frame.Message, frame.UnitID)
	}

	resp := &ReadInputRegistersResponse{Registers: []uint16{11, 12}}
	raw, err = RTUFrame{UnitID: 5, PDU: mustPDU(t, resp)}.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	frame, err = (RTUOptions{}).ReadTypedResponse(bufio.NewReader(bytes.NewReader(raw)), 0, nil, req)
	if err != nil {
		t.Fatalf("ReadTypedResponse: %v", err)
	}
	gotResp, ok := frame.Message.(*ReadInputRegistersResponse)
	if !ok || len(gotResp.Registers) != 2 || gotResp.Registers[1] != 12 {
		t.Fatalf("decoded %#v", frame.Message)
	}
}

func TestRTUReaderErrors(t *testing.T) {
	raw, err := RTUFrame{UnitID: 1, PDU: mustPDU(t, &ReadHoldingRegistersRequest{Address: 0, Quantity: 1})}.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	t.Run("incomplete", func(t *testing.T) {
		for _, n := range []int{0, 1, 2, 3, len(raw) - 1} {
			_, err := (RTUOptions{}).Read(bufio.NewReader(bytes.NewReader(raw[:n])), 0, nil)
			if !errors.Is(err, wire.ErrIncomplete) {
				t.Fatalf("prefix of %d bytes: err = %v, want incomplete", n, err)
			}
		}
	})

	t.Run("bad CRC", func(t *testing.T) {
		bad := append([]byte(nil), raw...)
		bad[len(bad)-1] ^= 0xff
		if _, err := (RTUOptions{}).Parse(bad, 0); !errors.Is(err, wire.ErrMalformed) {
			t.Fatalf("Parse: err = %v, want malformed", err)
		}
		if _, err := (RTUOptions{}).Read(bufio.NewReader(bytes.NewReader(bad)), 0, nil); !errors.Is(err, wire.ErrMalformed) {
			t.Fatalf("Read: err = %v, want malformed", err)
		}
	})

	t.Run("too short", func(t *testing.T) {
		if _, err := (RTUOptions{}).Parse([]byte{0x01, 0x03}, 0); !errors.Is(err, wire.ErrMalformed) {
			t.Fatalf("Parse: err = %v, want malformed", err)
		}
	})

	t.Run("too large", func(t *testing.T) {
		if _, err := (RTUOptions{}).Parse(make([]byte, RTUMaxFrameLen+1), 0); !errors.Is(err, wire.ErrTooLarge) {
			t.Fatalf("Parse: err = %v, want too large", err)
		}
	})

	t.Run("PDU budget", func(t *testing.T) {
		if _, err := (RTUOptions{}).Parse(raw, 3); !errors.Is(err, wire.ErrTooLarge) {
			t.Fatalf("Parse: err = %v, want too large", err)
		}
	})

	t.Run("unsupported function", func(t *testing.T) {
		raw, err := RTUFrame{UnitID: 1, PDU: []byte{0x64, 0x00}}.Marshal()
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if _, err := (RTUOptions{}).Read(bufio.NewReader(bytes.NewReader(raw)), 0, nil); !errors.Is(err, wire.ErrUnsupported) {
			t.Fatalf("Read: err = %v, want unsupported", err)
		}
	})

	t.Run("empty PDU", func(t *testing.T) {
		if _, err := (RTUFrame{}).Marshal(); !errors.Is(err, wire.ErrMalformed) {
			t.Fatalf("Marshal: err = %v, want malformed", err)
		}
	})
}

func TestRTURegistryFramesCustomFunction(t *testing.T) {
	// A custom function code with a fixed five-byte PDU (address + quantity).
	custom := Functions{
		0x64: {
			RequestLength: func(frame []byte) (int, error) {
				if len(frame) < 2 {
					return 0, nil
				}
				return 5, nil
			},
			ResponseLength: func(req Message, frame []byte) (int, error) {
				if len(frame) < 2 {
					return 0, nil
				}
				return 2, nil
			},
		},
	}
	reqPDU := rawPDU(t, 0x64, 0x00, 0x01, 0x00, 0x02)
	raw, err := RTUFrame{UnitID: 9, PDU: reqPDU}.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got, err := (RTUOptions{}).Read(bufio.NewReader(bytes.NewReader(raw)), 0, custom)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(got.PDU, reqPDU) {
		t.Fatalf("PDU = % x", got.PDU)
	}

	respPDU := rawPDU(t, 0x64, 0x02)
	raw, err = RTUFrame{UnitID: 9, PDU: respPDU}.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got, err = (RTUOptions{}).ReadResponse(bufio.NewReader(bytes.NewReader(raw)), 0, custom, nil)
	if err != nil {
		t.Fatalf("ReadResponse: %v", err)
	}
	if !bytes.Equal(got.PDU, respPDU) {
		t.Fatalf("PDU = % x", got.PDU)
	}
}

func TestFunctionsDecodePDU(t *testing.T) {
	builtin, err := DecodePDU([]byte{0x03, 0x00, 0x00, 0x00, 0x01})
	if err != nil {
		t.Fatalf("DecodePDU: %v", err)
	}
	if _, ok := builtin.(*ReadHoldingRegistersRequest); !ok {
		t.Fatalf("built-in decode = %T", builtin)
	}

	var called bool
	custom := Functions{
		FuncReadHoldingRegisters: {
			DecodeRequest: func(pdu []byte) (Message, error) {
				called = true
				return &ReadHoldingRegistersRequest{Address: 0x1111, Quantity: 1}, nil
			},
		},
		0x64: {
			DecodeRequest: func(pdu []byte) (Message, error) {
				return &ReadHoldingRegistersRequest{Address: 0x2222, Quantity: 1}, nil
			},
		},
	}
	msg, err := custom.DecodePDU([]byte{0x03, 0x00, 0x00, 0x00, 0x01})
	if err != nil {
		t.Fatalf("custom decode: %v", err)
	}
	if !called || msg.(*ReadHoldingRegistersRequest).Address != 0x1111 {
		t.Fatalf("registry override not used: %#v", msg)
	}
	if _, err := custom.DecodePDU([]byte{0x64, 0x00}); err != nil {
		t.Fatalf("custom code decode: %v", err)
	}
	// An exception response never reaches a registered decoder.
	exc, err := custom.DecodePDU([]byte{0x83, 0x02})
	if err != nil {
		t.Fatalf("exception decode: %v", err)
	}
	if _, ok := exc.(*Exception); !ok {
		t.Fatalf("exception decode = %T", exc)
	}

	// A registered response decoder wins over the built-in one.
	custom[FuncReadHoldingRegisters] = Function{
		DecodeResponse: func(req Message, pdu []byte) (Message, error) {
			return &ReadHoldingRegistersResponse{Registers: []uint16{0x9999}}, nil
		},
	}
	msg, err = custom.DecodeResponse(builtin, []byte{0x03, 0x02, 0x00, 0x01})
	if err != nil {
		t.Fatalf("custom response decode: %v", err)
	}
	if msg.(*ReadHoldingRegistersResponse).Registers[0] != 0x9999 {
		t.Fatalf("response override not used: %#v", msg)
	}
}

func FuzzParseRTU(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x01, 0x03, 0x00, 0x00, 0x00, 0x01, 0x84, 0x0a})
	f.Add([]byte{0x11, 0x03, 0x00, 0x6b, 0x00, 0x03, 0x76, 0x87})
	f.Add([]byte{0x01, 0x83, 0x02, 0xc0, 0xf1})
	f.Add([]byte{0x01, 0x2b, 0x0e, 0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x01, 'x', 0x00, 0x00})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = (RTUOptions{}).Parse(data, 0)
		_, _ = (RTUOptions{NoCRC: true}).Parse(data, 0)
	})
}

func mustPDU(t *testing.T, m Message) []byte {
	t.Helper()
	pdu, err := m.PDU()
	if err != nil {
		t.Fatalf("PDU: %v", err)
	}
	return pdu
}

func rawPDU(t *testing.T, b ...byte) []byte {
	t.Helper()
	return b
}
