package setting

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/MAX-API-Next/MAX-API/common"
)

const (
	DefaultAutoRouteKey = "auto"
	AutoRoutePrefix     = "auto:"
)

var autoRouteKeyPattern = regexp.MustCompile(`^auto(?::[A-Za-z0-9][A-Za-z0-9._-]{0,58})?$`)

type AutoGroupRoute struct {
	Key            string   `json:"key"`
	Name           string   `json:"name,omitempty"`
	Enabled        bool     `json:"enabled"`
	UserSelectable bool     `json:"user_selectable"`
	Groups         []string `json:"groups"`
}

func (route *AutoGroupRoute) UnmarshalJSON(data []byte) error {
	var raw struct {
		Key            string   `json:"key"`
		Name           string   `json:"name,omitempty"`
		Enabled        *bool    `json:"enabled"`
		UserSelectable *bool    `json:"user_selectable"`
		Groups         []string `json:"groups"`
	}
	if err := common.Unmarshal(data, &raw); err != nil {
		return err
	}
	route.Key = raw.Key
	route.Name = raw.Name
	route.Enabled = true
	if raw.Enabled != nil {
		route.Enabled = *raw.Enabled
	}
	route.UserSelectable = true
	if raw.UserSelectable != nil {
		route.UserSelectable = *raw.UserSelectable
	}
	route.Groups = raw.Groups
	return nil
}

type AutoGroupRoutesConfig struct {
	Version      int              `json:"version"`
	DefaultRoute string           `json:"default_route"`
	Routes       []AutoGroupRoute `json:"routes"`
}

var autoGroups = []string{
	"default",
}

var DefaultUseAutoGroup = false

var (
	autoGroupMu             sync.RWMutex
	autoGroupRoutesExplicit bool
	autoGroupRoutesConfig   = AutoGroupRoutesConfig{
		Version:      1,
		DefaultRoute: DefaultAutoRouteKey,
		Routes: []AutoGroupRoute{
			{
				Key:            DefaultAutoRouteKey,
				Name:           "自动",
				Enabled:        true,
				UserSelectable: true,
				Groups:         []string{"default"},
			},
		},
	}
)

func IsAutoRouteKey(group string) bool {
	group = strings.TrimSpace(group)
	return group == DefaultAutoRouteKey || strings.HasPrefix(group, AutoRoutePrefix)
}

func ContainsAutoGroup(group string) bool {
	for _, autoGroup := range GetAutoGroups() {
		if autoGroup == group {
			return true
		}
	}
	return false
}

func ContainsAutoRouteKey(routeKey string) bool {
	_, ok := GetAutoRoute(routeKey)
	return ok
}

func UpdateAutoGroupsByJsonString(jsonString string) error {
	groups, err := parseAutoGroupsJSON(jsonString)
	if err != nil {
		return err
	}
	autoGroupMu.Lock()
	defer autoGroupMu.Unlock()
	if !autoGroupRoutesExplicit {
		autoGroups = groups
		autoGroupRoutesConfig = configFromLegacyGroupsLocked(groups)
	}
	return nil
}

func AutoGroups2JsonString() string {
	autoGroupMu.RLock()
	groups := slices.Clone(autoGroups)
	autoGroupMu.RUnlock()
	jsonBytes, err := common.Marshal(groups)
	if err != nil {
		return "[]"
	}
	return string(jsonBytes)
}

func GetAutoGroups() []string {
	autoGroupMu.RLock()
	defer autoGroupMu.RUnlock()
	return slices.Clone(autoGroups)
}

func AutoGroupRoutes2JsonString() string {
	config := GetAutoGroupRoutesConfig()
	jsonBytes, err := common.Marshal(config)
	if err != nil {
		return "{}"
	}
	return string(jsonBytes)
}

