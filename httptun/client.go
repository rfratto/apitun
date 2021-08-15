package httptun

import (
	"bufio"
	"bytes"
	context "context"
	"net/http"
	"net/http/httptest"
	sync "sync"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
)

// Forward will connect to the ProxyClient c with the provided context and
// proxy ID. The handler h will be invoked for every proxied request received.
//
// To stop forwarding, cancel the provided context.
func Forward(l log.Logger, ctx context.Context, c ProxyClient, proxyID string, h http.Handler) error {
	ctx = InjectProxyIDIntoGRPC(ctx, proxyID)
	stream, err := c.Register(ctx)
	if err != nil {
		return err
	}

	// We need a dedicated goroutine for handling responses because Send isn't
	// concurrency safe, and all requests are handled in a unique goroutine.
	var (
		respMut sync.Mutex
		respCh  = make(chan *Response)
	)
	go func() {
		for r := range respCh {
			if err := stream.Send(r); err != nil {
				level.Error(l).Log("msg", "failed to send response", "err", err)
			}
		}
	}()
	defer func() {
		respMut.Lock()
		defer respMut.Unlock()
		close(respCh)
		respCh = nil
	}()

	for {
		rawReq, err := stream.Recv()
		if ctx.Err() != nil {
			return nil
		} else if err != nil {
			return err
		}

		go func() {
			req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(rawReq.Request)))
			if err != nil {
				level.Error(l).Log("msg", "failed to read request", "err", err)
				return
			}

			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)

			var rawResp bytes.Buffer
			if err := rr.Result().Write(&rawResp); err != nil {
				level.Error(l).Log("msg", "failed to marshal response", "err", err)
				return
			}

			respMut.Lock()
			defer respMut.Unlock()
			if respCh == nil {
				level.Warn(l).Log("msg", "not sending response because forwarder is shutting down", "id", rawReq.Id)
				return
			}
			respCh <- &Response{
				Id:       rawReq.Id,
				Response: rawResp.Bytes(),
			}
		}()
	}
}
