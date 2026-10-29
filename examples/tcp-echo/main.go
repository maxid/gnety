package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/maxid/gnety"
)

func main() {
	srv, err := gnety.New(gnety.Config{
		Network: "tcp",
		Addr:    ":9000",
		Codec:   gnety.LengthFieldCodec{},
		Handler: gnety.HandlerFuncs{Message: func(s *gnety.Session, msg any) {
			_ = s.Send(msg)
		}},
	})
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := srv.Start(ctx); err != nil {
		log.Fatal(err)
	}
	log.Printf("tcp echo server listening on %s", srv.Addr())
	<-ctx.Done()
	_ = srv.Stop(context.Background())
}
