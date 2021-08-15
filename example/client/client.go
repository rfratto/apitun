package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/gorilla/mux"
	"github.com/rfratto/apitun/httptun"
	"google.golang.org/grpc"
)

func main() {
	l := log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))

	var (
		serverAddr string
		peerID     string
	)

	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	fs.StringVar(&serverAddr, "server-addr", "127.0.0.1:9095", "grpc server to connect to")
	fs.StringVar(&peerID, "peer-id", "", "peer id to use")

	if err := fs.Parse(os.Args[1:]); err != nil {
		level.Error(l).Log("msg", "failed to parse args", "err", err)
		os.Exit(1)
	}

	if peerID == "" {
		level.Error(l).Log("msg", "-peer-id is required")
		os.Exit(1)
	}

	r := mux.NewRouter()
	r.HandleFunc("/hello", func(rw http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(rw, "hello from peer %s!\n", peerID)
	})

	cli, err := grpc.Dial(serverAddr, grpc.WithInsecure())
	if err != nil {
		level.Error(l).Log("msg", "failed to connect to server", "err", err)
		os.Exit(1)
	}
	pc := httptun.NewProxyClient(cli)

	if err := httptun.Forward(l, context.Background(), pc, peerID, r); err != nil {
		level.Error(l).Log("msg", "forwarding failed", "err", err)
	}
}
