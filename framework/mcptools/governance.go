package mcptools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/maximhq/bifrost/core/schemas"
	"github.com/maximhq/bifrost/framework/configstore"
	"github.com/maximhq/bifrost/framework/configstore/tables"
)

// describeVirtualKeyTool looks up one virtual key's budget, rate limit and
// allowed providers - the natural follow-up to "how much has this key spent"
// (query_usage_by) that has no way to answer before: whether there is room
// left, not just how much has gone by.
//
// It never returns tables.TableVirtualKey (or any of its relations) directly.
// That struct carries the key's own secret value and its rotation history -
// exactly the kind of key material a tool must never surface - so
// describeVirtualKey below hand-picks only the budget/limit/provider fields
// onto a fresh map instead of ever serializing the row itself.
func describeVirtualKeyTool() Tool {
	return Tool{
		name: "describe_virtual_key",
		description: "Look up one virtual key's budget, rate limit and allowed providers/models - its configured room, not its traffic. " +
			"Use query_usage_by with dimension virtual_key for what it has actually spent; use this for what it is allowed to spend or call before it is throttled. " +
			"Needs the key's id, as returned by describe_filter_space's virtual_keys list - a name is not enough.",
		schemaJSON: `{
  "type": "object",
  "properties": {
    "virtual_key_id": {"type": "string", "description": "The virtual key's id, from describe_filter_space."}
  },
  "required": ["virtual_key_id"]
}`,
		execute: func(ctx context.Context, deps *Deps, args map[string]any) (any, error) {
			if deps.Governance == nil {
				return nil, fmt.Errorf("virtual key detail is not available on this deployment")
			}
			id, _ := args["virtual_key_id"].(string)
			id = strings.TrimSpace(id)
			if id == "" {
				return nil, fmt.Errorf("virtual_key_id is required")
			}
			vk, err := deps.Governance.GetVirtualKey(ctx, id)
			if err != nil {
				if errors.Is(err, configstore.ErrNotFound) {
					return nil, fmt.Errorf("no virtual key with id %q - describe_filter_space lists the real ones", id)
				}
				return nil, fmt.Errorf("virtual key lookup failed: %w", err)
			}
			return describeVirtualKey(vk), nil
		},
	}
}

// describeVirtualKey projects the safe subset of a virtual key row. Every
// field it reads is picked by name - there is no struct marshal of vk or any
// of its relations anywhere in this function, which is what keeps Value,
// PreviousValue, ValueHash and EncryptionStatus (and every provider key
// beneath ProviderConfigs) out of a tool result by construction rather than by
// remembering to strip them.
func describeVirtualKey(vk *tables.TableVirtualKey) map[string]any {
	out := map[string]any{
		"id":                  vk.ID,
		"name":                vk.Name,
		"is_active":           vk.IsActiveValue(),
		"allow_all_providers": vk.AllowAllProviders,
	}
	if vk.Description != "" {
		out["description"] = vk.Description
	}
	if vk.ExpiresAt != nil {
		out["expires_at"] = *vk.ExpiresAt
	}
	if vk.TeamID != nil {
		out["team_id"] = *vk.TeamID
	}
	if vk.CustomerID != nil {
		out["customer_id"] = *vk.CustomerID
	}
	if len(vk.Budgets) > 0 {
		budgets := make([]map[string]any, len(vk.Budgets))
		for i, budget := range vk.Budgets {
			budgets[i] = budgetSummary(budget)
		}
		out["budgets"] = budgets
	}
	if vk.RateLimit != nil {
		out["rate_limit"] = rateLimitSummary(*vk.RateLimit)
	}
	if len(vk.ProviderConfigs) > 0 {
		providers := make([]map[string]any, len(vk.ProviderConfigs))
		for i, config := range vk.ProviderConfigs {
			providers[i] = providerConfigSummary(config)
		}
		out["providers"] = providers
	}
	return out
}

