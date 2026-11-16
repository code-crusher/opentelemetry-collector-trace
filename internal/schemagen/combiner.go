// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package schemagen // import "go.opentelemetry.io/collector/internal/schemagen"

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
)

type CollectorSection string

const (
	CollectorSectionReceivers  CollectorSection = "receivers"
	CollectorSectionProcessors CollectorSection = "processors"
	CollectorSectionExporters  CollectorSection = "exporters"
	CollectorSectionConnectors CollectorSection = "connectors"
	CollectorSectionExtensions CollectorSection = "extensions"
	CollectorSectionService    CollectorSection = "service"
)

type CollectorComponentSchema struct {
	Type           string
	DeprecatedType string
	Schema         *JSONSchema
}

type CollectorSchemaParts struct {
	Receivers  []CollectorComponentSchema
	Processors []CollectorComponentSchema
	Exporters  []CollectorComponentSchema
	Connectors []CollectorComponentSchema
	Extensions []CollectorComponentSchema
}

func CombineCollectorSchema(parts CollectorSchemaParts) (*JSONSchema, error) {
	sections := []struct {
		name       CollectorSection
		components []CollectorComponentSchema
	}{
		{name: CollectorSectionReceivers, components: parts.Receivers},
		{name: CollectorSectionProcessors, components: parts.Processors},
		{name: CollectorSectionExporters, components: parts.Exporters},
		{name: CollectorSectionConnectors, components: parts.Connectors},
		{name: CollectorSectionExtensions, components: parts.Extensions},
	}

	properties := make(map[string]*JSONSchema, len(sections)+1)
	for _, section := range sections {
		schema, err := combineCollectorComponentSection(section.name, section.components)
		if err != nil {
			return nil, err
		}
		properties[string(section.name)] = schema
	}

	service, err := combineCollectorServiceSection(parts)
	if err != nil {
		return nil, err
	}
	properties[string(CollectorSectionService)] = service

	return &JSONSchema{
		Schema:     schemaVersion,
		Type:       "object",
		Properties: properties,
	}, nil
}

// pipelineSignals mirrors the signals supported by pipeline.Signal.
var pipelineSignals = []string{"traces", "metrics", "logs", "profiles"}

// combineCollectorServiceSection builds the schema for the "service" section
// from the same component parts as the other sections, mirroring the checks the
// collector performs at startup (see otelcol.Config.Validate and
// pipelines.PipelineConfig.Validate):
//   - pipelines may only reference components compiled into the distribution,
//     where receivers and exporters may additionally reference connectors,
//   - each pipeline requires at least one receiver and one exporter,
//   - a pipeline must not reference the same processor twice,
//   - the service extensions list may only reference configured extensions.
//
// The "telemetry" section is defined by the distribution's telemetry factory
// and cannot be constrained from CollectorSchemaParts, so it is left
// unconstrained.
func combineCollectorServiceSection(parts CollectorSchemaParts) (*JSONSchema, error) {
	pipelines, err := combineCollectorPipelinesSection(parts)
	if err != nil {
		return nil, err
	}

	return &JSONSchema{
		Type: "object",
		Properties: map[string]*JSONSchema{
			"telemetry":  {},
			"extensions": combineComponentReferenceList(parts.Extensions),
			"pipelines":  pipelines,
		},
		AdditionalProperties: &JSONSchema{Not: &JSONSchema{}},
	}, nil
}

func combineCollectorPipelinesSection(parts CollectorSchemaParts) (*JSONSchema, error) {
	pipeline, err := combineCollectorPipelineConfig(parts)
	if err != nil {
		return nil, err
	}

	return &JSONSchema{
		AnyOf: []*JSONSchema{
			{
				Type: "object",
				PatternProperties: map[string]*JSONSchema{
					collectorPipelineKeyPattern(): pipeline,
				},
				AdditionalProperties: &JSONSchema{Not: &JSONSchema{}},
			},
			{Type: "null"},
		},
	}, nil
}

func combineCollectorPipelineConfig(parts CollectorSchemaParts) (*JSONSchema, error) {
	receivers, err := combineComponentReference(append(slices.Clone(parts.Receivers), parts.Connectors...))
	if err != nil {
		return nil, fmt.Errorf("service pipeline receivers: %w", err)
	}
	processors, err := combineComponentReference(parts.Processors)
	if err != nil {
		return nil, fmt.Errorf("service pipeline processors: %w", err)
	}
	exporters, err := combineComponentReference(append(slices.Clone(parts.Exporters), parts.Connectors...))
	if err != nil {
		return nil, fmt.Errorf("service pipeline exporters: %w", err)
	}

	return &JSONSchema{
		Type: "object",
		Properties: map[string]*JSONSchema{
			"receivers":  {Type: "array", Items: receivers, MinItems: intPtr(1)},
			"processors": {Type: "array", Items: processors, UniqueItems: true},
			"exporters":  {Type: "array", Items: exporters, MinItems: intPtr(1)},
		},
		Required:             []string{"receivers", "exporters"},
		AdditionalProperties: &JSONSchema{Not: &JSONSchema{}},
	}, nil
}

