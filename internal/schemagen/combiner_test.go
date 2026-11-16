// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package schemagen

import (
	"encoding/json"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestCombineCollectorSchema_LayoutAndValidation(t *testing.T) {
	t.Parallel()

	schema, err := CombineCollectorSchema(CollectorSchemaParts{
		Receivers: []CollectorComponentSchema{
			{
				Type: "otlp",
				Schema: &JSONSchema{
					Type: "object",
					Properties: map[string]*JSONSchema{
						"endpoint": {Type: "string"},
					},
					Required: []string{"endpoint"},
				},
			},
		},
		Processors: []CollectorComponentSchema{
			{
				Type: "batch",
				Schema: &JSONSchema{
					Type: "object",
					Properties: map[string]*JSONSchema{
						"timeout": {Type: "string"},
					},
				},
			},
		},
	})
	require.NoError(t, err)
	require.Contains(t, schema.Properties, "receivers")
	require.Contains(t, schema.Properties, "processors")
	require.Contains(t, schema.Properties, "exporters")
	require.Contains(t, schema.Properties, "connectors")
	require.Contains(t, schema.Properties, "extensions")
	require.Contains(t, schema.Properties, "service")

	compiled := compileSchema(t, schema)

	require.NoError(t, compiled.Validate(unmarshalJSON(t, `{
		"receivers": {
			"otlp": {"endpoint": "localhost:4317"},
			"otlp/secondary": {"endpoint": "localhost:4318"}
		},
		"processors": {
			"batch": {"timeout": "5s"}
		},
		"service": {
			"pipelines": {}
		}
	}`)))

	err = compiled.Validate(unmarshalJSON(t, `{
		"receivers": {
			"otlp": {}
		}
	}`))
	require.Error(t, err)

	err = compiled.Validate(unmarshalJSON(t, `{
		"receivers": {
			"prometheus": {}
		}
	}`))
	require.Error(t, err)
}

func TestCombineCollectorSchema_DeprecatedTypeAndMissingSchema(t *testing.T) {
	t.Parallel()

	schema, err := CombineCollectorSchema(CollectorSchemaParts{
		Exporters: []CollectorComponentSchema{
			{
				Type:           "otlp_http",
				DeprecatedType: "otlphttp",
				Schema: &JSONSchema{
					Type: "object",
					Properties: map[string]*JSONSchema{
						"endpoint": {Type: "string"},
					},
					Required: []string{"endpoint"},
				},
			},
			{
				Type: "debug",
			},
		},
	})
	require.NoError(t, err)

	exporters := schema.Properties[string(CollectorSectionExporters)]
	require.NotNil(t, exporters)
	require.Len(t, exporters.PatternProperties, 3)
	require.True(t, exporters.PatternProperties[collectorComponentPattern("otlphttp")].Deprecated)
	require.Empty(t, exporters.PatternProperties[collectorComponentPattern("debug")].Type)

	compiled := compileSchema(t, schema)

	require.NoError(t, compiled.Validate(unmarshalJSON(t, `{
		"exporters": {
			"otlp_http": {"endpoint": "https://example.test"},
			"otlphttp/legacy": {"endpoint": "https://example.test"},
			"debug/custom": {"verbosity": "detailed"}
		}
	}`)))

	err = compiled.Validate(unmarshalJSON(t, `{
		"exporters": {
			"nope": {}
		}
	}`))
	require.Error(t, err)
}

func TestCombineCollectorSchema_DuplicateIdentifiers(t *testing.T) {
	t.Parallel()

	_, err := CombineCollectorSchema(CollectorSchemaParts{
		Receivers: []CollectorComponentSchema{
			{Type: "otlp"},
			{Type: "otlp"},
		},
	})
	require.ErrorContains(t, err, `duplicate component identifier "otlp"`)

	_, err = CombineCollectorSchema(CollectorSchemaParts{
		Exporters: []CollectorComponentSchema{
			{Type: "otlp_http", DeprecatedType: "otlphttp"},
			{Type: "otlphttp"},
		},
	})
	require.ErrorContains(t, err, `duplicate component identifier "otlphttp"`)
}

func TestCombineCollectorSchema_EmptyComponentType(t *testing.T) {
	t.Parallel()

	_, err := CombineCollectorSchema(CollectorSchemaParts{
		Connectors: []CollectorComponentSchema{
			{Type: ""},
		},
	})
	require.ErrorContains(t, err, "component type must not be empty")
}

func TestCombineCollectorSchema_DeprecatedTypeDuplicatesMainType(t *testing.T) {
	t.Parallel()

	_, err := CombineCollectorSchema(CollectorSchemaParts{
		Extensions: []CollectorComponentSchema{
			{Type: "health_check"},
			{Type: "healthcheck", DeprecatedType: "health_check"},
		},
	})
	require.ErrorContains(t, err, `duplicate component identifier "health_check"`)
}

func TestCombineCollectorSchema_NullComponentBodyIsAccepted(t *testing.T) {
	t.Parallel()

	schema, err := CombineCollectorSchema(CollectorSchemaParts{
		Receivers: []CollectorComponentSchema{
			{
				Type: "otlp",
				Schema: &JSONSchema{
					Type: "object",
					Properties: map[string]*JSONSchema{
						"endpoint": {Type: "string"},
					},
				},
			},
		},
	})
	require.NoError(t, err)

	compiled := compileSchema(t, schema)

	require.NoError(t, compiled.Validate(unmarshalJSON(t, `{
		"receivers": {"otlp": null}
	}`)))

	require.NoError(t, compiled.Validate(unmarshalYAML(t, "receivers:\n  otlp:\n")))

	require.NoError(t, compiled.Validate(unmarshalJSON(t, `{
		"receivers": {"otlp/secondary": null}
	}`)))

	require.NoError(t, compiled.Validate(unmarshalJSON(t, `{
		"receivers": {"otlp": {"endpoint": "localhost:4317"}}
	}`)))
}

func TestCombineCollectorSchema_NullBranchDoesNotSwallowTypeErrors(t *testing.T) {
	t.Parallel()

	schema, err := CombineCollectorSchema(CollectorSchemaParts{
		Receivers: []CollectorComponentSchema{
			{
				Type: "otlp",
				Schema: &JSONSchema{
					Type: "object",
					Properties: map[string]*JSONSchema{
						"endpoint": {Type: "string"},
					},
					Required: []string{"endpoint"},
				},
			},
		},
	})
	require.NoError(t, err)

	compiled := compileSchema(t, schema)

	for name, config := range map[string]string{
		"scalar body":         `{"receivers": {"otlp": 5}}`,
		"string body":         `{"receivers": {"otlp": "enabled"}}`,
		"array body":          `{"receivers": {"otlp": []}}`,
		"wrong property type": `{"receivers": {"otlp": {"endpoint": 4317}}}`,
		"missing required":    `{"receivers": {"otlp": {}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, compiled.Validate(unmarshalJSON(t, config)))
		})
	}
}

func TestCombineCollectorSchema_NilSchemaIsNotWrapped(t *testing.T) {
	t.Parallel()

	schema, err := CombineCollectorSchema(CollectorSchemaParts{
		Exporters: []CollectorComponentSchema{
			{Type: "debug"},
		},
	})
	require.NoError(t, err)

	debug := schema.Properties[string(CollectorSectionExporters)].PatternProperties[collectorComponentPattern("debug")]
	require.NotNil(t, debug)
	require.Empty(t, debug.AnyOf)

	data, err := debug.MarshalJSON()
	require.NoError(t, err)
	require.JSONEq(t, `true`, string(data))

	compiled := compileSchema(t, schema)
	require.NoError(t, compiled.Validate(unmarshalJSON(t, `{"exporters": {"debug": null}}`)))
	require.NoError(t, compiled.Validate(unmarshalJSON(t, `{"exporters": {"debug": {"verbosity": "detailed"}}}`)))
}

func TestCombineCollectorSchema_NilSchemaDeprecatedKeepsMarker(t *testing.T) {
	t.Parallel()

	schema, err := CombineCollectorSchema(CollectorSchemaParts{
		Extensions: []CollectorComponentSchema{
			{Type: "health_check", DeprecatedType: "healthcheck"},
		},
	})
	require.NoError(t, err)

	extensions := schema.Properties[string(CollectorSectionExtensions)]
	deprecatedSchema := extensions.PatternProperties[collectorComponentPattern("healthcheck")]
	require.NotNil(t, deprecatedSchema)
	require.True(t, deprecatedSchema.Deprecated)
	require.Empty(t, deprecatedSchema.AnyOf)

	data, err := deprecatedSchema.MarshalJSON()
	require.NoError(t, err)
	require.JSONEq(t, `{"deprecated": true}`, string(data))

	compiled := compileSchema(t, schema)
	require.NoError(t, compiled.Validate(unmarshalJSON(t, `{"extensions": {"healthcheck": null}}`)))
	require.NoError(t, compiled.Validate(unmarshalJSON(t, `{"extensions": {"healthcheck/1": {"endpoint": "x"}}}`)))
}

// distributionParts returns schema parts for a distribution with an otlp
// receiver, a batch processor, an otlp and a debug exporter, a forward
// connector, a health_check extension, and a deprecated exporter alias.
func distributionParts() CollectorSchemaParts {
	return CollectorSchemaParts{
		Receivers: []CollectorComponentSchema{{Type: "otlp"}},
		Processors: []CollectorComponentSchema{
			{Type: "batch"},
		},
		Exporters: []CollectorComponentSchema{
			{Type: "otlp"},
			{Type: "otlp_http", DeprecatedType: "otlphttp"},
			{Type: "debug"},
		},
		Connectors: []CollectorComponentSchema{{Type: "forward"}},
		Extensions: []CollectorComponentSchema{{Type: "health_check"}},
	}
}

func TestCombineCollectorSchema_ServiceSection(t *testing.T) {
	t.Parallel()

	schema, err := CombineCollectorSchema(distributionParts())
	require.NoError(t, err)

	compiled := compileSchema(t, schema)

	t.Run("valid pipelines", func(t *testing.T) {
		require.NoError(t, compiled.Validate(unmarshalJSON(t, `{
			"service": {
				"telemetry": {"metrics": {"level": "basic"}},
				"extensions": ["health_check"],
				"pipelines": {
					"traces": {
						"receivers": ["otlp"],
						"processors": ["batch"],
						"exporters": ["otlp", "debug"]
					},
					"metrics/extra": {
						"receivers": ["otlp", "forward"],
						"exporters": ["forward"]
					},
					"logs": {
						"receivers": ["forward/myconn"],
						"exporters": ["otlp_http"]
					}
				}
			}
		}`)))
	})

	t.Run("pipeline reference to unknown component", func(t *testing.T) {
		for name, config := range map[string]string{
			"unknown receiver": `{
				"service": {"pipelines": {"traces": {
					"receivers": ["prometheus"], "exporters": ["debug"]
				}}}
			}`,
			"unknown processor": `{
				"service": {"pipelines": {"traces": {
					"receivers": ["otlp"], "processors": ["memory_limiter"], "exporters": ["debug"]
				}}}
			}`,
			"unknown exporter": `{
				"service": {"pipelines": {"traces": {
					"receivers": ["otlp"], "exporters": ["zipkin"]
				}}}
			}`,
			"processor as receiver": `{
				"service": {"pipelines": {"traces": {
					"receivers": ["batch"], "exporters": ["debug"]
				}}}
			}`,
			"unknown extension": `{
				"service": {"extensions": ["zpages"]}
			}`,
			"pipeline signal typo": `{
				"service": {"pipelines": {"trace": {
					"receivers": ["otlp"], "exporters": ["debug"]
				}}}
			}`,
			"unknown pipeline key": `{
				"service": {"pipelines": {"traces": {
					"receivers": ["otlp"], "exporters": ["debug"], "unknown": true
				}}}
			}`,
			"unknown service key": `{
				"service": {"unknown_section": true}
			}`,
		} {
			t.Run(name, func(t *testing.T) {
				require.Error(t, compiled.Validate(unmarshalJSON(t, config)))
			})
		}
	})

	t.Run("pipelines without exporters", func(t *testing.T) {
		require.Error(t, compiled.Validate(unmarshalJSON(t, `{
			"service": {"pipelines": {"traces": {"receivers": ["otlp"]}}}
		}`)))
	})

	t.Run("pipelines without receivers", func(t *testing.T) {
		require.Error(t, compiled.Validate(unmarshalJSON(t, `{
			"service": {"pipelines": {"traces": {"exporters": ["debug"]}}}
		}`)))
	})

	t.Run("duplicate processor in pipeline", func(t *testing.T) {
		require.Error(t, compiled.Validate(unmarshalJSON(t, `{
			"service": {"pipelines": {"traces": {
				"receivers": ["otlp"], "processors": ["batch", "batch"], "exporters": ["debug"]
			}}}
		}`)))
	})

	t.Run("deprecated exporter type in pipeline", func(t *testing.T) {
		require.NoError(t, compiled.Validate(unmarshalJSON(t, `{
			"service": {"pipelines": {"traces": {
				"receivers": ["otlp"], "exporters": ["otlphttp"]
			}}}
		}`)))
	})

	t.Run("null extensions list", func(t *testing.T) {
		require.NoError(t, compiled.Validate(unmarshalJSON(t, `{
			"service": {"pipelines": null, "extensions": null}
		}`)))
		require.NoError(t, compiled.Validate(unmarshalYAML(t, "service:\n  pipelines:\n  extensions:\n")))
	})
}

func TestCombineCollectorSchema_ServiceSectionStructural(t *testing.T) {
	t.Parallel()

	schema, err := CombineCollectorSchema(distributionParts())
	require.NoError(t, err)

	service := schema.Properties[string(CollectorSectionService)]
	require.NotNil(t, service)
	require.Equal(t, "object", service.Type)
	require.Contains(t, service.Properties, "telemetry")
	require.Contains(t, service.Properties, "extensions")
	require.Contains(t, service.Properties, "pipelines")
	require.NotNil(t, service.AdditionalProperties.Not)

	// The telemetry section is defined by the distribution's telemetry
	// factory, so it is left unconstrained.
	telemetry, err := service.Properties["telemetry"].MarshalJSON()
	require.NoError(t, err)
	require.JSONEq(t, `true`, string(telemetry))

	pipelines := service.Properties["pipelines"]
	require.NotNil(t, pipelines)
	require.Len(t, pipelines.AnyOf, 2)
	require.Equal(t, "object", pipelines.AnyOf[0].Type)
	require.Equal(t, "null", pipelines.AnyOf[1].Type)

	var pipelinePattern string
	var pipeline *JSONSchema
	for pattern, p := range pipelines.AnyOf[0].PatternProperties {
		pipelinePattern, pipeline = pattern, p
	}
	require.Regexp(t, `^\^\(\?:traces\|metrics\|logs\|profiles\)\(\?:/`, pipelinePattern)
	require.Equal(t, []string{"receivers", "exporters"}, pipeline.Required)
	require.Equal(t, 1, *pipeline.Properties["receivers"].MinItems)
	require.Equal(t, 1, *pipeline.Properties["exporters"].MinItems)
	require.True(t, pipeline.Properties["processors"].UniqueItems)
}

func TestCombineCollectorSchema_ServicePipelineMergesSections(t *testing.T) {
	t.Parallel()

	// A component type may appear in two sections the service section merges,
	// e.g. the same type as exporter and connector: both references stay
	// valid, matching otelcol.Config.Validate which accepts either.
	schema, err := CombineCollectorSchema(CollectorSchemaParts{
		Exporters:  []CollectorComponentSchema{{Type: "otlp"}},
		Connectors: []CollectorComponentSchema{{Type: "otlp"}},
	})
	require.NoError(t, err)

	compiled := compileSchema(t, schema)
	require.NoError(t, compiled.Validate(unmarshalJSON(t, `{
		"service": {"pipelines": {"traces": {
			"receivers": ["otlp"], "exporters": ["otlp"]
		}}}
	}`)))
}

func compileSchema(t *testing.T, schema *JSONSchema) *jsonschema.Schema {
	t.Helper()

	data, err := schema.MarshalJSON()
	require.NoError(t, err)

	compiler := jsonschema.NewCompiler()
	var doc any
	require.NoError(t, json.Unmarshal(data, &doc))
	require.NoError(t, compiler.AddResource("schema.json", doc))

	compiled, err := compiler.Compile("schema.json")
	require.NoError(t, err)

	return compiled
}

func unmarshalJSON(t *testing.T, data string) any {
	t.Helper()

	var value any
	require.NoError(t, json.Unmarshal([]byte(data), &value))

	return value
}

func unmarshalYAML(t *testing.T, data string) any {
	t.Helper()

	var value any
	require.NoError(t, yaml.Unmarshal([]byte(data), &value))

	return value
}