// budgetSummary reports a budget the way an operator reads it: what it is
// capped at right now (EffectiveMaxLimit, which folds in an active override
// rather than the raw MaxLimit an override has already changed), what has
// been spent against that cap, and when it next resets.
func budgetSummary(budget tables.TableBudget) map[string]any {
	out := map[string]any{
		"id":             budget.ID,
		"max_limit":      budget.EffectiveMaxLimit(),
		"current_usage":  budget.CurrentUsage,
		"reset_duration": budget.ResetDuration,
		"last_reset":     budget.LastReset,
	}
	if budget.HasActiveOverride() {
		out["override_active"] = true
	}
	return out
}

// rateLimitSummary reports only the limit family (token, request) that is
// actually configured - a limit whose MaxLimit is nil is unset, not zero, and
// including it anyway would read as a rate limit of zero requests allowed.
func rateLimitSummary(limit tables.TableRateLimit) map[string]any {
	out := map[string]any{}
	if limit.TokenMaxLimit != nil {
		out["token_max_limit"] = *limit.TokenMaxLimit
		out["token_current_usage"] = limit.TokenCurrentUsage
		if limit.TokenResetDuration != nil {
			out["token_reset_duration"] = *limit.TokenResetDuration
		}
	}
	if limit.RequestMaxLimit != nil {
		out["request_max_limit"] = *limit.RequestMaxLimit
		out["request_current_usage"] = limit.RequestCurrentUsage
		if limit.RequestResetDuration != nil {
			out["request_reset_duration"] = *limit.RequestResetDuration
		}
	}
	return out
}

// providerConfigSummary reports which models a provider is scoped to under
// this key. Keys is deliberately never touched: even the narrower preload
// used elsewhere (id, name, key_id, provider) is more than a chat tool needs
// to say, and the actual credential is never in reach of this struct at all.
func providerConfigSummary(config tables.TableVirtualKeyProviderConfig) map[string]any {
	out := map[string]any{"provider": config.Provider}
	if len(config.AllowedModels) > 0 {
		out["allowed_models"] = []string(config.AllowedModels)
	}
	if len(config.BlacklistedModels) > 0 {
		out["blacklisted_models"] = []string(config.BlacklistedModels)
	}
	if len(config.Budgets) > 0 {
		budgets := make([]map[string]any, len(config.Budgets))
		for i, budget := range config.Budgets {
			budgets[i] = budgetSummary(budget)
		}
		out["budgets"] = budgets
	}
	if config.RateLimit != nil {
		out["rate_limit"] = rateLimitSummary(*config.RateLimit)
	}
	return out
}

func listVirtualKeysTool() Tool {
	return Tool{
		name: "list_virtual_keys",
		description: "List configured virtual keys (id, name, active, team/customer). This is the configured catalog, not keys seen in logs; a key with no traffic still appears. " +
			"Never returns the secret value. Use describe_virtual_key for budgets and allowed providers.",
		schemaJSON: `{
  "type": "object",
  "properties": {
    "search": {"type": "string", "description": "Optional substring matched against the key name."},
    "team_id": {"type": "string"},
    "customer_id": {"type": "string"},
    "limit": {"type": "integer", "minimum": 1, "maximum": 20}
  }
}`,
		execute: func(ctx context.Context, deps *Deps, args map[string]any) (any, error) {
			if err := requireGovernance(deps); err != nil {
				return nil, err
			}
			search, err := stringArg(args, "search", false)
			if err != nil {
				return nil, err
			}
			teamID, err := stringArg(args, "team_id", false)
			if err != nil {
				return nil, err
			}
			customerID, err := stringArg(args, "customer_id", false)
			if err != nil {
				return nil, err
			}
			limit, err := intArg(args, "limit", 10, MaxGovernanceRows)
			if err != nil {
				return nil, err
			}
			keys, total, err := deps.Governance.GetVirtualKeysPaginated(ctx, configstore.VirtualKeyQueryParams{
				Limit:      limit,
				Search:     search,
				TeamID:     teamID,
				CustomerID: customerID,
			})
			if err != nil {
				return nil, fmt.Errorf("could not list virtual keys: %w", err)
			}
			rows := make([]map[string]any, 0, len(keys))
			for i := range keys {
				rows = append(rows, virtualKeyListRow(&keys[i]))
			}
			return map[string]any{"virtual_keys": rows, "returned": len(rows), "total_matching": total}, nil
		},
	}
}

