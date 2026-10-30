package gnety

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

func rawWebSocketFrame(fin bool, opcode byte, payload []byte, masked bool) []byte {
	first := opcode
	if fin {
		first |= 0x80
	}
	frame := []byte{first}
	maskBit := byte(0)
	if masked {
		maskBit = 0x80
	}
	switch {
	case len(payload) <= 125:
		frame = append(frame, maskBit|byte(len(payload)))
	case len(payload) <= 65535:
		frame = append(frame, maskBit|126, byte(len(payload)>>8), byte(len(payload)))
	default:
		frame = append(frame, maskBit|127)
		var extended [8]byte
		binary.BigEndian.PutUint64(extended[:], uint64(len(payload)))
		frame = append(frame, extended[:]...)
	}
	if masked {
		key := [4]byte{0x37, 0xfa, 0x21, 0x3d}
		frame = append(frame, key[:]...)
		maskedPayload := append([]byte(nil), payload...)
		applyWebSocketMask(maskedPayload, key, 0)
		frame = append(frame, maskedPayload...)
	} else {
		frame = append(frame, payload...)
	}
	return frame
}

func testWebSocketReader(input []byte, client bool, maxMessage int) (*webSocketConn, *bytes.Buffer) {
	var output bytes.Buffer
	return &webSocketConn{
		reader: bufio.NewReader(bytes.NewReader(input)), writer: bufio.NewWriter(&output),
		maskWrites: client, maxMessage: uint64(maxMessage),
	}, &output
}

func TestWebSocketRFCMaskedHelloVector(t *testing.T) {
	// RFC 6455 section 5.7: masked text frame containing "Hello".
	wire := []byte{0x81, 0x85, 0x37, 0xfa, 0x21, 0x3d, 0x7f, 0x9f, 0x4d, 0x51, 0x58}
	r := bufio.NewReader(bytes.NewReader(wire))
	h, err := readWebSocketFrameHeader(r)
	if err != nil {
		t.Fatal(err)
	}
	if !h.fin || h.opcode != webSocketTextFrame || !h.masked || h.length != 5 {
		t.Fatalf("unexpected header: %+v", h)
	}
	payload := make([]byte, h.length)
	if _, err := io.ReadFull(r, payload); err != nil {
		t.Fatal(err)
	}
	applyWebSocketMask(payload, h.maskingKey, 0)
	if string(payload) != "Hello" {
		t.Fatalf("payload = %q", payload)
	}
}

func TestWebSocketFragmentedMessageWithPing(t *testing.T) {
	var wire []byte
	wire = append(wire, rawWebSocketFrame(false, webSocketTextFrame, []byte("hel"), true)...)
	wire = append(wire, rawWebSocketFrame(true, webSocketPingFrame, []byte("?"), true)...)
	wire = append(wire, rawWebSocketFrame(true, webSocketContinuationFrame, []byte("lo"), true)...)
	conn, output := testWebSocketReader(wire, false, 1024)
	message := make([]byte, 5)
	if _, err := io.ReadFull(conn, message); err != nil {
		t.Fatal(err)
	}
	if string(message) != "hello" {
		t.Fatalf("message = %q", message)
	}
	h, err := readWebSocketFrameHeader(bufio.NewReader(output))
	if err != nil {
		t.Fatal(err)
	}
	if h.opcode != webSocketPongFrame || h.masked {
		t.Fatalf("unexpected pong header: %+v", h)
	}
}

func TestWebSocketClientWriteIsMaskedBinary(t *testing.T) {
	conn, output := testWebSocketReader(nil, true, 1024)
	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(output)
	h, err := readWebSocketFrameHeader(r)
	if err != nil {
		t.Fatal(err)
	}
	if !h.fin || h.opcode != webSocketBinaryFrame || !h.masked {
		t.Fatalf("unexpected header: %+v", h)
	}
	payload := make([]byte, h.length)
	if _, err := io.ReadFull(r, payload); err != nil {
		t.Fatal(err)
	}
	applyWebSocketMask(payload, h.maskingKey, 0)
	if string(payload) != "hello" {
		t.Fatalf("payload = %q", payload)
	}
}

func TestWebSocketRejectsInvalidMaskingAndControlFrame(t *testing.T) {
	tests := []struct {
		name   string
		wire   []byte
		client bool
	}{
		{name: "unmasked client data", wire: rawWebSocketFrame(true, webSocketBinaryFrame, []byte("x"), false)},
		{name: "masked server data", wire: rawWebSocketFrame(true, webSocketBinaryFrame, []byte("x"), true), client: true},
		{name: "fragmented ping", wire: rawWebSocketFrame(false, webSocketPingFrame, nil, true)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, output := testWebSocketReader(tt.wire, tt.client, 1024)
			_, err := conn.Read(make([]byte, 1))
			var protocolErr *webSocketProtocolError
			if !errors.As(err, &protocolErr) || protocolErr.status != 1002 {
				t.Fatalf("expected protocol error 1002, got %v", err)
			}
			h, readErr := readWebSocketFrameHeader(bufio.NewReader(output))
			if readErr != nil || h.opcode != webSocketCloseFrame {
				t.Fatalf("expected close frame, header=%+v err=%v", h, readErr)
			}
		})
	}
}

func TestWebSocketFragmentedMessageLimit(t *testing.T) {
	var wire []byte
	wire = append(wire, rawWebSocketFrame(false, webSocketBinaryFrame, []byte("123"), true)...)
	wire = append(wire, rawWebSocketFrame(true, webSocketContinuationFrame, []byte("45"), true)...)
	conn, output := testWebSocketReader(wire, false, 4)
	buf := make([]byte, 3)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Read(make([]byte, 2)); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("expected ErrFrameTooLarge, got %v", err)
	}
	r := bufio.NewReader(output)
	h, err := readWebSocketFrameHeader(r)
	if err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, h.length)
	if _, err := io.ReadFull(r, payload); err != nil {
		t.Fatal(err)
	}
	if h.opcode != webSocketCloseFrame || binary.BigEndian.Uint16(payload[:2]) != 1009 {
		t.Fatalf("expected close 1009, header=%+v payload=%v", h, payload)
	}
}

func TestWebSocketRejectsInvalidUTF8AcrossFragments(t *testing.T) {
	var wire []byte
	wire = append(wire, rawWebSocketFrame(false, webSocketTextFrame, []byte{0xe2}, true)...)
	wire = append(wire, rawWebSocketFrame(true, webSocketContinuationFrame, []byte{0x28, 0xa1}, true)...)
	conn, _ := testWebSocketReader(wire, false, 1024)
	_, err := io.ReadAll(conn)
	var protocolErr *webSocketProtocolError
	if !errors.As(err, &protocolErr) || protocolErr.status != 1007 {
		t.Fatalf("expected UTF-8 error 1007, got %v", err)
	}
}

func TestWebSocketCloseEcho(t *testing.T) {
	wire := rawWebSocketFrame(true, webSocketCloseFrame, []byte{0x03, 0xe8}, true)
	conn, output := testWebSocketReader(wire, false, 1024)
	if _, err := conn.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF, got %v", err)
	}
	r := bufio.NewReader(output)
	h, err := readWebSocketFrameHeader(r)
	if err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, h.length)
	if _, err := io.ReadFull(r, payload); err != nil {
		t.Fatal(err)
	}
	if h.opcode != webSocketCloseFrame || binary.BigEndian.Uint16(payload[:2]) != 1000 {
		t.Fatalf("unexpected close response: %+v %v", h, payload)
	}
}
