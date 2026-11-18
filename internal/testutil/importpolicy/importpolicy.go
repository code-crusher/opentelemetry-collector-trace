// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package importpolicy checks the transitive import graph of Go packages against
// a deny list of import path prefixes.
//
// The package is policy agnostic: callers supply the forbidden import prefixes
// and document the violations they accept. It is meant to be used from tests to
// keep a module or binary free of dependencies that violate a compliance policy,
// such as FIPS 140-3. See
// https://github.com/open-telemetry/opentelemetry-collector/issues/15689.
//
// A typical test scans the packages that make up a shipped binary and fails when
// a forbidden package enters the dependency graph:
//
//	func TestFIPSCompliance(t *testing.T) {
//		importpolicy.Check(t, importpolicy.Settings{
//			Patterns: []string{"."},
//			// Packages outside the Go FIPS 140-3 module.
//			Forbidden: []string{"golang.org/x/crypto"},
//			Known: []importpolicy.KnownViolation{
//				{
//					Importer: "example.com/dependency",
//					Imported: "golang.org/x/crypto/chacha20poly1305",
//					Reason:   "The dependency only uses this package to parse stored keys.",
//				},
//			},
//		})
//	}
//
// Every accepted violation must still be found, so a dependency that is upgraded
// or removed cannot silently keep an outdated exception alive.
package importpolicy // import "go.opentelemetry.io/collector/internal/testutil/importpolicy"

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Violation is a direct import of a forbidden package.
type Violation struct {
	// Importer is the package that directly imports Imported.
	Importer string
	// Imported is the forbidden package.
	Imported string
}

func (v Violation) String() string {
	return v.Importer + " -> " + v.Imported
}

// KnownViolation documents a violation that is accepted.
//
// Importer and Imported are import paths or import path prefixes, so a module
// root matches all of its packages. An empty value matches any package in that
// position.
type KnownViolation struct {
	Importer string
	Imported string
	// Reason explains why the violation is accepted. It is logged when the
	// violation is found.
	Reason string
}

func (k KnownViolation) String() string {
	return orAny(k.Importer) + " -> " + orAny(k.Imported)
}

func orAny(prefix string) string {
	if prefix == "" {
		return "<any>"
	}
	return prefix
}

// matches reports whether the known violation accepts v.
func (k KnownViolation) matches(v Violation) bool {
	return matchesPrefix(k.Importer, v.Importer) && matchesPrefix(k.Imported, v.Imported)
}

// Settings configures an import policy check.
type Settings struct {
	// Patterns are the package patterns passed to `go list`, for example
	// []string{"./..."}. They are resolved from the root of the current module
	// and should select the packages that are shipped, so that the check covers
	// the same dependency graph as the built artifacts.
	Patterns []string
	// Forbidden lists the import path prefixes that the scanned packages must
	// not depend on.
	Forbidden []string
	// Known lists the violations that are accepted. Every entry must still be
	// found, otherwise it is reported as stale.
	Known []KnownViolation
	// Tags lists the build tags used to resolve imports, for example
	// []string{"requirefips"}.
	Tags []string
}

// Check scans the packages matched by Settings.Patterns and their dependencies
// and reports, via tb.Errorf, every violation that is not accepted by
// Settings.Known as well as every Settings.Known entry that no longer matches a
// violation.
func Check(tb testing.TB, settings Settings) {
	tb.Helper()

	violations, err := scan(tb.Context(), settings)
	if err != nil {
		tb.Fatalf("failed to scan imports: %v", err)
	}
	check(tb, violations, settings.Known)
}

// Scan resolves the packages matched by Settings.Patterns and their
// dependencies from the root of the current module and returns every direct
// import of a package matching one of Settings.Forbidden.
func Scan(tb testing.TB, settings Settings) []Violation {
	tb.Helper()

	violations, err := scan(tb.Context(), settings)
	if err != nil {
		tb.Fatalf("failed to scan imports: %v", err)
	}
	return violations
}

// reporter is the subset of testing.TB used to report the check results.
type reporter interface {
	Helper()
	Logf(format string, args ...any)
	Errorf(format string, args ...any)
}

// check reports the violations that are not accepted by known and the known
// entries that accept no violation.
func check(r reporter, violations []Violation, known []KnownViolation) {
	r.Helper()

	matches, unmatched, stale := compare(violations, known)
	for _, m := range matches {
		r.Logf("known violation: %s: %s", m.violation, m.known.Reason)
	}
	for _, v := range unmatched {
		r.Errorf("forbidden import is not covered by a known violation: %s; "+
			"add it to Settings.Known with a reason, or remove the dependency", v)
	}
	for _, k := range stale {
		r.Errorf("known violation no longer applies: %s; remove it from Settings.Known", k)
	}
}

