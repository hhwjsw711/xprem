// Copyright (c) 2026 Axel Marciano (Mercure Technologies). All rights reserved.
// This file is governed by the Mercure Technologies Enterprise Edition License
// (see ee/LICENSE); it is NOT covered by the MIT license of this repository.

package apikeyrestrictions

import (
	"context"
	"fmt"
	"xprem/internal/namepattern"
	"xprem/internal/services"
	"xprem/internal/validation"
)

const maxEnvironmentRules = 50

// EnvironmentRequest describes reading the variables of one resolved environment.
type EnvironmentRequest struct {
	APIKeyContext
	Environment string
}

// EnvironmentRule lets a key read the variables of every environment matching Pattern, where "*"
// stands for any run of characters. An API key holding no rules reads every environment.
type EnvironmentRule struct {
	Pattern string `json:"pattern"`
}

// AllowsEnvironment imposes no restriction on an empty rule list.
func AllowsEnvironment(rules []EnvironmentRule, environment string) bool {
	if environment == "" {
		return false
	}
	if len(rules) == 0 {
		return true
	}
	for _, rule := range rules {
		if namepattern.Match(rule.Pattern, environment) {
			return true
		}
	}
	return false
}

// NormalizeEnvironmentRules validates the given rules and returns the form to persist.
func NormalizeEnvironmentRules(rules []EnvironmentRule) ([]EnvironmentRule, error) {
	if len(rules) > maxEnvironmentRules {
		return nil, validation.Errorf("environments.rules", "a key cannot hold more than %d environment rules", maxEnvironmentRules)
	}
	normalized := make([]EnvironmentRule, 0, len(rules))
	seen := make(map[string]struct{}, len(rules))
	for _, rule := range rules {
		if err := validation.NamePattern("environments.pattern", rule.Pattern); err != nil {
			return nil, err
		}
		pattern := namepattern.CollapseWildcards(rule.Pattern)
		if _, duplicate := seen[pattern]; duplicate {
			return nil, validation.Errorf("environments.pattern", "%q appears in more than one rule", pattern)
		}
		seen[pattern] = struct{}{}
		normalized = append(normalized, EnvironmentRule{Pattern: pattern})
	}
	return normalized, nil
}

// describeEnvironmentRules renders a rule list for the audit trail.
func describeEnvironmentRules(rules []EnvironmentRule) []string {
	described := make([]string, 0, len(rules))
	for _, rule := range rules {
		described = append(described, rule.Pattern)
	}
	return described
}

// AuthorizeEnvironment checks the authenticated key and IP, then only Environment grants.
// An empty rule list imposes no restrictions in this domain.
// It is a no-op without an active Enterprise license or control plane.
func (s *ApiKeyAccessService) AuthorizeEnvironment(ctx context.Context, req EnvironmentRequest) error {
	access, err := s.authorizationAccess(ctx, req.APIKeyContext)
	if err != nil || access == nil {
		return err
	}
	if !AllowsEnvironment(access.EnvironmentRules, req.Environment) {
		return fmt.Errorf("%w: this API key is not allowed to read the variables of environment %q", services.ErrCliAccessDenied, req.Environment)
	}
	return nil
}