func virtualKeyListRow(vk *tables.TableVirtualKey) map[string]any {
	row := map[string]any{
		"id":                  vk.ID,
		"name":                vk.Name,
		"is_active":           vk.IsActiveValue(),
		"allow_all_providers": vk.AllowAllProviders,
	}
	if vk.TeamID != nil {
		row["team_id"] = *vk.TeamID
	}
	if vk.CustomerID != nil {
		row["customer_id"] = *vk.CustomerID
	}
	return row
}

func listTeamsTool() Tool {
	return Tool{
		name:        "list_teams",
		description: "List configured teams (id, name, customer, virtual-key count). Use describe_team for budgets.",
		schemaJSON: `{
  "type": "object",
  "properties": {
    "search": {"type": "string"},
    "customer_id": {"type": "string"},
    "limit": {"type": "integer", "minimum": 1, "maximum": 20}
  }
}`,
		execute: func(ctx context.Context, deps *Deps, args map[string]any) (any, error) {
			if err := requireGovernance(deps); err != nil {
				return nil, err
			}
			search, err := stringArg(args, "search", false)
			if err != nil {
				return nil, err
			}
			customerID, err := stringArg(args, "customer_id", false)
			if err != nil {
				return nil, err
			}
			limit, err := intArg(args, "limit", 10, MaxGovernanceRows)
			if err != nil {
				return nil, err
			}
			teams, total, err := deps.Governance.GetTeamsPaginated(ctx, configstore.TeamsQueryParams{
				Limit: limit, Search: search, CustomerID: customerID,
			})
			if err != nil {
				return nil, fmt.Errorf("could not list teams: %w", err)
			}
			rows := make([]map[string]any, 0, len(teams))
			for i := range teams {
				t := teams[i]
				row := map[string]any{"id": t.ID, "name": t.Name, "virtual_key_count": t.VirtualKeyCount}
				if t.CustomerID != nil {
					row["customer_id"] = *t.CustomerID
				}
				rows = append(rows, row)
			}
			return map[string]any{"teams": rows, "returned": len(rows), "total_matching": total}, nil
		},
	}
}

func describeTeamTool() Tool {
	return Tool{
		name:        "describe_team",
		description: "Look up one team by id: name, customer, budgets, rate limit, virtual-key count.",
		schemaJSON: `{
  "type": "object",
  "properties": {
    "team_id": {"type": "string"}
  },
  "required": ["team_id"]
}`,
		execute: func(ctx context.Context, deps *Deps, args map[string]any) (any, error) {
			if err := requireGovernance(deps); err != nil {
				return nil, err
			}
			id, err := stringArg(args, "team_id", true)
			if err != nil {
				return nil, err
			}
			team, err := deps.Governance.GetTeam(ctx, id)
			if err != nil {
				if errors.Is(err, configstore.ErrNotFound) {
					return nil, fmt.Errorf("no team with id %q", id)
				}
				return nil, fmt.Errorf("team lookup failed: %w", err)
			}
			return describeTeam(team), nil
		},
	}
}

func describeTeam(team *tables.TableTeam) map[string]any {
	out := map[string]any{
		"id":                team.ID,
		"name":              team.Name,
		"virtual_key_count": team.VirtualKeyCount,
		"calendar_aligned":  team.CalendarAligned,
	}
	if team.CustomerID != nil {
		out["customer_id"] = *team.CustomerID
	}
	if len(team.Budgets) > 0 {
		budgets := make([]map[string]any, len(team.Budgets))
		for i, b := range team.Budgets {
			budgets[i] = budgetSummary(b)
		}
		out["budgets"] = budgets
	}
	if team.RateLimit != nil {
		out["rate_limit"] = rateLimitSummary(*team.RateLimit)
	}
	return out
}

