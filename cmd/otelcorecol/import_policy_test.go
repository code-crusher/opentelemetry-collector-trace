// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	"go.opentelemetry.io/collector/internal/testutil/importpolicy"
)

// TestFIPSCompliance checks that the binary does not depend on cryptography
// that is implemented outside of the Go FIPS 140-3 module. Go links every
// package that a dependency imports, so a single transitive import can make the
// binary non-compliant even when the collector never calls into it.
//
// The check resolves the import graph with the requirefips build tag, so it
// covers the same dependencies as a FIPS build of the binary.
func TestFIPSCompliance(t *testing.T) {
	importpolicy.Check(t, importpolicy.Settings{
		Patterns:  []string{"."},
		Forbidden: importpolicy.FIPSForbiddenImports(),
		Tags:      []string{"requirefips"},
		Known: []importpolicy.KnownViolation{
			{
				Importer: "github.com/foxboron/go-tpm-keyfiles",
				Imported: "golang.org/x/crypto/cryptobyte",
				Reason: "The TPM key parser uses cryptobyte to read ASN.1 structures. " +
					"cryptobyte only encodes and decodes data and performs no cryptography.",
			},
		},
	})
}
