// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
// Schema reference: QuantumNous/new-api 8542c9d6, pkg/jsplugin/config_schema.go
// (AGPL-3.0-or-later). MAX exposes scalar, non-secret channel parameters.
package taskplugin

import (
	"bytes"
	"encoding/json"
	"math"
	"regexp"
	"slices"

	"github.com/MAX-API-Next/MAX-API/common"
)

type ChannelConfigField struct {
	Type        string   `json:"type"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Format      string   `json:"format,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Default     any      `json:"default,omitempty"`
	Minimum     *float64 `json:"minimum,omitempty"`
	Maximum     *float64 `json:"maximum,omitempty"`
}

type ChannelConfigSchema struct {
	Type                 string                        `json:"type"`
	Properties           map[string]ChannelConfigField `json:"properties"`
	Required             []string                      `json:"required,omitempty"`
	AdditionalProperties *bool                         `json:"additionalProperties,omitempty"`
}

var channelConfigName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]{0,63}$`)

func (m Manifest) ChannelSchema() (*ChannelConfigSchema, error) {
	if len(m.ChannelConfig) == 0 {
		return nil, nil
	}
	decoder := common.NewJSONDecoder(bytes.NewReader(m.ChannelConfig))
	decoder.DisallowUnknownFields()
	var schema ChannelConfigSchema
	if decoder.Decode(&schema) != nil || schema.Type != "object" || schema.Properties == nil ||
		len(schema.Properties) > 64 || (schema.AdditionalProperties != nil && *schema.AdditionalProperties) {
		return nil, ErrPluginSDK
	}
	for name, field := range schema.Properties {
		if !channelConfigName.MatchString(name) || !field.validDeclaration() {
			return nil, ErrPluginSDK
		}
	}
	for _, name := range schema.Required {
		if _, ok := schema.Properties[name]; !ok {
			return nil, ErrPluginSDK
		}
	}
	return &schema, nil
}

func (field ChannelConfigField) validDeclaration() bool {
	switch field.Type {
	case "string":
		if field.Format != "" && field.Format != "textarea" || field.Minimum != nil || field.Maximum != nil || field.Enum != nil && len(field.Enum) == 0 {
			return false
		}
	case "number", "integer", "boolean":
		if field.Format != "" || field.Enum != nil || (field.Type == "boolean" && (field.Minimum != nil || field.Maximum != nil)) {
			return false
		}
	default:
		return false
	}
	return (field.Minimum == nil || field.Maximum == nil || *field.Minimum <= *field.Maximum) &&
		(field.Default == nil || field.accepts(field.Default))
}

func (field ChannelConfigField) accepts(value any) bool {
	switch typed := value.(type) {
	case string:
		return field.Type == "string" && (field.Enum == nil || slices.Contains(field.Enum, typed))
	case bool:
		return field.Type == "boolean"
	case float64:
		return (field.Type == "number" || field.Type == "integer" && math.Trunc(typed) == typed && math.Abs(typed) <= 9007199254740991) &&
			(field.Minimum == nil || typed >= *field.Minimum) && (field.Maximum == nil || typed <= *field.Maximum)
	default:
		return false
	}
}

// ResolveChannelConfig validates the stored scalar values against the selected
// version and materializes defaults only for execution.
func (m Manifest) ResolveChannelConfig(raw json.RawMessage, upstream string) (json.RawMessage, error) {
	if upstream != "vendor" && upstream != "new_api" ||
		(len(m.Upstreams) > 0 && !slices.Contains(m.Upstreams, upstream)) {
		return nil, ErrBindingRejected
	}
	values := map[string]any{}
	if len(raw) > 16384 || len(raw) != 0 && (common.Unmarshal(raw, &values) != nil || values == nil) {
		return nil, ErrBindingRejected
	}
	schema, err := m.ChannelSchema()
	if err != nil {
		return nil, err
	}
	if schema == nil {
		if len(values) != 0 {
			return nil, ErrBindingRejected
		}
		return json.RawMessage("{}"), nil
	}
	for key, value := range values {
		field, ok := schema.Properties[key]
		if !ok || !field.accepts(value) {
			return nil, ErrBindingRejected
		}
	}
	for key, field := range schema.Properties {
		if _, exists := values[key]; !exists && field.Default != nil {
			values[key] = field.Default
		}
	}
	for _, name := range schema.Required {
		if _, exists := values[name]; !exists {
			return nil, ErrBindingRejected
		}
	}
	return common.Marshal(values)
}
