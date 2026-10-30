package websocket

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"
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
		applyMask(maskedPayload, key, 0)
		frame = append(frame, maskedPayload...)
	} else {
		frame = append(frame, payload...)
	}
	return frame
}

func testWebSocketReader(input []byte, client bool, maxMessage int) (*Conn, *bytes.Buffer) {
	var output bytes.Buffer
	c := &Conn{
		reader: bufio.NewReader(bytes.NewReader(input)), writer: bufio.NewWriter(&output),
		maskWrites: client, done: make(chan struct{}), activePings: make(map[string]chan struct{}),
	}
	c.maxMessage.Store(uint64(maxMessage))
	return c, &output
}

func TestWebSocketRFCMaskedHelloVector(t *testing.T) {
	// RFC 6455 section 5.7: masked text frame containing "Hello".
	wire := []byte{0x81, 0x85, 0x37, 0xfa, 0x21, 0x3d, 0x7f, 0x9f, 0x4d, 0x51, 0x58}
	r := bufio.NewReader(bytes.NewReader(wire))
	h, err := readFrameHeader(r)
	if err != nil {
		t.Fatal(err)
	}
	if !h.fin || h.opcode != textFrame || !h.masked || h.length != 5 {
		t.Fatalf("unexpected header: %+v", h)
	}
	payload := make([]byte, h.length)
	if _, err := io.ReadFull(r, payload); err != nil {
		t.Fatal(err)
	}
	applyMask(payload, h.maskingKey, 0)
	if string(payload) != "Hello" {
		t.Fatalf("payload = %q", payload)
	}
}

func TestWebSocketFragmentedMessageWithPing(t *testing.T) {
	var wire []byte
	wire = append(wire, rawWebSocketFrame(false, textFrame, []byte("hel"), true)...)
	wire = append(wire, rawWebSocketFrame(true, pingFrame, []byte("?"), true)...)
	wire = append(wire, rawWebSocketFrame(true, continuationFrame, []byte("lo"), true)...)
	conn, output := testWebSocketReader(wire, false, 1024)
	message := make([]byte, 5)
	if _, err := io.ReadFull(conn, message); err != nil {
		t.Fatal(err)
	}
	if string(message) != "hello" {
		t.Fatalf("message = %q", message)
	}
	h, err := readFrameHeader(bufio.NewReader(output))
	if err != nil {
		t.Fatal(err)
	}
	if h.opcode != pongFrame || h.masked {
		t.Fatalf("unexpected pong header: %+v", h)
	}
}

func TestWebSocketClientWriteIsMaskedBinary(t *testing.T) {
	conn, output := testWebSocketReader(nil, true, 1024)
	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(output)
	h, err := readFrameHeader(r)
	if err != nil {
		t.Fatal(err)
	}
	if !h.fin || h.opcode != binaryFrame || !h.masked {
		t.Fatalf("unexpected header: %+v", h)
	}
	payload := make([]byte, h.length)
	if _, err := io.ReadFull(r, payload); err != nil {
		t.Fatal(err)
	}
	applyMask(payload, h.maskingKey, 0)
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
		{name: "unmasked client data", wire: rawWebSocketFrame(true, binaryFrame, []byte("x"), false)},
		{name: "masked server data", wire: rawWebSocketFrame(true, binaryFrame, []byte("x"), true), client: true},
		{name: "fragmented ping", wire: rawWebSocketFrame(false, pingFrame, nil, true)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, output := testWebSocketReader(tt.wire, tt.client, 1024)
			_, err := conn.Read(make([]byte, 1))
			var protocolErr *protocolError
			if !errors.As(err, &protocolErr) || protocolErr.status != 1002 {
				t.Fatalf("expected protocol error 1002, got %v", err)
			}
			h, readErr := readFrameHeader(bufio.NewReader(output))
			if readErr != nil || h.opcode != closeFrame {
				t.Fatalf("expected close frame, header=%+v err=%v", h, readErr)
			}
		})
	}
}

