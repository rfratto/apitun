package httptun

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"sync"

	"go.uber.org/atomic"
)

// The proxier handles the individual proxying of requests.
type proxier struct {
	// id is the next ID to assign to a given request.
	id *atomic.Int64

	// mut protects the channel, which is closed and set to nil when the proxier
	// is closed.
	mut sync.RWMutex
	ch  chan *Request

	listeners sync.Map
}

// ServeHTTP queues a request to be sent and writes the response to rw
// once the response is received.
func (p *proxier) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	p.mut.RLock()
	defer p.mut.RUnlock()

	if p.ch == nil {
		rw.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(rw, "gateway closed")
		return
	}

	var buf bytes.Buffer
	if err := r.WriteProxy(&buf); err != nil {
		rw.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(rw, "error writing request: %s", err)
		return
	}

	var (
		requestID = p.id.Inc()
		respCh    = make(chan *Response, 1)
	)

	// Create a listener to handler the request. This *must* be done before
	// enqueueing to guarantee the channel exists when the response is received.
	p.listeners.Store(requestID, respCh)
	defer p.listeners.Delete(requestID)

	// Enqueue the request.
	p.ch <- &Request{
		Id:      requestID,
		Request: buf.Bytes(),
	}

	select {
	case <-r.Context().Done():
		rw.WriteHeader(http.StatusGatewayTimeout)
		fmt.Fprintf(rw, "timed out waiting for peer")
	case rawResp := <-respCh:
		resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(rawResp.Response)), nil)
		if err != nil {
			rw.WriteHeader(http.StatusBadGateway)
			fmt.Fprintf(rw, "error reading response: %s", err)
			return
		}

		// Copy all of the headers from the response.
		for k, vv := range resp.Header {
			rw.Header().Del(k)
			for _, v := range vv {
				rw.Header().Add(k, v)
			}
		}

		// Then write our status code and the response body.
		rw.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(rw, resp.Body)
		defer resp.Body.Close()
	}
}

// Close closes the proxier, preventing more requests from coming through.
func (p *proxier) Close() {
	p.mut.Lock()
	defer p.mut.Unlock()

	close(p.ch)
	p.ch = nil
}

// forwardResponse will forward the response to the ServeHTTP caller for the
// matching ID.
func (p *proxier) forwardResponse(r *Response) {
	listener, ok := p.listeners.Load(r.Id)
	if !ok {
		// It's possible the listener timed out. Ignore the response and move on.
		return
	}
	listener.(chan *Response) <- r
}
