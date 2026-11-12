// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package exporterhelper // import "go.opentelemetry.io/collector/exporter/exporterhelper"

import (
	"context"

	"go.opentelemetry.io/collector/exporter/exporterhelper/internal"
)

// RecordPartialSuccess records that the destination accepted the request but rejected the given
// number of items, e.g. via an OTLP partial success response. Exporters MUST call this with the
// context passed to their Consume/RequestConsume function for the rejected items to be reported
// in the otelcol_exporter_send_failed_* metrics; the remaining items are reported as sent.
func RecordPartialSuccess(ctx context.Context, rejectedItems int64) {
	internal.RecordPartialSuccess(ctx, rejectedItems)
}