func UpdateAutoGroupRoutesByJsonString(jsonString string) error {
	config, err := ParseAutoGroupRoutesConfig(jsonString)
	if err != nil {
		return err
	}
	autoGroupMu.Lock()
	defer autoGroupMu.Unlock()
	autoGroupRoutesConfig = config
	autoGroupRoutesExplicit = true
	autoGroups = defaultRouteGroupsLocked(config)
	return nil
}

func GetAutoRouteGroups(routeKey string) ([]string, bool) {
	route, ok := GetAutoRoute(routeKey)
	if !ok {
		return nil, false
	}
	return slices.Clone(route.Groups), true
}

func ParseAutoGroupRoutesConfig(jsonString string) (AutoGroupRoutesConfig, error) {
	trimmed := strings.TrimSpace(jsonString)
	if trimmed == "" {
		return AutoGroupRoutesConfig{}, errors.New("auto group routes config is empty")
	}
	if strings.HasPrefix(trimmed, "[") {
		groups, err := parseAutoGroupsJSON(trimmed)
		if err != nil {
			return AutoGroupRoutesConfig{}, err
		}
		return configFromLegacyGroups(groups), nil
	}
	var config AutoGroupRoutesConfig
	if err := common.Unmarshal([]byte(trimmed), &config); err != nil {
		return AutoGroupRoutesConfig{}, err
	}
	return normalizeAutoGroupRoutesConfig(config)
}

func GetAutoGroupRoutesConfig() AutoGroupRoutesConfig {
	autoGroupMu.RLock()
	defer autoGroupMu.RUnlock()
	return cloneAutoGroupRoutesConfig(autoGroupRoutesConfig)
}

func GetAutoRoutes() []AutoGroupRoute {
	config := GetAutoGroupRoutesConfig()
	return config.Routes
}

func GetAutoRoute(routeKey string) (AutoGroupRoute, bool) {
	routeKey = strings.TrimSpace(routeKey)
	autoGroupMu.RLock()
	defer autoGroupMu.RUnlock()
	for _, route := range autoGroupRoutesConfig.Routes {
		if route.Key == routeKey {
			return cloneAutoGroupRoute(route), true
		}
	}
	return AutoGroupRoute{}, false
}

func GetDefaultAutoRouteKey() string {
	autoGroupMu.RLock()
	defer autoGroupMu.RUnlock()
	if autoGroupRoutesConfig.DefaultRoute != "" {
		return autoGroupRoutesConfig.DefaultRoute
	}
	return DefaultAutoRouteKey
}

func parseAutoGroupsJSON(jsonString string) ([]string, error) {
	var groups []string
	if err := common.Unmarshal([]byte(jsonString), &groups); err != nil {
		return nil, err
	}
	return normalizeAutoGroupList(groups)
}

func configFromLegacyGroups(groups []string) AutoGroupRoutesConfig {
	normalized, err := normalizeAutoGroupList(groups)
	if err != nil {
		normalized = []string{"default"}
	}
	return configFromLegacyGroupsLocked(normalized)
}

func configFromLegacyGroupsLocked(groups []string) AutoGroupRoutesConfig {
	return AutoGroupRoutesConfig{
		Version:      1,
		DefaultRoute: DefaultAutoRouteKey,
		Routes: []AutoGroupRoute{
			{
				Key:            DefaultAutoRouteKey,
				Name:           "自动",
				Enabled:        true,
				UserSelectable: true,
				Groups:         slices.Clone(groups),
			},
		},
	}
}

