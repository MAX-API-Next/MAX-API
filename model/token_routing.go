package model

import (
	"bytes"
	"errors"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
)

const (
	TokenRoutingPolicyVersion = 1

	TokenRoutingModeSmart  = "smart"
	TokenRoutingModeManual = "manual"
)

type TokenRoutingPolicy struct {
	Version        int      `json:"version"`
	Mode           string   `json:"mode"`
	Route          string   `json:"route,omitempty"`
	Groups         []string `json:"groups,omitempty"`
	RetryOnFailure bool     `json:"retry_on_failure"`
}

func (policy TokenRoutingPolicy) Clone() TokenRoutingPolicy {
	policy.Groups = append([]string(nil), policy.Groups...)
	return policy
}

func (token *Token) GetStoredRoutingPolicy() (*TokenRoutingPolicy, error) {
	if token == nil || token.RoutingPolicyJSON == nil || strings.TrimSpace(*token.RoutingPolicyJSON) == "" {
		return nil, nil
	}

	var policy TokenRoutingPolicy
	if err := common.Unmarshal([]byte(*token.RoutingPolicyJSON), &policy); err != nil {
		return nil, err
	}
	return &policy, nil
}

func (token *Token) SetRoutingPolicy(policy *TokenRoutingPolicy) error {
	if token == nil {
		return errors.New("token is nil")
	}
	if policy == nil {
		token.RoutingPolicyJSON = nil
		return nil
	}

	payload, err := common.Marshal(policy)
	if err != nil {
		return err
	}
	payload = bytes.TrimSpace(payload)
	encoded := string(payload)
	token.RoutingPolicyJSON = &encoded
	return nil
}
