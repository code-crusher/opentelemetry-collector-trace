// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !unix

package otelcol

import "os"

// extOnlySignal is an OS signal the collector itself does not react to, used
// to exercise dispatching of extension-only signals in tests. On platforms
// without a suitable signal (e.g. js/wasm, Windows) a synthetic value is used;
// tests only ever inject it into the collector's signal channel.
var extOnlySignal os.Signal = stubSignal{}

type stubSignal struct{}

func (stubSignal) String() string { return "stub" }

func (stubSignal) Signal() {}