func normalizeAutoGroupRoutesConfig(config AutoGroupRoutesConfig) (AutoGroupRoutesConfig, error) {
	if config.Version == 0 {
		config.Version = 1
	}
	if config.Version != 1 {
		return AutoGroupRoutesConfig{}, fmt.Errorf("unsupported auto group routes config version: %d", config.Version)
	}
	config.DefaultRoute = strings.TrimSpace(config.DefaultRoute)
	if config.DefaultRoute == "" {
		config.DefaultRoute = DefaultAutoRouteKey
	}
	if !autoRouteKeyPattern.MatchString(config.DefaultRoute) {
		return AutoGroupRoutesConfig{}, fmt.Errorf("invalid default auto route key: %s", config.DefaultRoute)
	}
	if len(config.Routes) == 0 {
		return AutoGroupRoutesConfig{}, errors.New("auto group routes must not be empty")
	}
	if len(config.Routes) > 32 {
		return AutoGroupRoutesConfig{}, errors.New("auto group routes must not exceed 32 entries")
	}

	seen := make(map[string]struct{}, len(config.Routes))
	hasDefault := false
	defaultEnabled := false
	routes := make([]AutoGroupRoute, 0, len(config.Routes))
	for _, route := range config.Routes {
		normalized, err := normalizeAutoGroupRoute(route)
		if err != nil {
			return AutoGroupRoutesConfig{}, err
		}
		if _, ok := seen[normalized.Key]; ok {
			return AutoGroupRoutesConfig{}, fmt.Errorf("duplicate auto route key: %s", normalized.Key)
		}
		seen[normalized.Key] = struct{}{}
		if normalized.Key == config.DefaultRoute {
			hasDefault = true
			defaultEnabled = normalized.Enabled
		}
		routes = append(routes, normalized)
	}
	if !hasDefault {
		return AutoGroupRoutesConfig{}, fmt.Errorf("default auto route %s is not defined", config.DefaultRoute)
	}
	if !defaultEnabled {
		return AutoGroupRoutesConfig{}, fmt.Errorf("default auto route %s must be enabled", config.DefaultRoute)
	}
	config.Routes = routes
	return config, nil
}

func normalizeAutoGroupRoute(route AutoGroupRoute) (AutoGroupRoute, error) {
	route.Key = strings.TrimSpace(route.Key)
	if !autoRouteKeyPattern.MatchString(route.Key) {
		return AutoGroupRoute{}, fmt.Errorf("invalid auto route key: %s", route.Key)
	}
	route.Name = strings.TrimSpace(route.Name)
	if route.Name == "" {
		route.Name = route.Key
	}
	if len([]rune(route.Name)) > 64 {
		return AutoGroupRoute{}, fmt.Errorf("auto route %s name must not exceed 64 characters", route.Key)
	}
	groups, err := normalizeAutoGroupList(route.Groups)
	if err != nil {
		return AutoGroupRoute{}, fmt.Errorf("auto route %s: %w", route.Key, err)
	}
	route.Groups = groups
	return route, nil
}

func normalizeAutoGroupList(groups []string) ([]string, error) {
	if len(groups) == 0 {
		return nil, errors.New("group list must not be empty")
	}
	if len(groups) > 64 {
		return nil, errors.New("group list must not exceed 64 entries")
	}
	seen := make(map[string]struct{}, len(groups))
	normalized := make([]string, 0, len(groups))
	for _, group := range groups {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		if IsAutoRouteKey(group) {
			return nil, fmt.Errorf("group list must contain real groups only: %s", group)
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		normalized = append(normalized, group)
	}
	if len(normalized) == 0 {
		return nil, errors.New("group list must contain at least one real group")
	}
	return normalized, nil
}

func defaultRouteGroupsLocked(config AutoGroupRoutesConfig) []string {
	for _, route := range config.Routes {
		if route.Key == config.DefaultRoute {
			return slices.Clone(route.Groups)
		}
	}
	return []string{}
}

func cloneAutoGroupRoutesConfig(config AutoGroupRoutesConfig) AutoGroupRoutesConfig {
	config.Routes = slices.Clone(config.Routes)
	for i := range config.Routes {
		config.Routes[i] = cloneAutoGroupRoute(config.Routes[i])
	}
	return config
}

func cloneAutoGroupRoute(route AutoGroupRoute) AutoGroupRoute {
	route.Groups = slices.Clone(route.Groups)
	return route
}