// match pairs a violation with the known violation that accepts it.
type match struct {
	violation Violation
	known     KnownViolation
}

// compare pairs violations with known entries, returning the matches, the
// violations that no known entry accepts, and the known entries that accept no
// violation.
func compare(violations []Violation, known []KnownViolation) (matches []match, unmatched []Violation, stale []KnownViolation) {
	accepted := make([]bool, len(known))
	for _, v := range violations {
		found := false
		for i, k := range known {
			if k.matches(v) {
				accepted[i] = true
				found = true
				matches = append(matches, match{violation: v, known: k})
			}
		}
		if !found {
			unmatched = append(unmatched, v)
		}
	}
	for i, k := range known {
		if !accepted[i] {
			stale = append(stale, k)
		}
	}
	return matches, unmatched, stale
}

// scan resolves the import graph and returns the forbidden imports in it.
func scan(ctx context.Context, settings Settings) ([]Violation, error) {
	if len(settings.Forbidden) == 0 {
		// A check without a deny list always passes, which is more likely a
		// configuration mistake than an intentional no-op.
		return nil, errors.New("no forbidden import prefixes configured")
	}
	graph, err := loadImportGraph(ctx, settings.Patterns, settings.Tags)
	if err != nil {
		return nil, err
	}
	return findViolations(graph, settings.Forbidden), nil
}

// findViolations returns every direct import of a forbidden package in graph,
// skipping importers that are themselves forbidden so that only the packages
// pulling a forbidden module into the graph are reported.
func findViolations(graph map[string][]string, forbidden []string) []Violation {
	var violations []Violation
	for importer, imports := range graph {
		if isForbidden(importer, forbidden) {
			continue
		}
		for _, imported := range imports {
			if isForbidden(imported, forbidden) {
				violations = append(violations, Violation{Importer: importer, Imported: imported})
			}
		}
	}
	slices.SortFunc(violations, func(a, b Violation) int {
		if c := strings.Compare(a.Importer, b.Importer); c != 0 {
			return c
		}
		return strings.Compare(a.Imported, b.Imported)
	})
	return violations
}

// isForbidden reports whether pkg matches one of the import path prefixes.
func isForbidden(pkg string, forbidden []string) bool {
	return slices.ContainsFunc(forbidden, func(prefix string) bool {
		return matchesPrefix(prefix, pkg)
	})
}

// matchesPrefix reports whether pkg is the import path prefix or a package
// below it. An empty prefix matches any package.
func matchesPrefix(prefix, pkg string) bool {
	if prefix == "" {
		return true
	}
	prefix = strings.TrimRight(prefix, "/")
	return pkg == prefix || strings.HasPrefix(pkg, prefix+"/")
}

// listedPackage is the subset of the `go list -json` output used by Scan.
type listedPackage struct {
	ImportPath string
	Imports    []string
	// DepOnly is false for the packages matched by the patterns and true for
	// the packages that are only pulled in as dependencies.
	DepOnly bool
}

// listArgs returns the `go list` arguments used to resolve the import graph.
func listArgs(patterns, tags []string) []string {
	args := []string{"list", "-deps", "-json"}
	if len(tags) > 0 {
		args = append(args, "-tags", strings.Join(tags, ","))
	}
	return append(args, patterns...)
}

// loadImportGraph runs `go list -deps -json` from the root of the current
// module and returns the direct imports of every resolved package.
func loadImportGraph(ctx context.Context, patterns, tags []string) (map[string][]string, error) {
	if len(patterns) == 0 {
		return nil, errors.New("no package patterns configured")
	}

	root, err := moduleRoot()
	if err != nil {
		return nil, err
	}

	// The patterns and tags are configured by the test author, not by untrusted input.
	cmd := exec.CommandContext(ctx, "go", listArgs(patterns, tags)...) //nolint:gosec // G204
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go list failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	graph := make(map[string][]string)
	matched := 0
	dec := json.NewDecoder(&stdout)
	for {
		var pkg listedPackage
		if err := dec.Decode(&pkg); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("failed to parse go list output: %w", err)
		}
		graph[pkg.ImportPath] = pkg.Imports
		if !pkg.DepOnly {
			matched++
		}
	}
	if matched == 0 {
		return nil, fmt.Errorf("no packages matched patterns %v", patterns)
	}
	return graph, nil
}

// moduleRoot walks up from the working directory to the nearest directory
// containing a go.mod file. Tests run with the working directory set to their
// package directory, so relative patterns such as ./... need the module root.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get the working directory: %w", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found above %q", dir)
		}
		dir = parent
	}
}