func listCustomersTool() Tool {
	return Tool{
		name:        "list_customers",
		description: "List configured customers (id, name, virtual-key count). Use describe_customer for budgets.",
		schemaJSON: `{
  "type": "object",
  "properties": {
    "search": {"type": "string"},
    "limit": {"type": "integer", "minimum": 1, "maximum": 20}
  }
}`,
		execute: func(ctx context.Context, deps *Deps, args map[string]any) (any, error) {
			if err := requireGovernance(deps); err != nil {
				return nil, err
			}
			search, err := stringArg(args, "search", false)
			if err != nil {
				return nil, err
			}
			limit, err := intArg(args, "limit", 10, MaxGovernanceRows)
			if err != nil {
				return nil, err
			}
			customers, total, err := deps.Governance.GetCustomersPaginated(ctx, configstore.CustomersQueryParams{
				Limit: limit, Search: search,
			})
			if err != nil {
				return nil, fmt.Errorf("could not list customers: %w", err)
			}
			rows := make([]map[string]any, 0, len(customers))
			for i := range customers {
				c := customers[i]
				rows = append(rows, map[string]any{
					"id":                c.ID,
					"name":              c.Name,
					"virtual_key_count": c.VirtualKeyCount,
				})
			}
			return map[string]any{"customers": rows, "returned": len(rows), "total_matching": total}, nil
		},
	}
}

func describeCustomerTool() Tool {
	return Tool{
		name:        "describe_customer",
		description: "Look up one customer by id: name, budgets, rate limit, virtual-key count.",
		schemaJSON: `{
  "type": "object",
  "properties": {
    "customer_id": {"type": "string"}
  },
  "required": ["customer_id"]
}`,
		execute: func(ctx context.Context, deps *Deps, args map[string]any) (any, error) {
			if err := requireGovernance(deps); err != nil {
				return nil, err
			}
			id, err := stringArg(args, "customer_id", true)
			if err != nil {
				return nil, err
			}
			customer, err := deps.Governance.GetCustomer(ctx, id)
			if err != nil {
				if errors.Is(err, configstore.ErrNotFound) {
					return nil, fmt.Errorf("no customer with id %q", id)
				}
				return nil, fmt.Errorf("customer lookup failed: %w", err)
			}
			return describeCustomer(customer), nil
		},
	}
}

func describeCustomer(customer *tables.TableCustomer) map[string]any {
	out := map[string]any{
		"id":                customer.ID,
		"name":              customer.Name,
		"virtual_key_count": customer.VirtualKeyCount,
		"calendar_aligned":  customer.CalendarAligned,
	}
	if len(customer.Budgets) > 0 {
		budgets := make([]map[string]any, len(customer.Budgets))
		for i, b := range customer.Budgets {
			budgets[i] = budgetSummary(b)
		}
		out["budgets"] = budgets
	}
	if customer.RateLimit != nil {
		out["rate_limit"] = rateLimitSummary(*customer.RateLimit)
	}
	return out
}

func listBudgetsTool() Tool {
	return Tool{
		name:        "list_budgets",
		description: "List configured budgets (id, cap, current usage, reset, owner). Spend already incurred is query_usage_by; remaining room is here.",
		schemaJSON: `{
  "type": "object",
  "properties": {
    "limit": {"type": "integer", "minimum": 1, "maximum": 20}
  }
}`,
		execute: func(ctx context.Context, deps *Deps, args map[string]any) (any, error) {
			if err := requireGovernance(deps); err != nil {
				return nil, err
			}
			limit, err := intArg(args, "limit", 10, MaxGovernanceRows)
			if err != nil {
				return nil, err
			}
			budgets, err := deps.Governance.GetBudgets(ctx)
			if err != nil {
				return nil, fmt.Errorf("could not list budgets: %w", err)
			}
			if len(budgets) > limit {
				budgets = budgets[:limit]
			}
			rows := make([]map[string]any, 0, len(budgets))
			for i := range budgets {
				rows = append(rows, budgetListRow(&budgets[i]))
			}
			return map[string]any{"budgets": rows, "returned": len(rows)}, nil
		},
	}
}

