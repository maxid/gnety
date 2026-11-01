package gnety

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/pion/dtls/v2"
)

// testKeyPair builds a self-signed server certificate and the pool that trusts
// it, entirely in memory.
func testKeyPair(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "gnety-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pool
}

func TestInMemoryTLSConfig(t *testing.T) {
	cert, pool := testKeyPair(t)
	srv, err := New(Config{
		Network:   "tcp-tls",
		Addr:      "127.0.0.1:0",
		Codec:     LineCodec{},
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}},
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
	defer func() { _ = srv.Stop(context.Background()) }()

	cli, err := Dial(context.Background(), ClientConfig{
		Network:   "tcp-tls",
		Addr:      srv.Addr(),
		Codec:     LineCodec{},
		TLSConfig: &tls.Config{RootCAs: pool, ServerName: "127.0.0.1"},
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer cli.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := cli.Send([]byte("hello\n")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	msg, err := cli.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if got, ok := msg.([]byte); !ok || string(got) != "hello" {
		t.Fatalf("msg = %#v", msg)
	}
}

func TestInMemoryDTLSConfig(t *testing.T) {
	cert, pool := testKeyPair(t)
	srv, err := New(Config{
		Network:       "dtls",
		Addr:          "127.0.0.1:0",
		DatagramCodec: BytesDatagramCodec{},
		DTLSConfig:    &dtls.Config{Certificates: []tls.Certificate{cert}},
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
	defer func() { _ = srv.Stop(context.Background()) }()

	cli, err := Dial(context.Background(), ClientConfig{
		Network:       "dtls",
		Addr:          srv.Addr(),
		DatagramCodec: BytesDatagramCodec{},
		DTLSConfig:    &dtls.Config{RootCAs: pool, ServerName: "127.0.0.1"},
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer cli.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := cli.Send([]byte("dtls")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	msg, err := cli.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if got, ok := msg.([]byte); !ok || string(got) != "dtls" {
		t.Fatalf("msg = %#v", msg)
	}
}

func TestClientSetDeadline(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	// Accept and never answer.
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		<-time.After(2 * time.Second)
	}()

	cli, err := Dial(context.Background(), ClientConfig{
		Network: "tcp", Addr: ln.Addr().String(), Codec: LineCodec{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := cli.SetDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatalf("SetDeadline: %v", err)
	}
	start := time.Now()
	_, err = cli.Receive(context.Background())
	if err == nil {
		t.Fatal("Receive returned nil")
	}
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Fatalf("Receive err = %v, want a timeout", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("deadline took %v", elapsed)
	}
	_ = cli.Close()
	if err := cli.SetDeadline(time.Now()); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("SetDeadline after Close = %v, want ErrSessionClosed", err)
	}
}
