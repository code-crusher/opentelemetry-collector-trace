// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/exporter/exporterhelper/internal"

import (
	"context"
	"sync/atomic"
)

type partialSuccessKeyType struct{}

// newPartialSuccessContext returns a context that allows an exporter to record the number of
// items rejected by the destination as part of a partial success response, e.g. an OTLP
// partial success. The recording is read by the observability reporter once sending completes.
func newPartialSuccessContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, partialSuccessKeyType{}, new(atomic.Int64))
}

// RecordPartialSuccess records the given number of rejected items in the context. It is a no-op
// if the context was not prepared with newPartialSuccessContext, e.g. when the request is sent
// outside of exporterhelper.
func RecordPartialSuccess(ctx context.Context, rejectedItems int64) {
	if rejectedItems <= 0 {
		return
	}
	if rejected, ok := ctx.Value(partialSuccessKeyType{}).(*atomic.Int64); ok {
		rejected.Add(rejectedItems)
	}
}

// partialSuccessRejectedItems returns the number of items recorded as rejected in the context.
func partialSuccessRejectedItems(ctx context.Context) int64 {
	if rejected, ok := ctx.Value(partialSuccessKeyType{}).(*atomic.Int64); ok {
		return rejected.Load()
	}
	return 0
}
