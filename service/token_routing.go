package service

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/setting"
	"github.com/MAX-API-Next/MAX-API/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

const MaxTokenManualGroups = 8

type TokenRoutePlan struct {
	PolicyVersion  int
	Legacy         bool
	Mode           string
	Route          string
	OrderedGroups  []string
	RetryOnFailure bool
}

type tokenRoutingValidationOptions struct {
	requireSelectableRoute bool
	skipUnavailableGroups  bool
}

func LegacyTokenRoutingPolicy(group string, crossGroupRetry bool, userGroup string) model.TokenRoutingPolicy {
	group = strings.TrimSpace(group)
	if group == "" {
		group = strings.TrimSpace(userGroup)
	}
	if setting.IsAutoRouteKey(group) {
		return model.TokenRoutingPolicy{
			Version:        model.TokenRoutingPolicyVersion,
			Mode:           model.TokenRoutingModeSmart,
			Route:          group,
			RetryOnFailure: crossGroupRetry,
		}
	}
	if group == "" {
		policy := DefaultTokenRoutingPolicy()
		policy.RetryOnFailure = crossGroupRetry
		return policy
	}
	return model.TokenRoutingPolicy{
		Version:        model.TokenRoutingPolicyVersion,
		Mode:           model.TokenRoutingModeManual,
		Groups:         []string{group},
		RetryOnFailure: crossGroupRetry,
	}
}

func DefaultTokenRoutingPolicy() model.TokenRoutingPolicy {
	return model.TokenRoutingPolicy{
		Version:        model.TokenRoutingPolicyVersion,
		Mode:           model.TokenRoutingModeSmart,
		Route:          setting.GetDefaultAutoRouteKey(),
		RetryOnFailure: true,
	}
}

func ResolveTokenRoutingPolicy(token *model.Token, userGroup string) (model.TokenRoutingPolicy, bool, error) {
	if token == nil {
		return model.TokenRoutingPolicy{}, false, errors.New("token is nil")
	}
	stored, err := token.GetStoredRoutingPolicy()
	if err != nil {
		return model.TokenRoutingPolicy{}, false, fmt.Errorf("invalid token routing policy: %w", err)
	}
	if stored == nil {
		policy := LegacyTokenRoutingPolicy(token.Group, token.CrossGroupRetry, userGroup)
		normalized, err := normalizeTokenRoutingPolicyForRuntime(policy, userGroup)
		return normalized, true, err
	}
	normalized, err := normalizeTokenRoutingPolicyForRuntime(*stored, userGroup)
	return normalized, false, err
}

func NormalizeTokenRoutingPolicy(policy model.TokenRoutingPolicy, userGroup string) (model.TokenRoutingPolicy, error) {
	return normalizeTokenRoutingPolicy(policy, userGroup, tokenRoutingValidationOptions{
		requireSelectableRoute: true,
	})
}

func NormalizeLegacyTokenRoutingPolicy(policy model.TokenRoutingPolicy, userGroup string) (model.TokenRoutingPolicy, error) {
	return normalizeTokenRoutingPolicy(policy, userGroup, tokenRoutingValidationOptions{})
}

func normalizeTokenRoutingPolicyForRuntime(policy model.TokenRoutingPolicy, userGroup string) (model.TokenRoutingPolicy, error) {
	return normalizeTokenRoutingPolicy(policy, userGroup, tokenRoutingValidationOptions{
		skipUnavailableGroups: true,
	})
}

func normalizeTokenRoutingPolicy(policy model.TokenRoutingPolicy, userGroup string, options tokenRoutingValidationOptions) (model.TokenRoutingPolicy, error) {
	if policy.Version == 0 {
		policy.Version = model.TokenRoutingPolicyVersion
	}
	if policy.Version != model.TokenRoutingPolicyVersion {
		return model.TokenRoutingPolicy{}, fmt.Errorf("unsupported token routing policy version: %d", policy.Version)
	}

	policy.Mode = strings.TrimSpace(policy.Mode)
	policy.Route = strings.TrimSpace(policy.Route)

	switch policy.Mode {
	case model.TokenRoutingModeSmart:
		if len(policy.Groups) != 0 {
			return model.TokenRoutingPolicy{}, errors.New("smart routing must not define manual groups")
		}
		if policy.Route == "" {
			policy.Route = setting.GetDefaultAutoRouteKey()
		}
		if !setting.IsAutoRouteKey(policy.Route) {
			return model.TokenRoutingPolicy{}, fmt.Errorf("invalid smart route: %s", policy.Route)
		}
		if _, ok := GetUserAutoRoute(userGroup, policy.Route, options.requireSelectableRoute); !ok {
			return model.TokenRoutingPolicy{}, fmt.Errorf("auto route %s is not available", policy.Route)
		}
		policy.Groups = nil
	case model.TokenRoutingModeManual:
		if policy.Route != "" {
			return model.TokenRoutingPolicy{}, errors.New("manual routing must not define a smart route")
		}
		if len(policy.Groups) == 0 {
			return model.TokenRoutingPolicy{}, errors.New("manual routing requires at least one group")
		}
		if len(policy.Groups) > MaxTokenManualGroups {
			return model.TokenRoutingPolicy{}, fmt.Errorf("manual routing must not exceed %d groups", MaxTokenManualGroups)
		}
		seen := make(map[string]struct{}, len(policy.Groups))
		groups := make([]string, 0, len(policy.Groups))
		for _, rawGroup := range policy.Groups {
			group := strings.TrimSpace(rawGroup)
			if group == "" {
				return model.TokenRoutingPolicy{}, errors.New("manual routing groups must not be empty")
			}
			if setting.IsAutoRouteKey(group) {
				return model.TokenRoutingPolicy{}, fmt.Errorf("manual routing requires real groups only: %s", group)
			}
			if _, exists := seen[group]; exists {
				return model.TokenRoutingPolicy{}, fmt.Errorf("duplicate manual routing group: %s", group)
			}
			seen[group] = struct{}{}
			if !CanUseTokenGroupRuntime(userGroup, group) || !ratio_setting.ContainsGroupRatio(group) {
				if options.skipUnavailableGroups {
					continue
				}
				return model.TokenRoutingPolicy{}, fmt.Errorf("group %s is not available", group)
			}
			groups = append(groups, group)
		}
		if len(groups) == 0 {
			return model.TokenRoutingPolicy{}, errors.New("manual routing has no available groups")
		}
		policy.Groups = groups
	default:
		return model.TokenRoutingPolicy{}, fmt.Errorf("unsupported token routing mode: %s", policy.Mode)
	}

	return policy, nil
}

