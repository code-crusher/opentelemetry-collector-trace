// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package importpolicy // import "go.opentelemetry.io/collector/internal/testutil/importpolicy"

// FIPSForbiddenImports returns the import path prefixes of the well-known
// packages that implement cryptography outside of the Go FIPS 140-3 module, and
// that are therefore not allowed in a FIPS 140-3 build of the Collector.
//
// The list is a baseline that callers can extend with prefixes specific to
// their dependencies. The copies of golang.org/x/crypto that the standard
// library vendors for crypto/tls are not matched, because their import paths
// start with "vendor/".
func FIPSForbiddenImports() []string {
	return []string{
		"golang.org/x/crypto",
		"github.com/cloudflare/circl",
		"github.com/ProtonMail/go-crypto",
		"github.com/Azure/go-ntlmssp",
		"github.com/xdg-go/pbkdf2",
		"github.com/youmark/pkcs8",
		"github.com/jcmturner/gokrb5",
		"github.com/jcmturner/aescts",
		"github.com/jcmturner/gofork",
		"filippo.io",
	}
}
