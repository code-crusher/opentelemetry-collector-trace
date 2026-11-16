// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package client contains generic representations of clients connecting to
// different receivers
package client

import (
	"context"
	"net"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewContext(t *testing.T) {
	testCases := []struct {
		name string
		cl   Info
	}{
		{
			name: "valid client",
			cl: Info{
				Addr: &net.IPAddr{
					IP: net.IPv4(1, 2, 3, 4),
				},
			},
		},
		{
			name: "nil client",
			cl:   Info{},
		},
	}
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			ctx := NewContext(context.Background(), tt.cl)
			assert.Equal(t, ctx.Value(ctxKey{}), tt.cl)
		})
	}
}

func TestFromContext(t *testing.T) {
	testCases := []struct {
		name     string
		input    context.Context
		expected Info
	}{
		{
			name: "context with client",
			input: context.WithValue(context.Background(), ctxKey{}, Info{
				Addr: &net.IPAddr{
					IP: net.IPv4(1, 2, 3, 4),
				},
			}),
			expected: Info{
				Addr: &net.IPAddr{
					IP: net.IPv4(1, 2, 3, 4),
				},
			},
		},
		{
			name:     "context without client",
			input:    context.Background(),
			expected: Info{},
		},
		{
			name:     "context with something else in the key",
			input:    context.WithValue(context.Background(), ctxKey{}, "unexpected!"),
			expected: Info{},
		},
	}
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, FromContext(tt.input))
		})
	}
}

func TestMetadata(t *testing.T) {
	source := map[string][]string{"test-key": {"test-val"}, "TEST-KEY-2": {"test-val"}}
	md := NewMetadata(source)
	assert.Equal(t, []string{"test-key", "test-key-2"}, slices.Sorted(md.Keys()))
	assert.Equal(t, []string{"test-val"}, md.Get("test-key"))
	assert.Equal(t, []string{"test-val"}, md.Get("test-KEY"))   // case insensitive lookup
	assert.Equal(t, []string{"test-val"}, md.Get("test-key-2")) // case insensitive lookup

	// test if copy. In regular use, source cannot change
	val := md.Get("test-key")
	source["test-key"][0] = "abc"
	assert.Equal(t, []string{"test-val"}, val)

	assert.Empty(t, md.Get("non-existent-key"))
}

func TestUninstantiatedMetadata(t *testing.T) {
	i := Info{}
	assert.Empty(t, slices.Collect(i.Metadata.Keys()))
	assert.Empty(t, i.Metadata.Get("test"))
}

func TestMetadataBuilder(t *testing.T) {
	base := NewMetadata(map[string][]string{"test-key": {"test-val"}, "TEST-KEY-2": {"test-val"}})

	md := NewMetadataBuilder(base).
		Set("new-key", "new-val").
		Set("test-key", "replaced").
		Set("case-key", "first", "second").
		Build()

	assert.Equal(t, []string{"case-key", "new-key", "test-key", "test-key-2"}, slices.Sorted(md.Keys()))
	assert.Equal(t, []string{"replaced"}, md.Get("TEST-KEY")) // case insensitive, overridden
	assert.Equal(t, []string{"test-val"}, md.Get("test-key-2"))
	assert.Equal(t, []string{"first", "second"}, md.Get("case-key"))
	assert.Equal(t, []string{"new-val"}, md.Get("new-key"))

	// The base Metadata is immutable and not affected by the builder.
	assert.Equal(t, []string{"test-key", "test-key-2"}, slices.Sorted(base.Keys()))
	assert.Equal(t, []string{"test-val"}, base.Get("test-key"))
	assert.Empty(t, base.Get("new-key"))
}

func TestMetadataBuilderEmpty(t *testing.T) {
	// A builder without a base and a builder with an empty base are both usable.
	assert.Empty(t, slices.Collect(NewMetadataBuilder(Metadata{}).Build().Keys()))
	assert.Empty(t, slices.Collect(NewMetadataBuilder(NewMetadata(nil)).Build().Keys()))
	assert.Empty(t, slices.Collect((&MetadataBuilder{}).Build().Keys()))

	md := NewMetadataBuilder(Metadata{}).Set("key", "val").Build()
	assert.Equal(t, []string{"val"}, md.Get("key"))
}

func TestMetadataBuilderImmutability(t *testing.T) {
	source := map[string][]string{"base-key": {"base-val"}}
	base := NewMetadata(source)

	// Derive a second generation, then mutate everything the caller owns.
	values := []string{"derived-val"}
	derived := NewMetadataBuilder(base).Set("derived-key", values...).Build()

	values[0] = "mutated"
	source["base-key"] = []string{"mutated"}
	source["source-key"] = []string{"source-val"}

	assert.Equal(t, []string{"derived-val"}, derived.Get("derived-key"))
	assert.Equal(t, []string{"base-val"}, derived.Get("base-key"))
	assert.Empty(t, derived.Get("source-key"))
	assert.Equal(t, []string{"base-val"}, base.Get("base-key"))
	assert.Empty(t, base.Get("source-key"))

	// A third generation derived from the second one is independent from it.
	third := NewMetadataBuilder(derived).Set("third-key", "third-val").Build()
	assert.Equal(t, []string{"third-val"}, third.Get("third-key"))
	assert.Equal(t, []string{"derived-val"}, third.Get("derived-key"))
	assert.Empty(t, derived.Get("third-key"))
	assert.Empty(t, base.Get("derived-key"))
}

func TestMetadataBuilderBuildResets(t *testing.T) {
	builder := NewMetadataBuilder(NewMetadata(map[string][]string{"base-key": {"base-val"}}))
	first := builder.Set("first-key", "first-val").Build()
	second := builder.Set("second-key", "second-val").Build()

	assert.Equal(t, []string{"first-val"}, first.Get("first-key"))
	assert.Empty(t, first.Get("second-key"))

	assert.Equal(t, []string{"second-val"}, second.Get("second-key"))
	assert.Empty(t, second.Get("base-key")) // the base was consumed by the first Build
}

func BenchmarkDeriveMetadata(b *testing.B) {
	base := NewMetadata(map[string][]string{
		"key-1": {"val-1"}, "key-2": {"val-2"}, "key-3": {"val-3"}, "key-4": {"val-4"},
	})

	b.Run("NewMetadata", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			md := make(map[string][]string, 5)
			for k := range base.Keys() {
				md[k] = base.Get(k)
			}
			md["new-key"] = []string{"new-val"}
			_ = NewMetadata(md)
		}
	})

	b.Run("MetadataBuilder", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = NewMetadataBuilder(base).Set("new-key", "new-val").Build()
		}
	})
}