func describeBudgetTool() Tool {
	return Tool{
		name:        "describe_budget",
		description: "Look up one budget by id: cap, current usage, reset window, owner.",
		schemaJSON: `{
  "type": "object",
  "properties": {
    "budget_id": {"type": "string"}
  },
  "required": ["budget_id"]
}`,
		execute: func(ctx context.Context, deps *Deps, args map[string]any) (any, error) {
			if err := requireGovernance(deps); err != nil {
				return nil, err
			}
			id, err := stringArg(args, "budget_id", true)
			if err != nil {
				return nil, err
			}
			budget, err := deps.Governance.GetBudget(ctx, id)
			if err != nil {
				if errors.Is(err, configstore.ErrNotFound) {
					return nil, fmt.Errorf("no budget with id %q", id)
				}
				return nil, fmt.Errorf("budget lookup failed: %w", err)
			}
			return budgetListRow(budget), nil
		},
	}
}

func budgetListRow(budget *tables.TableBudget) map[string]any {
	out := budgetSummary(*budget)
	if budget.VirtualKeyID != nil {
		out["virtual_key_id"] = *budget.VirtualKeyID
	}
	if budget.TeamID != nil {
		out["team_id"] = *budget.TeamID
	}
	if budget.CustomerID != nil {
		out["customer_id"] = *budget.CustomerID
	}
	return out
}

func createVirtualKeyTool() Tool {
	return Tool{
		name: "create_virtual_key",
		description: "Create a virtual key. Returns the secret value once; store it, it cannot be retrieved later. " +
			"allow_all_providers defaults to true so the key can call configured providers; set it false only if you will attach provider configs separately. " +
			"Team and customer are mutually exclusive.",
		schemaJSON: `{
  "type": "object",
  "properties": {
    "name": {"type": "string"},
    "description": {"type": "string"},
    "team_id": {"type": "string"},
    "customer_id": {"type": "string"},
    "allow_all_providers": {"type": "boolean", "description": "Defaults to true."}
  },
  "required": ["name"]
}`,
		execute: func(ctx context.Context, deps *Deps, args map[string]any) (any, error) {
			if err := requireGovernance(deps); err != nil {
				return nil, err
			}
			name, err := stringArg(args, "name", true)
			if err != nil {
				return nil, err
			}
			description, err := stringArg(args, "description", false)
			if err != nil {
				return nil, err
			}
			teamID, err := stringArg(args, "team_id", false)
			if err != nil {
				return nil, err
			}
			customerID, err := stringArg(args, "customer_id", false)
			if err != nil {
				return nil, err
			}
			if teamID != "" && customerID != "" {
				return nil, fmt.Errorf("virtual key cannot be attached to both a team and a customer")
			}
			allowAll, err := boolArg(args, "allow_all_providers")
			if err != nil {
				return nil, err
			}
			if _, present := args["allow_all_providers"]; !present {
				allowAll = true
			}
			plaintext := VirtualKeyPrefix + uuid.NewString()
			active := true
			vk := tables.TableVirtualKey{
				ID:                uuid.NewString(),
				Name:              name,
				Description:       description,
				Value:             *schemas.NewSecretVar(plaintext),
				IsActive:          &active,
				AllowAllProviders: allowAll,
			}
			if teamID != "" {
				vk.TeamID = &teamID
			}
			if customerID != "" {
				vk.CustomerID = &customerID
			}
			if err := deps.Governance.CreateVirtualKey(ctx, &vk); err != nil {
				if errors.Is(err, configstore.ErrAlreadyExists) {
					return nil, fmt.Errorf("a virtual key named %q already exists", name)
				}
				return nil, fmt.Errorf("could not create virtual key: %w", err)
			}
			reloadNote := reloadVirtualKey(ctx, deps, vk.ID)
			out := map[string]any{
				"id":                  vk.ID,
				"name":                vk.Name,
				"value":               plaintext,
				"is_active":           true,
				"allow_all_providers": allowAll,
				"note":                "This is the only time the secret value is returned. Store it; list_virtual_keys and describe_virtual_key will not show it.",
			}
			if teamID != "" {
				out["team_id"] = teamID
			}
			if customerID != "" {
				out["customer_id"] = customerID
			}
			if reloadNote != "" {
				out["reload_warning"] = reloadNote
			}
			return out, nil
		},
	}
}

