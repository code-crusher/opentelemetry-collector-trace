// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package importpolicy

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchesPrefix(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		pkg    string
		want   bool
	}{
		{name: "empty prefix matches anything", prefix: "", pkg: "golang.org/x/crypto/sha3", want: true},
		{name: "exact match", prefix: "golang.org/x/crypto", pkg: "golang.org/x/crypto", want: true},
		{name: "subpackage", prefix: "golang.org/x/crypto", pkg: "golang.org/x/crypto/sha3", want: true},
		{name: "trailing slash in prefix", prefix: "golang.org/x/crypto/", pkg: "golang.org/x/crypto/sha3", want: true},
		{name: "sibling with a shared name prefix", prefix: "golang.org/x/crypto", pkg: "golang.org/x/cryptobyte", want: false},
		{name: "unrelated package", prefix: "golang.org/x/crypto", pkg: "crypto/sha256", want: false},
		{name: "prefix longer than package", prefix: "golang.org/x/crypto/sha3", pkg: "golang.org/x/crypto", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, matchesPrefix(tt.prefix, tt.pkg))
		})
	}
}

func TestIsForbidden(t *testing.T) {
	forbidden := []string{"golang.org/x/crypto", "filippo.io/"}
	assert.True(t, isForbidden("golang.org/x/crypto/chacha20poly1305", forbidden))
	assert.True(t, isForbidden("filippo.io/edwards25519", forbidden))
	assert.False(t, isForbidden("crypto/sha256", forbidden))
	assert.False(t, isForbidden("golang.org/x/net/http2", forbidden))
	assert.False(t, isForbidden("golang.org/x/crypto", nil))
}

func TestFindViolations(t *testing.T) {
	graph := map[string][]string{
		"example.com/app": {
			"crypto/tls",
			"example.com/app/internal",
		},
		"example.com/app/internal": {
			"golang.org/x/crypto/cryptobyte",
		},
		"golang.org/x/crypto/cryptobyte": {
			"golang.org/x/crypto/cryptobyte/asn1",
		},
		"crypto/tls": {},
	}

	violations := findViolations(graph, []string{"golang.org/x/crypto"})
	// The forbidden module importing its own subpackages is not reported: only
	// the packages that pull the module into the graph are.
	require.Equal(t, []Violation{
		{Importer: "example.com/app/internal", Imported: "golang.org/x/crypto/cryptobyte"},
	}, violations)
}

func TestFindViolationsSorted(t *testing.T) {
	graph := map[string][]string{
		"example.com/b": {"golang.org/x/crypto/sha3"},
		"example.com/a": {"golang.org/x/crypto/sha3", "golang.org/x/crypto/hkdf"},
	}

	assert.Equal(t, []Violation{
		{Importer: "example.com/a", Imported: "golang.org/x/crypto/hkdf"},
		{Importer: "example.com/a", Imported: "golang.org/x/crypto/sha3"},
		{Importer: "example.com/b", Imported: "golang.org/x/crypto/sha3"},
	}, findViolations(graph, []string{"golang.org/x/crypto"}))
}

func TestFindViolationsNoForbiddenImports(t *testing.T) {
	graph := map[string][]string{
		"example.com/app": {"crypto/tls", "crypto/sha256"},
	}
	assert.Empty(t, findViolations(graph, []string{"golang.org/x/crypto"}))
}