func TestWebSocketFragmentedMessageLimit(t *testing.T) {
	var wire []byte
	wire = append(wire, rawWebSocketFrame(false, binaryFrame, []byte("123"), true)...)
	wire = append(wire, rawWebSocketFrame(true, continuationFrame, []byte("45"), true)...)
	conn, output := testWebSocketReader(wire, false, 4)
	buf := make([]byte, 3)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Read(make([]byte, 2)); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("expected ErrFrameTooLarge, got %v", err)
	}
	r := bufio.NewReader(output)
	h, err := readFrameHeader(r)
	if err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, h.length)
	if _, err := io.ReadFull(r, payload); err != nil {
		t.Fatal(err)
	}
	if h.opcode != closeFrame || binary.BigEndian.Uint16(payload[:2]) != 1009 {
		t.Fatalf("expected close 1009, header=%+v payload=%v", h, payload)
	}
}

func TestWebSocketRejectsInvalidUTF8AcrossFragments(t *testing.T) {
	var wire []byte
	wire = append(wire, rawWebSocketFrame(false, textFrame, []byte{0xe2}, true)...)
	wire = append(wire, rawWebSocketFrame(true, continuationFrame, []byte{0x28, 0xa1}, true)...)
	conn, _ := testWebSocketReader(wire, false, 1024)
	_, err := io.ReadAll(conn)
	var protocolErr *protocolError
	if !errors.As(err, &protocolErr) || protocolErr.status != 1007 {
		t.Fatalf("expected UTF-8 error 1007, got %v", err)
	}
}

func TestWebSocketCloseEcho(t *testing.T) {
	wire := rawWebSocketFrame(true, closeFrame, []byte{0x03, 0xe8}, true)
	conn, output := testWebSocketReader(wire, false, 1024)
	if _, err := conn.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF, got %v", err)
	}
	r := bufio.NewReader(output)
	h, err := readFrameHeader(r)
	if err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, h.length)
	if _, err := io.ReadFull(r, payload); err != nil {
		t.Fatal(err)
	}
	if h.opcode != closeFrame || binary.BigEndian.Uint16(payload[:2]) != 1000 {
		t.Fatalf("unexpected close response: %+v %v", h, payload)
	}
}

func TestUTF8ValidatorDFACases(t *testing.T) {
	valid := []struct {
		name  string
		parts [][]byte
	}{
		{"ascii", [][]byte{[]byte("hello")}},
		{"euro-split", [][]byte{{0xe2}, {0x82, 0xac}}},
		{"emoji-split", [][]byte{{0xf0, 0x9f}, {0x98, 0x80}}},
	}
	for _, tt := range valid {
		var v utf8Validator
		for _, p := range tt.parts {
			if !v.check(p) {
				t.Fatalf("%s: rejected valid UTF-8", tt.name)
			}
		}
		if !v.finish() {
			t.Fatalf("%s: incomplete valid UTF-8", tt.name)
		}
	}
	invalid := []struct {
		name string
		data []byte
	}{
		{"overlong", []byte{0xc0, 0xaf}},
		{"surrogate", []byte{0xed, 0xa0, 0x80}},
		{"bad-continuation", []byte{0xe2, 0x28, 0xa1}},
		{"truncated", []byte{0xe2, 0x82}},
	}
	for _, tt := range invalid {
		var v utf8Validator
		ok := v.check(tt.data)
		if ok && v.finish() {
			t.Fatalf("%s: accepted invalid UTF-8", tt.name)
		}
	}
}

func TestCloseErrorAndHandshake(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	client := newConn(a, bufio.NewReadWriter(bufio.NewReader(a), bufio.NewWriter(a)), true, 4096, "")
	server := newConn(b, bufio.NewReadWriter(bufio.NewReader(b), bufio.NewWriter(b)), false, 4096, "")

	serverErr := make(chan error, 1)
	go func() {
		_, err := server.Read(make([]byte, 1))
		serverErr <- err
	}()
	go func() {
		_, _ = client.Read(make([]byte, 1)) // drive client close wait
	}()
	if err := client.CloseWithStatus(StatusCodeGoingAway, "bye"); err != nil {
		t.Fatalf("close: %v", err)
	}
	select {
	case err := <-serverErr:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("server error does not unwrap EOF: %v", err)
		}
		var ce *CloseError
		if !errors.As(err, &ce) || ce.Code != StatusCodeGoingAway || ce.Reason != "bye" {
			t.Fatalf("server close error = %#v", ce)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not finish close handshake")
	}
}