func updateVirtualKeyTool() Tool {
	return Tool{
		name:        "update_virtual_key",
		description: "Update a virtual key's name, description or active flag. Does not rotate the secret.",
		schemaJSON: `{
  "type": "object",
  "properties": {
    "virtual_key_id": {"type": "string"},
    "name": {"type": "string"},
    "description": {"type": "string"},
    "is_active": {"type": "boolean"}
  },
  "required": ["virtual_key_id"]
}`,
		execute: func(ctx context.Context, deps *Deps, args map[string]any) (any, error) {
			if err := requireGovernance(deps); err != nil {
				return nil, err
			}
			id, err := stringArg(args, "virtual_key_id", true)
			if err != nil {
				return nil, err
			}
			vk, err := deps.Governance.GetVirtualKey(ctx, id)
			if err != nil {
				if errors.Is(err, configstore.ErrNotFound) {
					return nil, fmt.Errorf("no virtual key with id %q", id)
				}
				return nil, fmt.Errorf("virtual key lookup failed: %w", err)
			}
			if name, err := stringArg(args, "name", false); err != nil {
				return nil, err
			} else if name != "" {
				vk.Name = name
			}
			if _, present := args["description"]; present {
				description, err := stringArg(args, "description", false)
				if err != nil {
					return nil, err
				}
				vk.Description = description
			}
			if _, present := args["is_active"]; present {
				active, err := boolArg(args, "is_active")
				if err != nil {
					return nil, err
				}
				vk.IsActive = &active
			}
			if err := deps.Governance.UpdateVirtualKey(ctx, vk); err != nil {
				return nil, fmt.Errorf("could not update virtual key: %w", err)
			}
			out := virtualKeyListRow(vk)
			if note := reloadVirtualKey(ctx, deps, vk.ID); note != "" {
				out["reload_warning"] = note
			}
			return out, nil
		},
	}
}

func deactivateVirtualKeyTool() Tool {
	return Tool{
		name:        "deactivate_virtual_key",
		description: "Deactivate a virtual key (is_active=false). Prefer this over deleting: the row stays, traffic stops authenticating with it.",
		schemaJSON: `{
  "type": "object",
  "properties": {
    "virtual_key_id": {"type": "string"}
  },
  "required": ["virtual_key_id"]
}`,
		execute: func(ctx context.Context, deps *Deps, args map[string]any) (any, error) {
			if err := requireGovernance(deps); err != nil {
				return nil, err
			}
			id, err := stringArg(args, "virtual_key_id", true)
			if err != nil {
				return nil, err
			}
			vk, err := deps.Governance.GetVirtualKey(ctx, id)
			if err != nil {
				if errors.Is(err, configstore.ErrNotFound) {
					return nil, fmt.Errorf("no virtual key with id %q", id)
				}
				return nil, fmt.Errorf("virtual key lookup failed: %w", err)
			}
			active := false
			vk.IsActive = &active
			if err := deps.Governance.UpdateVirtualKey(ctx, vk); err != nil {
				return nil, fmt.Errorf("could not deactivate virtual key: %w", err)
			}
			out := map[string]any{"id": vk.ID, "name": vk.Name, "is_active": false}
			if note := reloadVirtualKey(ctx, deps, vk.ID); note != "" {
				out["reload_warning"] = note
			}
			return out, nil
		},
	}
}

