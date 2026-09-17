// Copyright (c) 2026 Axel Marciano (Mercure Technologies). All rights reserved.
// This file is governed by the Mercure Technologies Enterprise Edition License
// (see ee/LICENSE); it is NOT covered by the MIT license of this repository.

package apikeyrestrictions

import (
	"context"
	"testing"
	"xprem/internal/services"
	"xprem/internal/validation"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvironmentRulesMatchNamesAndWildcards(t *testing.T) {
	rules := []EnvironmentRule{{Pattern: "staging"}, {Pattern: "preview-*"}}
	for environment, allowed := range map[string]bool{
		"staging":      true,
		"preview-42":   true,
		"preview-":     true,
		"production":   false,
		"staging-eu":   false,
		"my-preview-1": false,
		"":             false,
	} {
		assert.Equal(t, allowed, AllowsEnvironment(rules, environment), environment)
	}
	assert.True(t, AllowsEnvironment(nil, "production"), "a key without rules reads every environment")
	assert.False(t, AllowsEnvironment(nil, ""))
}

func TestAuthorizeEnvironment(t *testing.T) {
	ctx := context.Background()
	key := APIKeyContext{AppID: "app", APIKeyID: 42}
	restricted := &fakeAccessRepo{access: map[int64]ApiKeyAccess{42: {ApiKeyID: 42, EnvironmentRules: []EnvironmentRule{{Pattern: "staging*"}}}}}

	require.NoError(t, serviceWith(restricted, true).AuthorizeEnvironment(ctx, EnvironmentRequest{APIKeyContext: key, Environment: "staging-eu"}))
	err := serviceWith(restricted, true).AuthorizeEnvironment(ctx, EnvironmentRequest{APIKeyContext: key, Environment: "production"})
	require.ErrorIs(t, err, services.ErrCliAccessDenied)
	assert.Contains(t, err.Error(), `environment "production"`)

	unrestricted := &fakeAccessRepo{access: map[int64]ApiKeyAccess{42: {ApiKeyID: 42}}}
	require.NoError(t, serviceWith(unrestricted, true).AuthorizeEnvironment(ctx, EnvironmentRequest{APIKeyContext: key, Environment: "production"}))
	require.NoError(t, serviceWith(restricted, false).AuthorizeEnvironment(ctx, EnvironmentRequest{APIKeyContext: key, Environment: "production"}), "rules are not enforced without a license")
}

func TestSetAccessNormalizesEnvironmentRules(t *testing.T) {
	repo := &fakeAccessRepo{}
	require.NoError(t, serviceWith(repo, true).SetAccess(context.Background(), "app", 42, nil, nil, nil, nil,
		[]EnvironmentRule{{Pattern: "preview-**"}, {Pattern: "staging"}}))
	assert.Equal(t, []EnvironmentRule{{Pattern: "preview-*"}, {Pattern: "staging"}}, repo.setAccess.EnvironmentRules)

	for name, rules := range map[string][]EnvironmentRule{
		"empty pattern":  {{Pattern: ""}},
		"path separator": {{Pattern: "a/b"}},
		"duplicate":      {{Pattern: "pr-*"}, {Pattern: "pr-**"}},
		"too many":       make([]EnvironmentRule, maxEnvironmentRules+1),
	} {
		rejected := &fakeAccessRepo{}
		err := serviceWith(rejected, true).SetAccess(context.Background(), "app", 42, nil, nil, nil, nil, rules)
		assert.True(t, validation.IsValidationError(err), name)
		assert.Zero(t, rejected.setCalls, name)
	}
}