// combineComponentReference builds a schema accepting the identifiers of the
// given components, in both "type" and "type/name" form, including deprecated
// type aliases.
func combineComponentReference(components []CollectorComponentSchema) (*JSONSchema, error) {
	patterns := make([]string, 0, len(components)*2)
	for _, component := range components {
		if component.Type == "" {
			return nil, errors.New("component type must not be empty")
		}
		patterns = append(patterns, collectorComponentPattern(component.Type))
		if component.DeprecatedType != "" {
			patterns = append(patterns, collectorComponentPattern(component.DeprecatedType))
		}
	}
	patterns = slices.Compact(slices.Sorted(slices.Values(patterns)))

	if len(patterns) == 1 {
		return &JSONSchema{Type: "string", Pattern: patterns[0]}, nil
	}

	return &JSONSchema{
		Type: "string",
		AnyOf: slices.Collect(func(yield func(*JSONSchema) bool) {
			for _, pattern := range patterns {
				if !yield(&JSONSchema{Type: "string", Pattern: pattern}) {
					return
				}
			}
		}),
	}, nil
}

// combineComponentReferenceList builds a schema for a list of component
// references, e.g. the service "extensions" list. The null form is accepted
// because a YAML list with no items unmarshals as null.
func combineComponentReferenceList(components []CollectorComponentSchema) *JSONSchema {
	item, err := combineComponentReference(components)
	if err != nil {
		// The component sections are validated before the service section is
		// assembled, so an empty component type is already rejected there.
		return &JSONSchema{}
	}

	return &JSONSchema{
		AnyOf: []*JSONSchema{
			{Type: "array", Items: item, UniqueItems: true},
			{Type: "null"},
		},
	}
}

// collectorPipelineKeyPattern matches pipeline IDs in "signal" or
// "signal/name" form, where the name follows the same character restrictions
// as pipeline ID names (see pipeline.validateName).
func collectorPipelineKeyPattern() string {
	return "^(?:" + strings.Join(pipelineSignals, "|") + ")(?:/[^\\pZ\\pC\\pS]+)?$"
}

func combineCollectorComponentSection(section CollectorSection, components []CollectorComponentSchema) (*JSONSchema, error) {
	sectionSchema := newCollectorComponentSection()

	for _, component := range components {
		if component.Type == "" {
			return nil, fmt.Errorf("%s component type must not be empty", section)
		}

		if err := addCollectorComponentPattern(sectionSchema, component.Type, component.Schema, false); err != nil {
			return nil, fmt.Errorf("%s component %q: %w", section, component.Type, err)
		}

		if component.DeprecatedType != "" {
			if err := addCollectorComponentPattern(sectionSchema, component.DeprecatedType, component.Schema, true); err != nil {
				return nil, fmt.Errorf("%s component %q deprecated type %q: %w", section, component.Type, component.DeprecatedType, err)
			}
		}
	}

	return sectionSchema, nil
}

func addCollectorComponentPattern(section *JSONSchema, componentType string, schema *JSONSchema, deprecated bool) error {
	pattern := collectorComponentPattern(componentType)
	if _, exists := section.PatternProperties[pattern]; exists {
		return fmt.Errorf("duplicate component identifier %q", componentType)
	}

	patternSchema := cloneOrEmptySchema(schema)
	if schema != nil && !reflect.DeepEqual(*schema, JSONSchema{}) {
		patternSchema = &JSONSchema{
			AnyOf: []*JSONSchema{patternSchema, {Type: "null"}},
		}
	}
	if deprecated {
		patternSchema.Deprecated = true
	}
	section.PatternProperties[pattern] = patternSchema

	return nil
}

func collectorComponentPattern(componentType string) string {
	return "^" + regexp.QuoteMeta(componentType) + "(?:/.+)?$"
}

func newCollectorComponentSection() *JSONSchema {
	return &JSONSchema{
		Type:                 "object",
		PatternProperties:    map[string]*JSONSchema{},
		AdditionalProperties: &JSONSchema{Not: &JSONSchema{}},
	}
}

func cloneOrEmptySchema(schema *JSONSchema) *JSONSchema {
	if schema == nil {
		return &JSONSchema{}
	}

	cloned := *schema
	return &cloned
}

func intPtr(v int) *int {
	return &v
}
