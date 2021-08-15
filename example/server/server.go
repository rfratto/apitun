package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
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
		httpListenAddr string
		grpcListenAddr string
	)

	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	fs.StringVar(&httpListenAddr, "http-listen-addr", "0.0.0.0:8080", "http address to listen on")
	fs.StringVar(&grpcListenAddr, "grpc-listen-addr", "0.0.0.0:9095", "grpc address to listen on")

	if err := fs.Parse(os.Args[1:]); err != nil {
		level.Error(l).Log("msg", "failed to parse args", "err", err)
		os.Exit(1)
	}

	httpLis, err := net.Listen("tcp", httpListenAddr)
	if err != nil {
		level.Error(l).Log("msg", "failed to listen on http", "addr", httpListenAddr, "err", err)
		os.Exit(1)
	}

	grpcLis, err := net.Listen("tcp", grpcListenAddr)
	if err != nil {
		level.Error(l).Log("msg", "failed to listen on grpc", "addr", grpcListenAddr, "err", err)
		os.Exit(1)
	}

	level.Info(l).Log("msg", "http listener started", "addr", httpLis.Addr())
	level.Info(l).Log("msg", "grpc listener started", "addr", grpcLis.Addr())

	proxy := httptun.New(httptun.Options{})

	r := mux.NewRouter()
	r.HandleFunc("/peers", func(rw http.ResponseWriter, r *http.Request) {
		list, err := proxy.ListPeers(r.Context())
		if err != nil {
			level.Error(l).Log("msg", "failed to list peers", "err", err)
			rw.WriteHeader(http.StatusInternalServerError)
			return
		}
		if list == nil {
			list = []string{}
		}
		err = json.NewEncoder(rw).Encode(list)
		if err != nil {
			level.Error(l).Log("msg", "failed to send peers", "err", err)
		}
	})
	r.PathPrefix("/peer/{peer}/").HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		peerID := vars["peer"]

		h, err := proxy.HandlerFor(r.Context(), peerID)
		if err != nil {
			level.Error(l).Log("msg", "failed to get handler", "err", err)
			rw.WriteHeader(http.StatusInternalServerError)
			return
		}

		http.StripPrefix(fmt.Sprintf("/peer/%s", peerID), h).ServeHTTP(rw, r)
	})

	go http.Serve(httpLis, r)

	srv := grpc.NewServer()
	httptun.RegisterProxyServer(srv, proxy)
	srv.Serve(grpcLis)
}
