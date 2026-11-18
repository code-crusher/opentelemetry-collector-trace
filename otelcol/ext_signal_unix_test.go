// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package otelcol

import "syscall"

// extOnlySignal is an OS signal the collector itself does not react to, used
// to exercise dispatching of extension-only signals in tests.
const extOnlySignal = syscall.SIGUSR1