func rotateVirtualKeyTool() Tool {
	return Tool{
		name: "rotate_virtual_key",
		description: "Replace a virtual key's secret with a new one. Returns the new value once; store it. " +
			"If a rotation cooldown is configured the previous value keeps authenticating until it expires; otherwise it stops immediately. " +
			"Does not change name, team, providers or budgets.",
		schemaJSON: `{
  "type": "object",
  "properties": {
    "virtual_key_id": {"type": "string"}
  },
  "required": ["virtual_key_id"]
}`,
		execute: func(ctx context.Context, deps *Deps, args map[string]any) (any, error) {
			if err := requireGovernance(deps); err != nil {
				return nil, err
			}
			id, err := stringArg(args, "virtual_key_id", true)
			if err != nil {
				return nil, err
			}
			vk, err := deps.Governance.GetVirtualKey(ctx, id)
			if err != nil {
				if errors.Is(err, configstore.ErrNotFound) {
					return nil, fmt.Errorf("no virtual key with id %q", id)
				}
				return nil, fmt.Errorf("virtual key lookup failed: %w", err)
			}
			oldValue := vk.Value.GetValue()
			plaintext := VirtualKeyPrefix + uuid.NewString()
			if plaintext == oldValue {
				return nil, fmt.Errorf("generated virtual key matched existing value")
			}
			vk.Value = *schemas.NewSecretVar(plaintext)
			now := Now()
			vk.RotatedAt = &now
			var expiresAt *time.Time
			if cooldown := rotationCooldown(ctx, deps); cooldown > 0 {
				vk.PreviousValue = *schemas.NewSecretVar(oldValue)
				expiry := now.Add(cooldown)
				vk.PreviousValueExpiresAt = &expiry
				expiresAt = &expiry
			} else {
				vk.ClearPreviousValue()
			}
			if err := deps.Governance.UpdateVirtualKey(ctx, vk); err != nil {
				return nil, fmt.Errorf("could not rotate virtual key: %w", err)
			}
			out := map[string]any{
				"id":    vk.ID,
				"name":  vk.Name,
				"value": plaintext,
				"note":  "This is the only time the new secret value is returned. Store it; list_virtual_keys and describe_virtual_key will not show it.",
			}
			if expiresAt != nil {
				out["previous_value_expires_at"] = *expiresAt
			}
			if note := reloadVirtualKey(ctx, deps, vk.ID); note != "" {
				out["reload_warning"] = note
			}
			return out, nil
		},
	}
}

// rotationCooldown is the grace period during which a rotated-out value still
// authenticates. Errors and a missing client config degrade to 0 (immediate
// flip), matching the HTTP rotator: a failed lookup must not leave the old
// secret valid indefinitely.
func rotationCooldown(ctx context.Context, deps *Deps) time.Duration {
	if deps == nil || deps.Governance == nil {
		return 0
	}
	cfg, err := deps.Governance.GetClientConfig(ctx)
	if err != nil || cfg == nil {
		return 0
	}
	return cfg.VKRotationCooldown.D()
}

func createTeamTool() Tool {
	return Tool{
		name:        "create_team",
		description: "Create a team. Optionally attach it to a customer.",
		schemaJSON: `{
  "type": "object",
  "properties": {
    "name": {"type": "string"},
    "customer_id": {"type": "string"}
  },
  "required": ["name"]
}`,
		execute: func(ctx context.Context, deps *Deps, args map[string]any) (any, error) {
			if err := requireGovernance(deps); err != nil {
				return nil, err
			}
			name, err := stringArg(args, "name", true)
			if err != nil {
				return nil, err
			}
			customerID, err := stringArg(args, "customer_id", false)
			if err != nil {
				return nil, err
			}
			team := tables.TableTeam{ID: uuid.NewString(), Name: name}
			if customerID != "" {
				team.CustomerID = &customerID
			}
			if err := deps.Governance.CreateTeam(ctx, &team); err != nil {
				if errors.Is(err, configstore.ErrAlreadyExists) {
					return nil, fmt.Errorf("a team named %q already exists", name)
				}
				return nil, fmt.Errorf("could not create team: %w", err)
			}
			out := map[string]any{"id": team.ID, "name": team.Name}
			if team.CustomerID != nil {
				out["customer_id"] = *team.CustomerID
			}
			if deps.Reloader != nil {
				if _, err := deps.Reloader.ReloadTeam(ctx, team.ID); err != nil {
					out["reload_warning"] = err.Error()
				}
			} else {
				out["reload_warning"] = "governance cache was not reloaded; the team is stored but may not be live until restart"
			}
			return out, nil
		},
	}
}

