package gnety

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testCert(t *testing.T) (certFile, keyFile string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile
}

func echoLineServer(t *testing.T, cfg Config) *Server {
	t.Helper()
	cfg.Handler = HandlerFuncs{Message: func(s *Session, msg any) { _ = s.Send(msg) }}
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Stop(context.Background()) })
	return srv
}

func TestTLSListener(t *testing.T) {
	certFile, keyFile := testCert(t)
	srv := echoLineServer(t, Config{
		Network: "tcp-tls",
		Addr:    "127.0.0.1:0",
		Codec:   LineCodec{},
		TLS:     TLSConfig{CertFile: certFile, KeyFile: keyFile},
	})
	conn, err := tls.Dial("tcp", srv.Addr(), &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || line != "hello\n" {
		t.Fatalf("line=%q err=%v", line, err)
	}
}

func dialWebSocketStream(t *testing.T, network, addr, path, protocol string, tlsConfig TLSConfig) net.Conn {
	t.Helper()
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialWebSocket(context.Background(), dialer, ClientConfig{
		Network: network, Addr: addr, Path: path, Subprotocol: protocol,
		TLS: tlsConfig, Timeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	return conn
}

func TestWebSocketListener(t *testing.T) {
	srv := echoLineServer(t, Config{
		Network:     "ws",
		Addr:        "127.0.0.1:0",
		Path:        "/echo",
		Subprotocol: "gnety",
		Codec:       LineCodec{},
	})
	conn := dialWebSocketStream(t, "ws", srv.Addr(), "/echo", "gnety", TLSConfig{})
	defer conn.Close()
	if _, err := conn.Write([]byte("ws\n")); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || line != "ws\n" {
		t.Fatalf("line=%q err=%v", line, err)
	}
}

func TestWSSListener(t *testing.T) {
	certFile, keyFile := testCert(t)
	srv := echoLineServer(t, Config{
		Network: "wss",
		Addr:    "127.0.0.1:0",
		Path:    "/echo",
		Codec:   LineCodec{},
		TLS:     TLSConfig{CertFile: certFile, KeyFile: keyFile},
	})
	conn := dialWebSocketStream(t, "wss", srv.Addr(), "/echo", "", TLSConfig{CAFile: certFile})
	defer conn.Close()
	if _, err := conn.Write([]byte("wss\n")); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || line != "wss\n" {
		t.Fatalf("line=%q err=%v", line, err)
	}
}

func TestWebSocketClientEcho(t *testing.T) {
	for _, network := range []string{"ws", "wss"} {
		t.Run(network, func(t *testing.T) {
			serverCfg := Config{
				Network: network, Addr: "127.0.0.1:0", Path: "/echo",
				Subprotocol: "gnety", Codec: LineCodec{},
			}
			clientCfg := ClientConfig{Subprotocol: "gnety", Codec: LineCodec{}}
			if network == "wss" {
				certFile, keyFile := testCert(t)
				serverCfg.TLS = TLSConfig{CertFile: certFile, KeyFile: keyFile}
				clientCfg.TLS = TLSConfig{CAFile: certFile}
			}
			srv := echoLineServer(t, serverCfg)
			clientCfg.Network = network
			clientCfg.Addr = srv.Addr()
			clientCfg.Path = "/echo"
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			cli, err := Dial(ctx, clientCfg)
			if err != nil {
				t.Fatal(err)
			}
			defer cli.Close()
			if cli.LocalAddr() == nil {
				t.Fatal("expected underlying local address")
			}
			if err := cli.Send([]byte("client-" + network)); err != nil {
				t.Fatal(err)
			}
			msg, err := cli.Receive(ctx)
			if err != nil || string(msg.([]byte)) != "client-"+network {
				t.Fatalf("msg=%v err=%v", msg, err)
			}
		})
	}
}

func TestWebSocketSubprotocolRequired(t *testing.T) {
	srv := echoLineServer(t, Config{
		Network:     "ws",
		Addr:        "127.0.0.1:0",
		Path:        "/echo",
		Subprotocol: "gnety",
		Codec:       LineCodec{},
	})
	for _, offered := range []string{"", "notgnety", "GNETY"} {
		conn, err := dialWebSocket(context.Background(), &net.Dialer{Timeout: time.Second}, ClientConfig{
			Network: "ws", Addr: srv.Addr(), Path: "/echo", Subprotocol: offered, Timeout: time.Second,
		})
		if err == nil {
			_ = conn.Close()
			t.Fatalf("expected dial failure with subprotocol %q", offered)
		}
	}
}

func TestWebSocketFrameLimit(t *testing.T) {
	srv := echoLineServer(t, Config{
		Network: "ws", Addr: "127.0.0.1:0", Path: "/echo",
		Codec: LineCodec{}, MaxFrameSize: 4,
	})
	conn := dialWebSocketStream(t, "ws", srv.Addr(), "/echo", "", TLSConfig{})
	defer conn.Close()
	if _, err := conn.Write([]byte("12345")); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("expected oversized message to close the connection")
	}
}

func TestWebSocketClientFrameLimit(t *testing.T) {
	srv := echoLineServer(t, Config{
		Network: "ws", Addr: "127.0.0.1:0", Path: "/echo", Codec: LineCodec{},
	})
	cli, err := Dial(context.Background(), ClientConfig{
		Network: "ws", Addr: srv.Addr(), Path: "/echo", Codec: LineCodec{}, MaxFrameSize: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := cli.Send([]byte("12345\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := cli.Receive(ctx); err != ErrFrameTooLarge {
		t.Fatalf("expected ErrFrameTooLarge, got %v", err)
	}
}

func TestWebSocketClientDialContextCancel(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		if conn, acceptErr := ln.Accept(); acceptErr == nil {
			accepted <- conn
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = Dial(ctx, ClientConfig{
		Network: "ws", Addr: ln.Addr().String(), Codec: LineCodec{}, Timeout: 5 * time.Second,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline, got %v", err)
	}
	select {
	case conn := <-accepted:
		_ = conn.Close()
	case <-time.After(time.Second):
		t.Fatal("listener did not accept connection")
	}
}

func TestDTLSEcho(t *testing.T) {
	certFile, keyFile := testCert(t)
	srv, err := New(Config{
		Network: "dtls", Addr: "127.0.0.1:0",
		DatagramCodec: BytesDatagramCodec{},
		TLS:           TLSConfig{CertFile: certFile, KeyFile: keyFile},
		Handler: HandlerFuncs{Message: func(s *Session, msg any) {
			_ = s.Send(msg)
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop(context.Background())

	cli, err := Dial(context.Background(), ClientConfig{
		Network: "dtls", Addr: srv.Addr(), DatagramCodec: BytesDatagramCodec{},
		TLS: TLSConfig{CAFile: certFile},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := cli.Send([]byte("dtls")); err != nil {
		t.Fatal(err)
	}
	msg, err := cli.Receive(ctx)
	if err != nil || string(msg.([]byte)) != "dtls" {
		t.Fatalf("msg=%v err=%v", msg, err)
	}
}
