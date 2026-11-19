// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package filter // import "go.opentelemetry.io/collector/filter"

import (
	"errors"
	"regexp"
	"strings"
)

// Config configures the matching behavior of a Filter.
// Exactly one of Strict, Regex, or Pattern must be set.
type Config struct {
	Strict string `mapstructure:"strict"`
	Regex  string `mapstructure:"regexp"`
	// Pattern is a wildcard pattern in which '?' matches exactly one character
	// and '*' matches zero or more characters. All other characters, including
	// regular expression metacharacters such as '.', are matched literally.
	Pattern string `mapstructure:"pattern"`
	// prevent unkeyed literal initialization
	_ struct{}
}

func (c Config) Validate() error {
	set := 0
	for _, value := range []string{c.Strict, c.Regex, c.Pattern} {
		if value != "" {
			set++
		}
	}
	if set == 0 {
		return errors.New("must specify one of strict, regex, or pattern")
	}
	if set > 1 {
		return errors.New("only one of strict, regex, or pattern can be set")
	}

	if c.Regex != "" {
		_, err := regexp.Compile(c.Regex)
		if err != nil {
			return err
		}
	}

	return nil
}

// patternToRegexp converts a wildcard pattern into an anchored regular expression.
// The only wildcard characters are '?', which matches exactly one character, and
// '*', which matches zero or more characters. Every other character is matched
// literally.
func patternToRegexp(pattern string) string {
	re := regexp.QuoteMeta(pattern)
	re = "^" + re + "$"
	re = strings.ReplaceAll(re, `\?`, ".")
	re = strings.ReplaceAll(re, `\*`, ".*")
	return re
}

type combinedFilter struct {
	stricts map[any]struct{}
	regexes []*regexp.Regexp
}

// CreateFilter creates a Filter out of a set of Config configuration objects.
func CreateFilter(configs []Config) Filter {
	cf := &combinedFilter{
		stricts: make(map[any]struct{}),
	}
	for _, config := range configs {
		if config.Strict != "" {
			cf.stricts[config.Strict] = struct{}{}
		}

		if config.Regex != "" {
			// Validate() call above ensures that the regex is valid.
			re := regexp.MustCompile(config.Regex)
			cf.regexes = append(cf.regexes, re)
		}

		if config.Pattern != "" {
			cf.regexes = append(cf.regexes, regexp.MustCompile(patternToRegexp(config.Pattern)))
		}
	}
	return cf
}

func (cf *combinedFilter) Matches(toMatch any) bool {
	_, ok := cf.stricts[toMatch]
	if ok {
		return ok
	}
	if str, ok := toMatch.(string); ok {
		for _, re := range cf.regexes {
			if re.MatchString(str) {
				return true
			}
		}
	}
	return false
}
