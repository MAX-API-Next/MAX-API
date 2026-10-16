/*
Copyright (C) 2023-2026 MAX-API-Next

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact https://github.com/MAX-API-Next/MAX-API/issues
*/

package taskplugin

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const APIVersion = 1

var (
	manifestKeyPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,29}$`)
	semverPattern        = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
	identifierPattern    = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)
	allowedRouteMethods  = map[string]struct{}{"GET": {}, "POST": {}, "PUT": {}, "PATCH": {}, "DELETE": {}}
	allowedRouteTypes    = map[string]struct{}{"submit": {}, "query": {}, "dynamic": {}}
	allowedProtocolNames = map[string]struct{}{"openai_responses": {}, "openai_video": {}, "openai_image": {}}
	allowedProtocolModes = map[string]struct{}{"stream": {}, "sync": {}, "background": {}}
	allowedUsageTypes    = map[string]struct{}{"number": {}, "boolean": {}, "enum": {}}
	allowedUsageUnits    = map[string]struct{}{"second": {}, "count": {}, "token": {}, "credit": {}}
)

// Manifest is the host-owned portion of the Task Plugin v1 contract. It is
// deliberately data-only: executing plugin code belongs to a later sandboxed
// driver layer and is not part of validation or registry publication.
type Manifest struct {
	APIVersion   int
	Key          string
	Name         string
	Version      string
	Models       []string
	Protocols    []ProtocolClaim
	Routes       []Route
	BaseURL      string
	AllowedHosts []string
	Upstreams    []string
	UsageSchema  map[string]UsageField
}

type ProtocolClaim struct {
	Name     string
	Supports []string
	Bare     bool
}

type Route struct {
	Method       string
	Path         string
	Type         string
	Decode       string
	Render       string
	TaskIDParam  string
	Models       []string
	RetainResult *bool
}

type UsageField struct {
	Type string
	Unit string
	Enum []string
}

func ValidateManifest(manifest Manifest) error {
	if manifest.APIVersion != APIVersion {
		return fmt.Errorf("unsupported task plugin api version %d", manifest.APIVersion)
	}
	if !manifestKeyPattern.MatchString(manifest.Key) {
		return fmt.Errorf("invalid task plugin key %q", manifest.Key)
	}
	if strings.TrimSpace(manifest.Name) == "" {
		return fmt.Errorf("task plugin name is required")
	}
	if !semverPattern.MatchString(manifest.Version) {
		return fmt.Errorf("invalid task plugin version %q", manifest.Version)
	}
	modelSet, err := validateModels(manifest.Models)
	if err != nil {
		return err
	}
	if err := validateProtocols(manifest.Protocols); err != nil {
		return err
	}
	if err := validateRoutes(manifest.Routes, modelSet); err != nil {
		return err
	}
	if err := validateHTTPBaseURL(manifest.BaseURL); err != nil {
		return err
	}
	if err := validateAllowedHosts(manifest.AllowedHosts); err != nil {
		return err
	}
	if err := validateUpstreams(manifest.Upstreams); err != nil {
		return err
	}
	return validateUsageSchema(manifest.UsageSchema)
}

func validateModels(models []string) (map[string]struct{}, error) {
	if len(models) == 0 {
		return nil, fmt.Errorf("task plugin must declare at least one model")
	}
	set := make(map[string]struct{}, len(models))
	for _, model := range models {
		if model == "" {
			return nil, fmt.Errorf("task plugin model must not be empty")
		}
		if model != strings.TrimSpace(model) {
			return nil, fmt.Errorf("task plugin model %q must not have surrounding whitespace", model)
		}
		if _, exists := set[model]; exists {
			return nil, fmt.Errorf("duplicate task plugin model %q", model)
		}
		set[model] = struct{}{}
	}
	return set, nil
}

func validateProtocols(protocols []ProtocolClaim) error {
	seen := make(map[string]struct{}, len(protocols))
	for _, protocol := range protocols {
		if _, ok := allowedProtocolNames[protocol.Name]; !ok {
			return fmt.Errorf("unsupported task plugin protocol %q", protocol.Name)
		}
		if _, exists := seen[protocol.Name]; exists {
			return fmt.Errorf("duplicate task plugin protocol %q", protocol.Name)
		}
		seen[protocol.Name] = struct{}{}
		if protocol.Bare {
			if protocol.Name == "openai_responses" || len(protocol.Supports) > 0 {
				return fmt.Errorf("bare task plugin protocol %q cannot declare modes", protocol.Name)
			}
			continue
		}
		if protocol.Name == "openai_responses" && len(protocol.Supports) == 0 {
			return fmt.Errorf("task plugin protocol %q requires supports", protocol.Name)
		}
		modes := make(map[string]struct{}, len(protocol.Supports))
		for _, mode := range protocol.Supports {
			if _, ok := allowedProtocolModes[mode]; !ok {
				return fmt.Errorf("unsupported task plugin protocol mode %q", mode)
			}
			if _, exists := modes[mode]; exists {
				return fmt.Errorf("duplicate task plugin protocol mode %q", mode)
			}
			modes[mode] = struct{}{}
		}
	}
	return nil
}

func validateRoutes(routes []Route, models map[string]struct{}) error {
	seen := make(map[string]struct{}, len(routes))
	for _, route := range routes {
		method := strings.ToUpper(strings.TrimSpace(route.Method))
		if _, ok := allowedRouteMethods[method]; !ok {
			return fmt.Errorf("unsupported task plugin route method %q", route.Method)
		}
		if _, ok := allowedRouteTypes[route.Type]; !ok {
			return fmt.Errorf("unsupported task plugin route type %q", route.Type)
		}
		shape, err := routeShape(route.Path)
		if err != nil {
			return err
		}
		conflictKey := method + " " + shape
		if _, exists := seen[conflictKey]; exists {
			return fmt.Errorf("duplicate task plugin route %q", conflictKey)
		}
		seen[conflictKey] = struct{}{}
		if route.Type == "query" {
			if route.Decode != "" {
				return fmt.Errorf("query route %q must not declare decode", route.Path)
			}
			if !identifierPattern.MatchString(route.Render) {
				return fmt.Errorf("query route %q requires a valid render hook", route.Path)
			}
			if route.TaskIDParam != "" && !routeHasParameter(route.Path, route.TaskIDParam) {
				return fmt.Errorf("query route %q task id parameter %q is not in the path", route.Path, route.TaskIDParam)
			}
		} else {
			if !identifierPattern.MatchString(route.Decode) || !identifierPattern.MatchString(route.Render) {
				return fmt.Errorf("route %q requires valid decode and render hooks", route.Path)
			}
		}
		routeModels := make(map[string]struct{}, len(route.Models))
		for _, model := range route.Models {
			if _, ok := models[model]; !ok {
				return fmt.Errorf("route %q references undeclared model %q", route.Path, model)
			}
			if _, exists := routeModels[model]; exists {
				return fmt.Errorf("route %q declares duplicate model %q", route.Path, model)
			}
			routeModels[model] = struct{}{}
		}
	}
	return nil
}

func routeHasParameter(path, parameter string) bool {
	for _, part := range strings.Split(path, "/") {
		if strings.HasPrefix(part, ":") && strings.TrimPrefix(part, ":") == parameter {
			return true
		}
	}
	return false
}

func routeShape(rawPath string) (string, error) {
	if rawPath == "" || !strings.HasPrefix(rawPath, "/") || strings.ContainsAny(rawPath, "?#") {
		return "", fmt.Errorf("invalid task plugin route path %q", rawPath)
	}
	parsed, err := url.ParseRequestURI(rawPath)
	if err != nil || parsed.Path != rawPath || strings.Contains(rawPath, "//") {
		return "", fmt.Errorf("invalid task plugin route path %q", rawPath)
	}
	parts := strings.Split(rawPath, "/")
	for index, part := range parts {
		if strings.HasPrefix(part, ":") {
			if len(part) == 1 || !identifierPattern.MatchString(part[1:]) {
				return "", fmt.Errorf("invalid task plugin route parameter in %q", rawPath)
			}
			parts[index] = ":param"
		} else if strings.Contains(part, "*") {
			return "", fmt.Errorf("wildcard task plugin route is not allowed in %q", rawPath)
		}
	}
	return strings.Join(parts, "/"), nil
}

func validateHTTPBaseURL(rawURL string) error {
	if rawURL == "" {
		return nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" {
		return fmt.Errorf("invalid task plugin base url")
	}
	if !isASCII(parsed.Host) {
		return fmt.Errorf("task plugin base url host must be ascii")
	}
	return nil
}

func validateAllowedHosts(hosts []string) error {
	seen := make(map[string]struct{}, len(hosts))
	for _, host := range hosts {
		if host == "" || strings.ContainsAny(host, "\\/ ") || strings.IndexFunc(host, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0 {
			return fmt.Errorf("invalid task plugin allowed host %q", host)
		}
		parsed, err := url.Parse("//" + host)
		if err != nil || parsed.Host != host || parsed.Hostname() == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("invalid task plugin allowed host %q", host)
		}
		if _, exists := seen[strings.ToLower(host)]; exists {
			return fmt.Errorf("duplicate task plugin allowed host %q", host)
		}
		seen[strings.ToLower(host)] = struct{}{}
	}
	return nil
}

func validateUpstreams(upstreams []string) error {
	seen := make(map[string]struct{}, len(upstreams))
	for _, upstream := range upstreams {
		if upstream != "vendor" && upstream != "new_api" {
			return fmt.Errorf("unsupported task plugin upstream %q", upstream)
		}
		if _, exists := seen[upstream]; exists {
			return fmt.Errorf("duplicate task plugin upstream %q", upstream)
		}
		seen[upstream] = struct{}{}
	}
	return nil
}

func validateUsageSchema(schema map[string]UsageField) error {
	for name, field := range schema {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("task plugin usage field name must not be empty")
		}
		if _, ok := allowedUsageTypes[field.Type]; !ok {
			return fmt.Errorf("unsupported task plugin usage field type %q", field.Type)
		}
		if field.Type == "number" {
			if _, ok := allowedUsageUnits[field.Unit]; !ok {
				return fmt.Errorf("unsupported task plugin usage unit %q", field.Unit)
			}
			if len(field.Enum) > 0 {
				return fmt.Errorf("task plugin numeric usage field %q must not declare enum values", name)
			}
		} else if field.Type == "enum" {
			if field.Unit != "" || len(field.Enum) == 0 {
				return fmt.Errorf("task plugin enum usage field %q requires values and no unit", name)
			}
			seen := make(map[string]struct{}, len(field.Enum))
			for _, value := range field.Enum {
				if strings.TrimSpace(value) == "" {
					return fmt.Errorf("task plugin enum usage field %q contains an empty value", name)
				}
				if _, exists := seen[value]; exists {
					return fmt.Errorf("task plugin enum usage field %q contains duplicate value %q", name, value)
				}
				seen[value] = struct{}{}
			}
		} else if field.Unit != "" || len(field.Enum) > 0 {
			return fmt.Errorf("task plugin usage field %q must not declare a unit or enum values", name)
		}
	}
	return nil
}

func isASCII(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r > 127 {
			return false
		}
	}
	return true
}