func TestKnownViolationMatches(t *testing.T) {
	v := Violation{Importer: "github.com/foxboron/go-tpm-keyfiles", Imported: "golang.org/x/crypto/cryptobyte"}

	tests := []struct {
		name  string
		known KnownViolation
		want  bool
	}{
		{
			name:  "exact match",
			known: KnownViolation{Importer: "github.com/foxboron/go-tpm-keyfiles", Imported: "golang.org/x/crypto/cryptobyte"},
			want:  true,
		},
		{
			name:  "module root prefixes",
			known: KnownViolation{Importer: "github.com/foxboron/go-tpm-keyfiles", Imported: "golang.org/x/crypto"},
			want:  true,
		},
		{
			name:  "wildcard importer and imported",
			known: KnownViolation{},
			want:  true,
		},
		{
			name:  "wildcard imported",
			known: KnownViolation{Importer: "github.com/foxboron/go-tpm-keyfiles"},
			want:  true,
		},
		{
			name:  "different importer",
			known: KnownViolation{Importer: "example.com/other", Imported: "golang.org/x/crypto"},
			want:  false,
		},
		{
			name:  "parent path is a prefix",
			known: KnownViolation{Importer: "github.com/foxboron", Imported: "golang.org/x/crypto"},
			want:  true,
		},
		{
			name:  "importer is not a prefix",
			known: KnownViolation{Importer: "github.com/foxboron/go-tpm", Imported: "golang.org/x/crypto"},
			want:  false,
		},
		{
			name:  "different imported",
			known: KnownViolation{Importer: "github.com/foxboron/go-tpm-keyfiles", Imported: "filippo.io/edwards25519"},
			want:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.known.matches(v))
		})
	}
}

func TestCompare(t *testing.T) {
	known := []KnownViolation{
		{Importer: "example.com/app", Imported: "golang.org/x/crypto/sha3", Reason: "documented"},
		{Importer: "example.com/app", Imported: "golang.org/x/crypto/hkdf", Reason: "no longer imported"},
	}
	violations := []Violation{
		{Importer: "example.com/app", Imported: "golang.org/x/crypto/sha3"},
		{Importer: "example.com/app", Imported: "golang.org/x/crypto/chacha20poly1305"},
	}

	matches, unmatched, stale := compare(violations, known)

	assert.Equal(t, []match{{
		violation: Violation{Importer: "example.com/app", Imported: "golang.org/x/crypto/sha3"},
		known:     known[0],
	}}, matches)
	assert.Equal(t, []Violation{
		{Importer: "example.com/app", Imported: "golang.org/x/crypto/chacha20poly1305"},
	}, unmatched)
	assert.Equal(t, []KnownViolation{known[1]}, stale)
}

func TestCompareOneViolationAcceptedByMultipleEntries(t *testing.T) {
	v := Violation{Importer: "example.com/app/internal", Imported: "golang.org/x/crypto/sha3"}
	known := []KnownViolation{
		{Importer: "example.com/app", Imported: "golang.org/x/crypto"},
		{Importer: "", Imported: "golang.org/x/crypto/sha3"},
	}

	matches, unmatched, stale := compare([]Violation{v}, known)

	assert.Len(t, matches, 2)
	assert.Empty(t, unmatched)
	assert.Empty(t, stale)
}

func TestCompareNoViolations(t *testing.T) {
	matches, unmatched, stale := compare(nil, nil)
	assert.Empty(t, matches)
	assert.Empty(t, unmatched)
	assert.Empty(t, stale)
}

func TestCheck(t *testing.T) {
	tests := []struct {
		name       string
		violations []Violation
		known      []KnownViolation
		wantLogs   []string
		wantErrors []string
	}{
		{
			name: "accepted violation is logged",
			violations: []Violation{
				{Importer: "example.com/app", Imported: "golang.org/x/crypto/sha3"},
			},
			known: []KnownViolation{
				{Importer: "example.com/app", Imported: "golang.org/x/crypto/sha3", Reason: "reviewed"},
			},
			wantLogs: []string{"known violation: example.com/app -> golang.org/x/crypto/sha3: reviewed"},
		},
		{
			name: "new violation fails the test",
			violations: []Violation{
				{Importer: "example.com/app", Imported: "golang.org/x/crypto/sha3"},
			},
			wantErrors: []string{
				"forbidden import is not covered by a known violation: example.com/app -> golang.org/x/crypto/sha3",
			},
		},
		{
			name: "stale entry fails the test",
			known: []KnownViolation{
				{Importer: "example.com/app", Imported: "golang.org/x/crypto/sha3"},
			},
			wantErrors: []string{
				"known violation no longer applies: example.com/app -> golang.org/x/crypto/sha3",
			},
		},
		{
			name: "clean graph passes",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &fakeReporter{}
			check(r, tt.violations, tt.known)
			assert.Equal(t, tt.wantLogs, r.logs)
			require.Len(t, r.errors, len(tt.wantErrors))
			for i, want := range tt.wantErrors {
				assert.Contains(t, r.errors[i], want)
			}
		})
	}
}