func ProjectTokenRoutingPolicy(policy model.TokenRoutingPolicy) (group string, crossGroupRetry bool) {
	if policy.Mode == model.TokenRoutingModeSmart {
		return policy.Route, policy.RetryOnFailure
	}
	if len(policy.Groups) > 0 {
		return policy.Groups[0], policy.RetryOnFailure
	}
	return "", policy.RetryOnFailure
}

func BuildTokenRoutePlan(policy model.TokenRoutingPolicy, userGroup string) (TokenRoutePlan, error) {
	policy, err := normalizeTokenRoutingPolicyForRuntime(policy, userGroup)
	if err != nil {
		return TokenRoutePlan{}, err
	}

	plan := TokenRoutePlan{
		PolicyVersion:  policy.Version,
		Mode:           policy.Mode,
		Route:          policy.Route,
		RetryOnFailure: policy.RetryOnFailure,
	}
	if policy.Mode == model.TokenRoutingModeManual {
		plan.OrderedGroups = slices.Clone(policy.Groups)
		return plan, nil
	}

	groups := GetUserAutoGroupByRoute(userGroup, policy.Route)
	if len(groups) == 0 {
		return TokenRoutePlan{}, fmt.Errorf("auto route %s has no available groups", policy.Route)
	}
	plan.OrderedGroups = slices.Clone(groups)
	return plan, nil
}

func BuildContextTokenRoutePlan(c *gin.Context) (*TokenRoutePlan, error) {
	if c == nil {
		return nil, errors.New("request context is nil")
	}
	if existing, ok := GetTokenRoutePlan(c); ok {
		return existing, nil
	}

	userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	var policy model.TokenRoutingPolicy
	hasStoredPolicy := false
	if raw, ok := common.GetContextKey(c, constant.ContextKeyTokenRoutingPolicy); ok {
		switch value := raw.(type) {
		case model.TokenRoutingPolicy:
			policy = value.Clone()
			hasStoredPolicy = true
		case *model.TokenRoutingPolicy:
			if value != nil {
				policy = value.Clone()
				hasStoredPolicy = true
			}
		}
	}
	if policy.Version == 0 {
		policy = LegacyTokenRoutingPolicy(
			common.GetContextKeyString(c, constant.ContextKeyTokenGroup),
			common.GetContextKeyBool(c, constant.ContextKeyTokenCrossGroupRetry),
			userGroup,
		)
	}

	plan, err := BuildTokenRoutePlan(policy, userGroup)
	if err != nil {
		return nil, err
	}
	plan.Legacy = !hasStoredPolicy
	common.SetContextKey(c, constant.ContextKeyTokenRoutePlan, &plan)
	return &plan, nil
}

func GetTokenRoutePlan(c *gin.Context) (*TokenRoutePlan, bool) {
	if c == nil {
		return nil, false
	}
	raw, ok := common.GetContextKey(c, constant.ContextKeyTokenRoutePlan)
	if !ok {
		return nil, false
	}
	plan, ok := raw.(*TokenRoutePlan)
	return plan, ok && plan != nil
}

func GetContextTokenRoutingPolicy(c *gin.Context) (model.TokenRoutingPolicy, bool) {
	if c == nil {
		return model.TokenRoutingPolicy{}, false
	}
	raw, ok := common.GetContextKey(c, constant.ContextKeyTokenRoutingPolicy)
	if !ok {
		return model.TokenRoutingPolicy{}, false
	}
	switch value := raw.(type) {
	case model.TokenRoutingPolicy:
		return value.Clone(), true
	case *model.TokenRoutingPolicy:
		if value != nil {
			return value.Clone(), true
		}
	}
	return model.TokenRoutingPolicy{}, false
}

func GetContextTokenRouteGroups(c *gin.Context, userGroup string, tokenGroup string) []string {
	if plan, ok := GetTokenRoutePlan(c); ok && len(plan.OrderedGroups) > 0 {
		return slices.Clone(plan.OrderedGroups)
	}
	if policy, ok := GetContextTokenRoutingPolicy(c); ok {
		if policy.Mode == model.TokenRoutingModeManual {
			return slices.Clone(policy.Groups)
		}
		if policy.Mode == model.TokenRoutingModeSmart {
			return GetUserAutoGroupByRoute(userGroup, policy.Route)
		}
	}
	if setting.IsAutoRouteKey(tokenGroup) {
		return GetUserAutoGroupByRoute(userGroup, tokenGroup)
	}
	if tokenGroup != "" {
		return []string{tokenGroup}
	}
	if userGroup != "" {
		return []string{userGroup}
	}
	return nil
}
