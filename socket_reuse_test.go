//go:build linux || darwin || dragonfly || freebsd || netbsd || openbsd

package gnety

import (
	"context"
	"errors"
	"testing"
)

func TestReusePortAllowsTwoListeners(t *testing.T) {
	cfg := Config{
		Network: "tcp",
		Addr:    "127.0.0.1:0",
		Codec:   LengthFieldCodec{},
		Socket:  SocketOptions{ReusePort: true},
	}
	first, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Start(context.Background()); err != nil {
		if errors.Is(err, ErrSocketOptionsUnsupported) {
			t.Skip("SO_REUSEPORT is not available")
		}
		t.Fatal(err)
	}
	defer func() { _ = first.Stop(context.Background()) }()

	cfg.Addr = first.Addr()
	second, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Stop(context.Background()) }()

	if second.Addr() != first.Addr() {
		t.Fatalf("addresses = %q / %q", first.Addr(), second.Addr())
	}
}
