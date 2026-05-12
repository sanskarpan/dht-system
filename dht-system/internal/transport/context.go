package transport

import "context"

type senderContextKey struct{}

// WithSender annotates a context with the logical sender of an RPC.
// In-process transports use this to enforce partition and per-link latency rules
// even for RPC methods whose signature does not include an explicit sender.
func WithSender(ctx context.Context, sender NodeRef) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, senderContextKey{}, sender)
}

func senderFromContext(ctx context.Context) (NodeRef, bool) {
	if ctx == nil {
		return NodeRef{}, false
	}
	sender, ok := ctx.Value(senderContextKey{}).(NodeRef)
	if !ok || sender.Addr == "" {
		return NodeRef{}, false
	}
	return sender, true
}