// fakeReporter records the output of check so that it can be asserted on.
type fakeReporter struct {
	logs   []string
	errors []string
}

func (r *fakeReporter) Helper() {}

func (r *fakeReporter) Logf(format string, args ...any) {
	r.logs = append(r.logs, fmt.Sprintf(format, args...))
}

func (r *fakeReporter) Errorf(format string, args ...any) {
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}

func TestLoadImportGraph(t *testing.T) {
	graph, err := loadImportGraph(t.Context(), []string{"./..."}, nil)
	require.NoError(t, err)

	// The graph must contain this package and its dependencies, which is what
	// makes Scan able to see transitive imports.
	assert.Contains(t, graph, "go.opentelemetry.io/collector/internal/testutil/importpolicy")
	assert.Contains(t, graph["go.opentelemetry.io/collector/internal/testutil/importpolicy"], "testing")
}

func TestLoadImportGraphNoPatterns(t *testing.T) {
	_, err := loadImportGraph(t.Context(), nil, nil)
	require.ErrorContains(t, err, "no package patterns configured")
}

func TestLoadImportGraphNoMatches(t *testing.T) {
	_, err := loadImportGraph(t.Context(), []string{"./does-not-exist"}, nil)
	require.Error(t, err)
}

func TestListArgs(t *testing.T) {
	assert.Equal(t,
		[]string{"list", "-deps", "-json", "./..."},
		listArgs([]string{"./..."}, nil))

	assert.Equal(t,
		[]string{"list", "-deps", "-json", "-tags", "requirefips,integration", "./...", "."},
		listArgs([]string{"./...", "."}, []string{"requirefips", "integration"}))
}

func TestModuleRoot(t *testing.T) {
	root, err := moduleRoot()
	require.NoError(t, err)

	// The working directory of a test is its package directory, so the module
	// root must be an ancestor of it and contain the module file.
	assert.DirExists(t, root)
	assert.FileExists(t, filepath.Join(root, "go.mod"))
	assert.NotEqual(t, root, mustGetwd(t))
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	return wd
}

func TestScan(t *testing.T) {
	// This module does not depend on golang.org/x/crypto, so a policy that
	// forbids it finds nothing.
	violations := Scan(t, Settings{
		Patterns:  []string{"./..."},
		Forbidden: FIPSForbiddenImports(),
	})
	assert.Empty(t, violations)

	// Forbidding the standard library finds violations, proving Scan reports
	// what go list resolves.
	violations = Scan(t, Settings{
		Patterns:  []string{"./..."},
		Forbidden: []string{"testing"},
	})
	require.NotEmpty(t, violations)
	for _, v := range violations {
		assert.NotEqual(t, "testing", v.Importer, "importers that are themselves forbidden are skipped")
	}
}

func TestFIPSForbiddenImports(t *testing.T) {
	forbidden := FIPSForbiddenImports()
	require.NotEmpty(t, forbidden)
	assert.True(t, isForbidden("golang.org/x/crypto/chacha20poly1305", forbidden))
	assert.True(t, isForbidden("filippo.io/edwards25519", forbidden))
	assert.False(t, isForbidden("vendor/golang.org/x/crypto/chacha20poly1305", forbidden),
		"the copy vendored by the standard library is FIPS-approved")
	assert.False(t, isForbidden("crypto/sha256", forbidden))
}

func TestCheckWithoutForbiddenImportsFails(t *testing.T) {
	// A check without a deny list can never fail, so it is rejected instead of
	// silently passing.
	_, err := scan(t.Context(), Settings{Patterns: []string{"./..."}})
	require.ErrorContains(t, err, "no forbidden import prefixes configured")
}