func createCustomerTool() Tool {
	return Tool{
		name:        "create_customer",
		description: "Create a customer.",
		schemaJSON: `{
  "type": "object",
  "properties": {
    "name": {"type": "string"}
  },
  "required": ["name"]
}`,
		execute: func(ctx context.Context, deps *Deps, args map[string]any) (any, error) {
			if err := requireGovernance(deps); err != nil {
				return nil, err
			}
			name, err := stringArg(args, "name", true)
			if err != nil {
				return nil, err
			}
			customer := tables.TableCustomer{ID: uuid.NewString(), Name: name}
			if err := deps.Governance.CreateCustomer(ctx, &customer); err != nil {
				if errors.Is(err, configstore.ErrAlreadyExists) {
					return nil, fmt.Errorf("a customer named %q already exists", name)
				}
				return nil, fmt.Errorf("could not create customer: %w", err)
			}
			out := map[string]any{"id": customer.ID, "name": customer.Name}
			if deps.Reloader != nil {
				if _, err := deps.Reloader.ReloadCustomer(ctx, customer.ID); err != nil {
					out["reload_warning"] = err.Error()
				}
			} else {
				out["reload_warning"] = "governance cache was not reloaded; the customer is stored but may not be live until restart"
			}
			return out, nil
		},
	}
}

func createBudgetTool() Tool {
	return Tool{
		name: "create_budget",
		description: "Create a budget on a team or customer (not a virtual key: VK budgets are stored as model configs and are not written here). " +
			"max_limit is dollars. reset_duration is a positive duration such as 1d, 1w, 1M.",
		schemaJSON: `{
  "type": "object",
  "properties": {
    "max_limit": {"type": "number", "description": "Cap in dollars."},
    "reset_duration": {"type": "string", "description": "e.g. 1d, 1w, 1M."},
    "team_id": {"type": "string"},
    "customer_id": {"type": "string"}
  },
  "required": ["max_limit", "reset_duration"]
}`,
		execute: func(ctx context.Context, deps *Deps, args map[string]any) (any, error) {
			if err := requireGovernance(deps); err != nil {
				return nil, err
			}
			rawLimit, ok := args["max_limit"].(float64)
			if !ok {
				return nil, fmt.Errorf("max_limit is required and must be a number")
			}
			if rawLimit < 0 {
				return nil, fmt.Errorf("max_limit cannot be negative")
			}
			reset, err := stringArg(args, "reset_duration", true)
			if err != nil {
				return nil, err
			}
			if d, err := tables.ParseDuration(reset); err != nil || d <= 0 {
				return nil, fmt.Errorf("invalid reset_duration %q; expected a positive duration like 1d, 1w, 1M", reset)
			}
			teamID, err := stringArg(args, "team_id", false)
			if err != nil {
				return nil, err
			}
			customerID, err := stringArg(args, "customer_id", false)
			if err != nil {
				return nil, err
			}
			if (teamID == "") == (customerID == "") {
				return nil, fmt.Errorf("set exactly one of team_id or customer_id")
			}
			budget := tables.TableBudget{
				ID:            uuid.NewString(),
				MaxLimit:      rawLimit,
				ResetDuration: reset,
				LastReset:     Now(),
			}
			if teamID != "" {
				budget.TeamID = &teamID
			} else {
				budget.CustomerID = &customerID
			}
			if err := deps.Governance.CreateBudget(ctx, &budget); err != nil {
				return nil, fmt.Errorf("could not create budget: %w", err)
			}
			out := budgetListRow(&budget)
			if deps.Reloader != nil {
				var reloadErr error
				if teamID != "" {
					_, reloadErr = deps.Reloader.ReloadTeam(ctx, teamID)
				} else {
					_, reloadErr = deps.Reloader.ReloadCustomer(ctx, customerID)
				}
				if reloadErr != nil {
					out["reload_warning"] = reloadErr.Error()
				}
			} else {
				out["reload_warning"] = "governance cache was not reloaded; the budget is stored but may not enforce until restart"
			}
			return out, nil
		},
	}
}

func reloadVirtualKey(ctx context.Context, deps *Deps, id string) string {
	if deps.Reloader == nil {
		return "governance cache was not reloaded; the key is stored but may not authenticate until restart"
	}
	if _, err := deps.Reloader.ReloadVirtualKey(ctx, id); err != nil {
		return err.Error()
	}
	return ""
}
