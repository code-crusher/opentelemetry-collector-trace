# Import policy

The `go.opentelemetry.io/collector/internal/testutil/importpolicy` package checks
the transitive import graph of Go packages against a deny list of import path
prefixes. It is meant to be used from tests to keep a module or a binary free of
dependencies that violate a compliance policy.

The package is policy agnostic. It ships no default deny list other than
[`FIPSForbiddenImports`](fips.go), which returns the well-known packages that
implement cryptography outside of the Go FIPS 140-3 module.

## Why

Go links every package that a dependency imports, even when the code path is
never called. A single transitive import can therefore make a binary fail a
compliance requirement such as FIPS 140-3. [`GODEBUG=fips140=only`](https://go.dev/doc/security/fips140#fips-140-3-mode)
surfaces non-compliant calls made by the code that the tests exercise, but it
cannot see a dependency that is linked without being called. Scanning the import
graph closes that gap. See [#15689](https://github.com/open-telemetry/opentelemetry-collector/issues/15689).

## Usage

Add a test next to the code that is shipped. For a binary, the test belongs in
the module that contains its `main` package:

```go
func TestFIPSCompliance(t *testing.T) {
	importpolicy.Check(t, importpolicy.Settings{
		Patterns:  []string{"."},
		Forbidden: importpolicy.FIPSForbiddenImports(),
		Tags:      []string{"requirefips"},
		Known: []importpolicy.KnownViolation{
			{
				Importer: "github.com/foxboron/go-tpm-keyfiles",
				Imported: "golang.org/x/crypto/cryptobyte",
				Reason:   "cryptobyte only encodes and decodes data and performs no cryptography.",
			},
		},
	})
}
```

`Patterns` are passed to `go list` and resolved from the root of the current
module, so `./...` covers the whole module. `Tags` selects the build tags used to
resolve imports.

`Check` fails the test in two cases:

- a forbidden import is not covered by `Known`, which means a dependency was
  added or upgraded without a review of the policy implications;
- a `Known` entry no longer matches a violation, which means an exception can be
  removed.

`KnownViolation.Importer` and `KnownViolation.Imported` are import paths or
import path prefixes, so a module root covers all of its packages. An empty value
matches any package in that position. The `Reason` is logged for every accepted
violation so that the justification is visible when the test runs.

Use `Scan` when the violations need to be reported differently:

```go
violations := importpolicy.Scan(t, importpolicy.Settings{
	Patterns:  []string{"./..."},
	Forbidden: importpolicy.FIPSForbiddenImports(),
})
```

The importers that are themselves forbidden are not reported, so the result only
contains the packages that pull a forbidden module into the graph.

## Conventions

Use `import_policy_test.go` as the file name and `TestFIPSCompliance` as the test
name for FIPS checks. A consistent name makes the checks easy to find and to run
across modules:

```shell
go test -tags requirefips -run TestFIPSCompliance ./...
```
