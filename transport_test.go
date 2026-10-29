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
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"
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

func TestWebSocketListener(t *testing.T) {
	srv := echoLineServer(t, Config{
		Network:     "ws",
		Addr:        "127.0.0.1:0",
		Path:        "/echo",
		Subprotocol: "gnety",
		Codec:       LineCodec{},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws://"+srv.Addr()+"/echo", &websocket.DialOptions{Subprotocols: []string{"gnety"}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")
	conn := websocket.NetConn(context.Background(), c, websocket.MessageBinary)
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
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	c, _, err := websocket.Dial(ctx, "wss://"+srv.Addr()+"/echo", &websocket.DialOptions{HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")
	conn := websocket.NetConn(context.Background(), c, websocket.MessageBinary)
	if _, err := conn.Write([]byte("wss\n")); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || line != "wss\n" {
		t.Fatalf("line=%q err=%v", line, err)
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, _, err := websocket.Dial(ctx, "ws://"+srv.Addr()+"/echo", nil); err == nil {
		t.Fatal("expected dial failure without subprotocol")
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
