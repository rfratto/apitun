package httptun

import (
	context "context"
	"errors"

	"google.golang.org/grpc/metadata"
)

type contextKey int

var contextValue contextKey

// Errors
var (
	ErrNoProxyID       = errors.New("no Proxy ID")
	ErrTooManyProxyIDs = errors.New("multiple Proxy IDs present")
)

const ProxyIDHeaderName = "X-Scope-ProxyID"

// ExtractProxyID gets the proxy ID from the context. For a gRPC context, use
// ExtractProxyIDFromGRPC instead.
func ExtractProxyID(ctx context.Context) (string, error) {
	id, ok := ctx.Value(contextValue).(string)
	if !ok {
		return "", ErrNoProxyID
	}
	return id, nil
}

// InjectProxyID injects the proxy ID into the context.
func InjectProxyID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextValue, id)
}

// ExtractProxyIDFromGRPC extracts the proxy ID from a gRPC context. The
// returned context has the proxy ID injected and can be extracted with
// ExtractProxyID.
func ExtractProxyIDFromGRPC(ctx context.Context) (string, context.Context, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", ctx, ErrNoProxyID
	}
	ids := md.Get(ProxyIDHeaderName)
	if len(ids) == 0 {
		return "", ctx, ErrNoProxyID
	} else if len(ids) != 1 {
		return "", ctx, ErrTooManyProxyIDs
	}
	return ids[0], InjectProxyID(ctx, ids[0]), nil
}

// InjectProxyIDIntoGRPC injects a proxy ID into a gRPC context.
func InjectProxyIDIntoGRPC(ctx context.Context, id string) context.Context {
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		md := metadata.New(nil)
		md.Set(ProxyIDHeaderName, id)
		return metadata.NewOutgoingContext(ctx, md)
	}
	// FromOutgoingContext: "Motification should be made to copies of the returned MD"
	md = md.Copy()
	md.Set(ProxyIDHeaderName, id)
	return metadata.NewOutgoingContext(ctx, md)
}
