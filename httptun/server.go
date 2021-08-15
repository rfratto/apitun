package httptun

import (
	context "context"
	"fmt"
	"net/http"
	"sort"
	"sync"

	"github.com/weaveworks/common/user"
	"go.uber.org/atomic"
)

var DefaultOrgID string = "none"

type Proxy interface {
	ProxyServer

	// ListPeers lists the ID for all proxy peers.
	ListPeers(ctx context.Context) ([]string, error)

	// HandlerFor returns an http.Handler for a given proxyID. Requests
	// will be proxied to the connected peer with that proxyID.
	HandlerFor(ctx context.Context, proxyID string) (http.Handler, error)
}

// Options controls the proxy.
type Options struct {
	// Multitenant enables multi-tenancy. When enabled, an X-Scope-OrgID header
	// must be present for all requests to indicate the tenant.
	Multitenant bool
}

// New creates a new Proxy.
func New(opts Options) Proxy {
	return &proxyServer{
		opts:  opts,
		peers: make(map[string]proxyPeers),
	}
}

type proxyServer struct {
	UnimplementedProxyServer

	opts Options

	// TODO(rfratto): a massive lock over all of the tenants is latency-prone.
	mut   sync.RWMutex
	peers map[string]proxyPeers // map of OrgID -> (ProxyID -> proxier)
}

type proxyPeers map[string]*proxier

// Register registers a client to start receiving proxied requests.
func (p *proxyServer) Register(srv Proxy_RegisterServer) error {
	orgID := DefaultOrgID
	if p.opts.Multitenant {
		ctx := srv.Context()
		var err error
		orgID, ctx, err = user.ExtractFromGRPCRequest(ctx)
		if err != nil {
			return err
		}
		srv = setContext(srv, ctx)
	}

	return p.registerForOrg(orgID, srv)
}

func (p *proxyServer) registerForOrg(orgID string, srv Proxy_RegisterServer) error {
	var (
		ctx = srv.Context()

		proxyID string
		err     error
	)
	proxyID, ctx, err = ExtractProxyIDFromGRPC(ctx)
	if err != nil {
		return err
	} else if proxyID == "" {
		return ErrNoProxyID
	}
	srv = setContext(srv, ctx)

	proxy := &proxier{
		id: atomic.NewInt64(0),
		ch: make(chan *Request),
	}
	defer proxy.Close()

	p.mut.Lock()
	if p.peers[orgID] == nil {
		p.peers[orgID] = make(proxyPeers)
	}
	p.peers[orgID][proxyID] = proxy
	p.mut.Unlock()

	defer func() {
		p.mut.Lock()
		delete(p.peers[orgID], proxyID)
		if len(p.peers[orgID]) == 0 {
			delete(p.peers, orgID)
		}
		p.mut.Unlock()
	}()

	go func() {
		for r := range proxy.ch {
			if err := srv.Send(r); err != nil {
				break
			}
		}
	}()

	for {
		resp, err := srv.Recv()
		if err != nil {
			break
		}
		proxy.forwardResponse(resp)
	}

	return nil
}

func (p *proxyServer) ListPeers(ctx context.Context) ([]string, error) {
	orgID := DefaultOrgID
	if p.opts.Multitenant {
		var err error
		orgID, err = user.ExtractOrgID(ctx)
		if err != nil {
			return nil, err
		}
	}

	p.mut.RLock()
	defer p.mut.RUnlock()

	if p.peers[orgID] == nil {
		return nil, nil
	}

	list := make([]string, 0, len(p.peers[orgID]))
	for k := range p.peers[orgID] {
		list = append(list, k)
	}
	sort.Strings(list)
	return list, nil
}

func (p *proxyServer) HandlerFor(ctx context.Context, proxyID string) (http.Handler, error) {
	orgID := DefaultOrgID
	if p.opts.Multitenant {
		var err error
		orgID, err = user.ExtractOrgID(ctx)
		if err != nil {
			return nil, err
		}
	}

	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		p.mut.RLock()
		defer p.mut.RUnlock()

		if p.peers[orgID] == nil {
			rw.WriteHeader(http.StatusBadGateway)
			fmt.Fprintf(rw, "no such peer: %s", proxyID)
			return
		}

		p, ok := p.peers[orgID][proxyID]
		if !ok {
			rw.WriteHeader(http.StatusBadGateway)
			fmt.Fprintf(rw, "no such peer: %s", proxyID)
			return
		}

		p.ServeHTTP(rw, r)
	}), nil
}

type wrappedServer struct {
	Proxy_RegisterServer
	ctx context.Context
}

func (w *wrappedServer) Context() context.Context {
	return w.ctx
}

func setContext(srv Proxy_RegisterServer, ctx context.Context) Proxy_RegisterServer {
	if w, ok := srv.(*wrappedServer); ok {
		w.ctx = ctx
		return w
	}
	return &wrappedServer{Proxy_RegisterServer: srv, ctx: ctx}
}
