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
	"sort"
	"strings"
	"sync/atomic"
)

type Registry struct {
	state atomic.Pointer[registryState]
}

type registryState struct {
	generation uint64
	plugins    map[string]Manifest
	routes     map[string]routeBinding
	byMethod   map[string][]routeBinding
	byModel    map[string][]string
}

type Snapshot struct {
	state *registryState
}

type RouteMatch struct {
	PluginKey string
	Manifest  Manifest
	Route     Route
}

func NewRegistry() *Registry {
	registry := &Registry{}
	registry.state.Store(&registryState{
		plugins:  map[string]Manifest{},
		routes:   map[string]routeBinding{},
		byMethod: map[string][]routeBinding{},
		byModel:  map[string][]string{},
	})
	return registry
}

func (registry *Registry) Publish(manifest Manifest) (uint64, error) {
	if registry == nil {
		return 0, fmt.Errorf("task plugin registry is nil")
	}
	if err := ValidateManifest(manifest); err != nil {
		return 0, err
	}
	manifest = cloneManifest(manifest)
	for {
		current := registry.state.Load()
		if _, exists := current.plugins[manifest.Key]; exists {
			return 0, fmt.Errorf("task plugin %q is already published", manifest.Key)
		}
		next := cloneState(current)
		next.generation++
		next.plugins[manifest.Key] = manifest
		for _, route := range manifest.Routes {
			shape, _ := routeShape(route.Path)
			method := strings.ToUpper(strings.TrimSpace(route.Method))
			key := method + " " + shape
			if existing, exists := current.routes[key]; exists {
				return 0, fmt.Errorf("task plugin route %s conflicts with plugin %q", key, existing.pluginKey)
			}
			binding := routeBinding{pluginKey: manifest.Key, route: cloneRoute(route)}
			next.routes[key] = binding
			next.byMethod[method] = append(next.byMethod[method], binding)
		}
		for _, model := range manifest.Models {
			next.byModel[model] = append(next.byModel[model], manifest.Key)
			sort.Strings(next.byModel[model])
		}
		for method := range next.byMethod {
			sort.SliceStable(next.byMethod[method], func(i, j int) bool {
				a, b := next.byMethod[method][i], next.byMethod[method][j]
				sa, sb := routeSpecificity(a.route.Path), routeSpecificity(b.route.Path)
				if sa != sb {
					return sa > sb
				}
				if a.pluginKey != b.pluginKey {
					return a.pluginKey < b.pluginKey
				}
				return a.route.Path < b.route.Path
			})
		}
		if registry.state.CompareAndSwap(current, next) {
			return next.generation, nil
		}
	}
}

func (registry *Registry) Snapshot() Snapshot {
	if registry == nil {
		return Snapshot{}
	}
	return Snapshot{state: registry.state.Load()}
}

func (snapshot Snapshot) Generation() uint64 {
	if snapshot.state == nil {
		return 0
	}
	return snapshot.state.generation
}

func (snapshot Snapshot) Plugin(key string) (Manifest, bool) {
	if snapshot.state == nil {
		return Manifest{}, false
	}
	manifest, ok := snapshot.state.plugins[key]
	if !ok {
		return Manifest{}, false
	}
	return cloneManifest(manifest), true
}

func (snapshot Snapshot) ModelCandidates(model string) []Manifest {
	if snapshot.state == nil {
		return nil
	}
	keys := snapshot.state.byModel[model]
	result := make([]Manifest, 0, len(keys))
	for _, key := range keys {
		if manifest, ok := snapshot.state.plugins[key]; ok {
			result = append(result, cloneManifest(manifest))
		}
	}
	return result
}

func (snapshot Snapshot) MatchRoute(method, requestPath, model string) (RouteMatch, bool) {
	if snapshot.state == nil {
		return RouteMatch{}, false
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	for _, binding := range snapshot.state.byMethod[method] {
		if !routeMatches(binding.route.Path, requestPath) || !routeAllowsModel(binding.route, model) {
			continue
		}
		manifest, ok := snapshot.state.plugins[binding.pluginKey]
		if !ok || !declaresModel(manifest, model) {
			continue
		}
		return RouteMatch{PluginKey: binding.pluginKey, Manifest: cloneManifest(manifest), Route: cloneRoute(binding.route)}, true
	}
	return RouteMatch{}, false
}

type routeBinding struct {
	pluginKey string
	route     Route
}

func cloneState(source *registryState) *registryState {
	next := &registryState{
		generation: source.generation,
		plugins:    make(map[string]Manifest, len(source.plugins)+1),
		routes:     make(map[string]routeBinding, len(source.routes)+1),
		byMethod:   make(map[string][]routeBinding, len(source.byMethod)),
		byModel:    make(map[string][]string, len(source.byModel)),
	}
	for key, manifest := range source.plugins {
		next.plugins[key] = manifest
	}
	for key, binding := range source.routes {
		next.routes[key] = binding
	}
	for method, bindings := range source.byMethod {
		next.byMethod[method] = append([]routeBinding(nil), bindings...)
	}
	for model, keys := range source.byModel {
		next.byModel[model] = append([]string(nil), keys...)
	}
	return next
}

func routeAllowsModel(route Route, model string) bool {
	if len(route.Models) == 0 {
		return true
	}
	for _, allowed := range route.Models {
		if allowed == model {
			return true
		}
	}
	return false
}

func declaresModel(manifest Manifest, model string) bool {
	for _, candidate := range manifest.Models {
		if candidate == model {
			return true
		}
	}
	return false
}

func routeMatches(pattern, requestPath string) bool {
	parsed, err := url.ParseRequestURI(requestPath)
	if err != nil || parsed.Path != requestPath {
		return false
	}
	patternParts := strings.Split(pattern, "/")
	requestParts := strings.Split(requestPath, "/")
	if len(patternParts) != len(requestParts) {
		return false
	}
	for index, part := range patternParts {
		if strings.HasPrefix(part, ":") {
			if requestParts[index] == "" {
				return false
			}
			continue
		}
		if part != requestParts[index] {
			return false
		}
	}
	return true
}

func routeSpecificity(path string) int {
	score := 0
	for _, part := range strings.Split(path, "/") {
		if part != "" && !strings.HasPrefix(part, ":") {
			score++
		}
	}
	return score
}

func cloneManifest(manifest Manifest) Manifest {
	manifest.Models = append([]string(nil), manifest.Models...)
	manifest.Protocols = append([]ProtocolClaim(nil), manifest.Protocols...)
	for index := range manifest.Protocols {
		manifest.Protocols[index].Supports = append([]string(nil), manifest.Protocols[index].Supports...)
	}
	manifest.Routes = append([]Route(nil), manifest.Routes...)
	for index := range manifest.Routes {
		manifest.Routes[index] = cloneRoute(manifest.Routes[index])
	}
	manifest.AllowedHosts = append([]string(nil), manifest.AllowedHosts...)
	manifest.Upstreams = append([]string(nil), manifest.Upstreams...)
	if manifest.UsageSchema != nil {
		schema := manifest.UsageSchema
		manifest.UsageSchema = make(map[string]UsageField, len(manifest.UsageSchema))
		for key, field := range schema {
			field.Enum = append([]string(nil), field.Enum...)
			manifest.UsageSchema[key] = field
		}
	}
	return manifest
}

func cloneRoute(route Route) Route {
	route.Models = append([]string(nil), route.Models...)
	if route.RetainResult != nil {
		value := *route.RetainResult
		route.RetainResult = &value
	}
	return route
}
