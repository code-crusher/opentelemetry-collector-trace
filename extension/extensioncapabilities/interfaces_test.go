// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package extensioncapabilities

import (
	"os"
	"reflect"
	"syscall"
	"testing"

	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/extension"
)

type signalWatcherExt struct {
	extension.Extension
}

func (signalWatcherExt) Signals() []os.Signal { return nil }

func (signalWatcherExt) Signal(os.Signal) error { return nil }

func TestSignalWatcher(t *testing.T) {
	// Compile-time check that the interface can be implemented by extensions.
	var _ interface {
		extension.Extension
		SignalWatcher
	} = signalWatcherExt{}

	var watcher SignalWatcher = signalWatcherExt{}
	// An empty set of signals is a valid declaration of interest.
	if len(watcher.Signals()) != 0 {
		t.Fatal("Signals() returned a non-empty slice")
	}
	if err := watcher.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("Signal() returned an error: %v", err)
	}
}

func TestConfigSnapshot(t *testing.T) {
	effective := confmap.NewFromStringMap(map[string]any{
		"receivers": map[string]any{
			"nop": map[string]any{"endpoint": "localhost:4317"},
		},
	})
	unexpanded := confmap.NewFromStringMap(map[string]any{
		"receivers": map[string]any{
			"nop": map[string]any{"endpoint": "${env:ENDPOINT}"},
		},
	})

	snapshot := NewConfigSnapshot(effective, unexpanded)

	gotEffective := snapshot.Effective()
	if gotEffective == nil {
		t.Fatal("Effective() returned nil")
	}
	if !reflect.DeepEqual(effective.ToStringMap(), gotEffective.ToStringMap()) {
		t.Fatalf("Effective() = %v, want %v", gotEffective.ToStringMap(), effective.ToStringMap())
	}

	gotUnexpanded := snapshot.Unexpanded()
	if gotUnexpanded == nil {
		t.Fatal("Unexpanded() returned nil")
	}
	if !reflect.DeepEqual(unexpanded.ToStringMap(), gotUnexpanded.ToStringMap()) {
		t.Fatalf("Unexpanded() = %v, want %v", gotUnexpanded.ToStringMap(), unexpanded.ToStringMap())
	}
}

func TestConfigSnapshotReturnsIndependentConfs(t *testing.T) {
	snapshot := NewConfigSnapshot(confmap.NewFromStringMap(map[string]any{"a": "b"}), nil)

	got := snapshot.Effective()
	if got == nil {
		t.Fatal("Effective() returned nil")
	}
	if !got.Delete("a") {
		t.Fatal("Delete() returned false")
	}

	gotAgain := snapshot.Effective()
	if gotAgain == nil {
		t.Fatal("Effective() returned nil")
	}
	if !reflect.DeepEqual(map[string]any{"a": "b"}, gotAgain.ToStringMap()) {
		t.Fatalf("Effective() after mutation = %v, want %v", gotAgain.ToStringMap(), map[string]any{"a": "b"})
	}
}

func TestConfigSnapshotNilRepresentations(t *testing.T) {
	snapshot := NewConfigSnapshot(nil, nil)

	if snapshot.Effective() != nil {
		t.Fatal("Effective() returned non-nil config")
	}
	if snapshot.Unexpanded() != nil {
		t.Fatal("Unexpanded() returned non-nil config")
	}
}
