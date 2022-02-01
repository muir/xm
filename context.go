package xm

import (
	"context"
)

type contextKeyType struct{}

var contextKey = contextKeyType{}

func (log *Log) IntoContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, contextKey, log)
}

func FromContextOrDiscard(ctx context.Context) *Log {
	log, ok := FromContext(ctx)
	if !ok {
		return NewDiscardLogger()
	}
}

func FromContext(ctx context.Context) (*Log, bool) {
	v := ctx.Value(contextKey)
	return v.(*Log), v != nil
}

func MustFromContext(ctx context.Context) *Log {
	log, ok := FromContext(ctx)
	if !ok {
		panic("Could not find logger in context")
	}
	return log
}
