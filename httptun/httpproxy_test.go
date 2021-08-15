package httptun_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/rfratto/apitun/httptun"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestProxy_HandlerFor(t *testing.T) {
	logger := log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	p := httptun.New(httptun.Options{})

	srv := grpc.NewServer()
	httptun.RegisterProxyServer(srv, p)

	go func() {
		_ = srv.Serve(l)
	}()

	cli, err := grpc.Dial(l.Addr().String(), grpc.WithInsecure())
	require.NoError(t, err)

	c := httptun.NewProxyClient(cli)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		err := httptun.Forward(logger, ctx, c, "test-id", http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(rw, "hello, world")
		}))
		assert.NoError(t, err)
	}()

	time.Sleep(time.Millisecond * 500)

	proxyHandler, err := p.HandlerFor(context.Background(), "test-id")
	require.NoError(t, err)

	rr := httptest.NewRecorder()

	req, err := http.NewRequest("GET", fmt.Sprintf("http://%s/", l.Addr().String()), nil)
	require.NoError(t, err)
	proxyHandler.ServeHTTP(rr, req)
	require.Equal(t, "hello, world", string(rr.Body.Bytes()))
}

func TestProxy_ListPeers(t *testing.T) {
	logger := log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	p := httptun.New(httptun.Options{})

	srv := grpc.NewServer()
	httptun.RegisterProxyServer(srv, p)

	go func() {
		_ = srv.Serve(l)
	}()

	cli, err := grpc.Dial(l.Addr().String(), grpc.WithInsecure())
	require.NoError(t, err)

	c := httptun.NewProxyClient(cli)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		err := httptun.Forward(logger, ctx, c, "test-id", http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(rw, "hello, world")
		}))
		assert.NoError(t, err)
	}()

	time.Sleep(time.Millisecond * 500)

	peers, err := p.ListPeers(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"test-id"}, peers)
}
