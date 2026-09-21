package mcptools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/maximhq/bifrost/core/schemas"
	"github.com/maximhq/bifrost/framework/configstore"
	"github.com/maximhq/bifrost/framework/configstore/tables"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// GovernanceReaderStub embeds the interface without implementing it, so a
// fake that embeds it compiles but panics on any method it does not override.
type GovernanceReaderStub struct {
	GovernanceReader
}

// fakeGovernanceReader is the governance tools' store. lookup is keyed on
// id, mirroring GetVirtualKey's own "not found" contract: a missing key
// returns configstore.ErrNotFound, not a nil, nil pair.
type fakeGovernanceReader struct {
	GovernanceReaderStub

	byID            map[string]*tables.TableVirtualKey
	teams           map[string]*tables.TableTeam
	customers       map[string]*tables.TableCustomer
	budgets         map[string]*tables.TableBudget
	providers       []tables.TableProvider
	mcpClients      []tables.TableMCPClient
	clientConfig    *configstore.ClientConfig
	err             error
	sawContext      context.Context
	sawID           string
	createdVK       *tables.TableVirtualKey
	updatedVK       *tables.TableVirtualKey
	createdTeam     *tables.TableTeam
	createdCustomer *tables.TableCustomer
	createdBudget   *tables.TableBudget
}

func (f *fakeGovernanceReader) GetVirtualKey(ctx context.Context, id string) (*tables.TableVirtualKey, error) {
	f.sawContext = ctx
	f.sawID = id
	if f.err != nil {
		return nil, f.err
	}
	vk, ok := f.byID[id]
	if !ok {
		return nil, configstore.ErrNotFound
	}
	return vk, nil
}

func (f *fakeGovernanceReader) GetVirtualKeysPaginated(ctx context.Context, params configstore.VirtualKeyQueryParams) ([]tables.TableVirtualKey, int64, error) {
	f.sawContext = ctx
	if f.err != nil {
		return nil, 0, f.err
	}
	out := make([]tables.TableVirtualKey, 0, len(f.byID))
	for _, vk := range f.byID {
		if params.Search != "" && !strings.Contains(strings.ToLower(vk.Name), strings.ToLower(params.Search)) {
			continue
		}
		if params.TeamID != "" && (vk.TeamID == nil || *vk.TeamID != params.TeamID) {
			continue
		}
		if params.CustomerID != "" && (vk.CustomerID == nil || *vk.CustomerID != params.CustomerID) {
			continue
		}
		out = append(out, *vk)
	}
	total := int64(len(out))
	if params.Limit > 0 && len(out) > params.Limit {
		out = out[:params.Limit]
	}
	return out, total, nil
}

func (f *fakeGovernanceReader) CreateVirtualKey(ctx context.Context, virtualKey *tables.TableVirtualKey, _ ...*gorm.DB) error {
	f.sawContext = ctx
	if f.err != nil {
		return f.err
	}
	if f.byID == nil {
		f.byID = map[string]*tables.TableVirtualKey{}
	}
	copied := *virtualKey
	f.byID[virtualKey.ID] = &copied
	f.createdVK = &copied
	return nil
}

func (f *fakeGovernanceReader) UpdateVirtualKey(ctx context.Context, virtualKey *tables.TableVirtualKey, _ ...*gorm.DB) error {
	f.sawContext = ctx
	if f.err != nil {
		return f.err
	}
	copied := *virtualKey
	if f.byID == nil {
		f.byID = map[string]*tables.TableVirtualKey{}
	}
	f.byID[virtualKey.ID] = &copied
	f.updatedVK = &copied
	return nil
}

func (f *fakeGovernanceReader) GetTeam(ctx context.Context, id string) (*tables.TableTeam, error) {
	f.sawContext = ctx
	if f.err != nil {
		return nil, f.err
	}
	team, ok := f.teams[id]
	if !ok {
		return nil, configstore.ErrNotFound
	}
	return team, nil
}

func (f *fakeGovernanceReader) GetTeamsPaginated(ctx context.Context, params configstore.TeamsQueryParams) ([]tables.TableTeam, int64, error) {
	f.sawContext = ctx
	if f.err != nil {
		return nil, 0, f.err
	}
	out := make([]tables.TableTeam, 0, len(f.teams))
	for _, team := range f.teams {
		if params.Search != "" && !strings.Contains(strings.ToLower(team.Name), strings.ToLower(params.Search)) {
			continue
		}
		if params.CustomerID != "" && (team.CustomerID == nil || *team.CustomerID != params.CustomerID) {
			continue
		}
		out = append(out, *team)
	}
	total := int64(len(out))
	if params.Limit > 0 && len(out) > params.Limit {
		out = out[:params.Limit]
	}
	return out, total, nil
}

func (f *fakeGovernanceReader) CreateTeam(ctx context.Context, team *tables.TableTeam, _ ...*gorm.DB) error {
	f.sawContext = ctx
	if f.err != nil {
		return f.err
	}
	if f.teams == nil {
		f.teams = map[string]*tables.TableTeam{}
	}
	copied := *team
	f.teams[team.ID] = &copied
	f.createdTeam = &copied
	return nil
}

func (f *fakeGovernanceReader) GetCustomer(ctx context.Context, id string) (*tables.TableCustomer, error) {
	f.sawContext = ctx
	if f.err != nil {
		return nil, f.err
	}
	customer, ok := f.customers[id]
	if !ok {
		return nil, configstore.ErrNotFound
	}
	return customer, nil
}

func (f *fakeGovernanceReader) GetCustomersPaginated(ctx context.Context, params configstore.CustomersQueryParams) ([]tables.TableCustomer, int64, error) {
	f.sawContext = ctx
	if f.err != nil {
		return nil, 0, f.err
	}
	out := make([]tables.TableCustomer, 0, len(f.customers))
	for _, customer := range f.customers {
		if params.Search != "" && !strings.Contains(strings.ToLower(customer.Name), strings.ToLower(params.Search)) {
			continue
		}
		out = append(out, *customer)
	}
	total := int64(len(out))
	if params.Limit > 0 && len(out) > params.Limit {
		out = out[:params.Limit]
	}
	return out, total, nil
}

func (f *fakeGovernanceReader) CreateCustomer(ctx context.Context, customer *tables.TableCustomer, _ ...*gorm.DB) error {
	f.sawContext = ctx
	if f.err != nil {
		return f.err
	}
	if f.customers == nil {
		f.customers = map[string]*tables.TableCustomer{}
	}
	copied := *customer
	f.customers[customer.ID] = &copied
	f.createdCustomer = &copied
	return nil
}

func (f *fakeGovernanceReader) GetBudget(ctx context.Context, id string, _ ...*gorm.DB) (*tables.TableBudget, error) {
	f.sawContext = ctx
	if f.err != nil {
		return nil, f.err
	}
	budget, ok := f.budgets[id]
	if !ok {
		return nil, configstore.ErrNotFound
	}
	return budget, nil
}

func (f *fakeGovernanceReader) GetBudgets(ctx context.Context) ([]tables.TableBudget, error) {
	f.sawContext = ctx
	if f.err != nil {
		return nil, f.err
	}
	out := make([]tables.TableBudget, 0, len(f.budgets))
	for _, budget := range f.budgets {
		out = append(out, *budget)
	}
	return out, nil
}

func (f *fakeGovernanceReader) CreateBudget(ctx context.Context, budget *tables.TableBudget, _ ...*gorm.DB) error {
	f.sawContext = ctx
	if f.err != nil {
		return f.err
	}
	if f.budgets == nil {
		f.budgets = map[string]*tables.TableBudget{}
	}
	copied := *budget
	f.budgets[budget.ID] = &copied
	f.createdBudget = &copied
	return nil
}

func (f *fakeGovernanceReader) GetProviders(ctx context.Context) ([]tables.TableProvider, error) {
	f.sawContext = ctx
	if f.err != nil {
		return nil, f.err
	}
	return f.providers, nil
}

func (f *fakeGovernanceReader) GetMCPClientsPaginated(ctx context.Context, params configstore.MCPClientsQueryParams) ([]tables.TableMCPClient, int64, error) {
	f.sawContext = ctx
	if f.err != nil {
		return nil, 0, f.err
	}
	out := make([]tables.TableMCPClient, 0, len(f.mcpClients))
	for _, client := range f.mcpClients {
		if params.Search != "" && !strings.Contains(strings.ToLower(client.Name), strings.ToLower(params.Search)) {
			continue
		}
		out = append(out, client)
	}
	total := int64(len(out))
	if params.Limit > 0 && len(out) > params.Limit {
		out = out[:params.Limit]
	}
	return out, total, nil
}

func (f *fakeGovernanceReader) GetClientConfig(ctx context.Context) (*configstore.ClientConfig, error) {
	f.sawContext = ctx
	if f.err != nil {
		return nil, f.err
	}
	return f.clientConfig, nil
}

type fakeReloader struct {
	vkIDs       []string
	teamIDs     []string
	customerIDs []string
	err         error
	sawContext  context.Context
}

func (f *fakeReloader) ReloadVirtualKey(ctx context.Context, id string) (*tables.TableVirtualKey, error) {
	f.sawContext = ctx
	f.vkIDs = append(f.vkIDs, id)
	return nil, f.err
}

func (f *fakeReloader) ReloadTeam(ctx context.Context, id string) (*tables.TableTeam, error) {
	f.sawContext = ctx
	f.teamIDs = append(f.teamIDs, id)
	return nil, f.err
}

func (f *fakeReloader) ReloadCustomer(ctx context.Context, id string) (*tables.TableCustomer, error) {
	f.sawContext = ctx
	f.customerIDs = append(f.customerIDs, id)
	return nil, f.err
}

func TestDescribeVirtualKeyReportsUnavailableWithoutAGovernanceReader(t *testing.T) {
	_, err := runTool(t, "describe_virtual_key", &Deps{}, map[string]any{"virtual_key_id": "vk-1"})
	require.ErrorContains(t, err, "not available")
}

func TestDescribeVirtualKeyRequiresAnID(t *testing.T) {
	deps := &Deps{Governance: &fakeGovernanceReader{}}
	_, err := runTool(t, "describe_virtual_key", deps, map[string]any{"virtual_key_id": "  "})
	require.ErrorContains(t, err, "virtual_key_id")
}

func TestDescribeVirtualKeyReportsUnknownID(t *testing.T) {
	fake := &fakeGovernanceReader{byID: map[string]*tables.TableVirtualKey{}}
	deps := &Deps{Governance: fake}
	_, err := runTool(t, "describe_virtual_key", deps, map[string]any{"virtual_key_id": "vk-missing"})
	require.ErrorContains(t, err, "vk-missing")
	require.ErrorContains(t, err, "describe_filter_space")
}

// The caller's context is what carries queryscope's row-level filter into the
// store - GetVirtualKey narrows to rows the caller may see the same way every
// LogReader method does. Losing it here would return any key to anyone who
// asked, the same failure mode LogReader's own tools guard against.
func TestDescribeVirtualKeyPassesCallerContextToStore(t *testing.T) {
	type scopeKey struct{}
	fake := &fakeGovernanceReader{byID: map[string]*tables.TableVirtualKey{
		"vk-1": {ID: "vk-1", Name: "prod"},
	}}
	deps := &Deps{Governance: fake}
	ctx := context.WithValue(context.Background(), scopeKey{}, "caller-scope")
	_, err := runToolCtx(t, ctx, "describe_virtual_key", deps, map[string]any{"virtual_key_id": "vk-1"})
	require.NoError(t, err)
	require.Equal(t, "caller-scope", fake.sawContext.Value(scopeKey{}))
	require.Equal(t, "vk-1", fake.sawID)
}

// The result must never carry the key's own secret value, its rotation
// history, or any provider credential beneath it - only the budget/limit/
// provider shape describeVirtualKey hand-picks. This is the regression test
// for that: a row deliberately carrying secret-shaped data in every field
// describeVirtualKey does not touch, asserting none of it survives.
func TestDescribeVirtualKeyNeverLeaksSecretFields(t *testing.T) {
	teamID := "team-1"
	expires := time.Now().Add(24 * time.Hour)
	vk := &tables.TableVirtualKey{
		ID:                "vk-1",
		Name:              "prod",
		Description:       "production traffic",
		TeamID:            &teamID,
		ExpiresAt:         &expires,
		Value:             schemas.SecretVar{Val: "sk-super-secret-value"},
		PreviousValueHash: "leftover-hash",
		Budgets: []tables.TableBudget{
			{ID: "budget-1", MaxLimit: 100, CurrentUsage: 42, ResetDuration: "1M", LastReset: time.Now()},
		},
		RateLimit: &tables.TableRateLimit{ID: "rl-1", TokenMaxLimit: int64Ptr(1000), TokenCurrentUsage: 250},
		ProviderConfigs: []tables.TableVirtualKeyProviderConfig{
			{
				Provider:      "openai",
				AllowedModels: []string{"gpt-4o"},
				Keys: []tables.TableKey{
					{ID: 1, Name: "prod-openai-key", Value: schemas.SecretVar{Val: "sk-should-never-appear"}},
				},
			},
		},
	}
	fake := &fakeGovernanceReader{byID: map[string]*tables.TableVirtualKey{"vk-1": vk}}
	deps := &Deps{Governance: fake}

	result, err := runTool(t, "describe_virtual_key", deps, map[string]any{"virtual_key_id": "vk-1"})
	require.NoError(t, err)

	out, ok := result.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "vk-1", out["id"])
	require.Equal(t, "prod", out["name"])
	require.Equal(t, "team-1", out["team_id"])

	budgets, ok := out["budgets"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, budgets, 1)
	require.InDelta(t, 100.0, budgets[0]["max_limit"], 0.001)
	require.InDelta(t, 42.0, budgets[0]["current_usage"], 0.001)

	rateLimit, ok := out["rate_limit"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, int64(1000), rateLimit["token_max_limit"])
	require.NotContains(t, rateLimit, "request_max_limit", "an unset limit family must not read as a limit of zero")

	providers, ok := out["providers"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, providers, 1)
	require.Equal(t, "openai", providers[0]["provider"])
	require.Equal(t, []string{"gpt-4o"}, providers[0]["allowed_models"])
	require.NotContains(t, providers[0], "keys", "no provider key detail, secret or otherwise, may reach the model")

	// The bounded-result serialization is the last line of defense; walking
	// the returned value directly is what proves the secret was never placed
	// there in the first place, not merely stripped afterward.
	serialized := boundToolResult(result)
	require.NotContains(t, serialized, "sk-super-secret-value")
	require.NotContains(t, serialized, "sk-should-never-appear")
	require.NotContains(t, serialized, "leftover-hash")
	require.NotContains(t, serialized, "prod-openai-key", "not even a key's name belongs in a chat tool result")
}

// A budget under an active override must report the effective cap, not the
// raw one the override has already changed - the same distinction the
// dashboard itself makes (see TableBudget.EffectiveMaxLimit).
func TestDescribeVirtualKeyBudgetReportsEffectiveLimitUnderOverride(t *testing.T) {
	vk := &tables.TableVirtualKey{
		ID:   "vk-1",
		Name: "prod",
		Budgets: []tables.TableBudget{
			{
				ID: "budget-1", MaxLimit: 100, CurrentUsage: 10, ResetDuration: "1M", LastReset: time.Now(),
				OverrideAmount: 50, OverrideMode: tables.BudgetOverrideModeForever,
			},
		},
	}
	fake := &fakeGovernanceReader{byID: map[string]*tables.TableVirtualKey{"vk-1": vk}}
	deps := &Deps{Governance: fake}

	result, err := runTool(t, "describe_virtual_key", deps, map[string]any{"virtual_key_id": "vk-1"})
	require.NoError(t, err)

	budgets := result.(map[string]any)["budgets"].([]map[string]any)
	require.InDelta(t, 150.0, budgets[0]["max_limit"], 0.001, "override amount must be folded into the reported cap")
	require.Equal(t, true, budgets[0]["override_active"])
}

func int64Ptr(v int64) *int64 { return &v }

func TestListVirtualKeysNeverLeaksSecret(t *testing.T) {
	fake := &fakeGovernanceReader{byID: map[string]*tables.TableVirtualKey{
		"vk-1": {ID: "vk-1", Name: "prod", Value: schemas.SecretVar{Val: "sk-bf-must-not-appear"}},
	}}
	result, err := runTool(t, "list_virtual_keys", &Deps{Governance: fake}, map[string]any{})
	require.NoError(t, err)
	out := result.(map[string]any)
	rows := out["virtual_keys"].([]map[string]any)
	require.Len(t, rows, 1)
	require.Equal(t, "vk-1", rows[0]["id"])
	require.NotContains(t, rows[0], "value")
	require.NotContains(t, boundToolResult(result), "sk-bf-must-not-appear")
}

func TestListVirtualKeysReportsUnavailableWithoutGovernance(t *testing.T) {
	_, err := runTool(t, "list_virtual_keys", &Deps{}, map[string]any{})
	require.ErrorContains(t, err, "not available")
}

func TestCreateVirtualKeyReturnsSecretOnceAndReloads(t *testing.T) {
	fake := &fakeGovernanceReader{}
	reloader := &fakeReloader{}
	type scopeKey struct{}
	ctx := context.WithValue(context.Background(), scopeKey{}, "caller-scope")
	result, err := runToolCtx(t, ctx, "create_virtual_key", &Deps{Governance: fake, Reloader: reloader}, map[string]any{
		"name": "staging",
	})
	require.NoError(t, err)
	out := result.(map[string]any)
	value, ok := out["value"].(string)
	require.True(t, ok)
	require.True(t, strings.HasPrefix(value, VirtualKeyPrefix))
	require.Equal(t, "staging", out["name"])
	require.Equal(t, true, out["allow_all_providers"])
	require.NotEmpty(t, out["id"])
	require.Len(t, reloader.vkIDs, 1)
	require.Equal(t, out["id"], reloader.vkIDs[0])
	require.Equal(t, "caller-scope", fake.sawContext.Value(scopeKey{}))
	require.Equal(t, "caller-scope", reloader.sawContext.Value(scopeKey{}))

	listed, err := runTool(t, "list_virtual_keys", &Deps{Governance: fake}, map[string]any{})
	require.NoError(t, err)
	require.NotContains(t, boundToolResult(listed), value)
}

func TestCreateVirtualKeyRejectsTeamAndCustomerTogether(t *testing.T) {
	_, err := runTool(t, "create_virtual_key", &Deps{Governance: &fakeGovernanceReader{}}, map[string]any{
		"name":        "bad",
		"team_id":     "team-1",
		"customer_id": "cust-1",
	})
	require.ErrorContains(t, err, "both")
}

func TestCreateVirtualKeyWarnsWhenReloaderIsMissing(t *testing.T) {
	result, err := runTool(t, "create_virtual_key", &Deps{Governance: &fakeGovernanceReader{}}, map[string]any{
		"name": "no-reload",
	})
	require.NoError(t, err)
	require.Contains(t, result.(map[string]any)["reload_warning"], "not reloaded")
}

func TestDeactivateVirtualKeySetsInactiveAndReloads(t *testing.T) {
	active := true
	fake := &fakeGovernanceReader{byID: map[string]*tables.TableVirtualKey{
		"vk-1": {ID: "vk-1", Name: "prod", IsActive: &active},
	}}
	reloader := &fakeReloader{}
	result, err := runTool(t, "deactivate_virtual_key", &Deps{Governance: fake, Reloader: reloader}, map[string]any{
		"virtual_key_id": "vk-1",
	})
	require.NoError(t, err)
	out := result.(map[string]any)
	require.Equal(t, false, out["is_active"])
	require.Equal(t, []string{"vk-1"}, reloader.vkIDs)
	require.False(t, fake.updatedVK.IsActiveValue())
}

func TestUpdateVirtualKeyRenamesWithoutTouchingSecret(t *testing.T) {
	fake := &fakeGovernanceReader{byID: map[string]*tables.TableVirtualKey{
		"vk-1": {ID: "vk-1", Name: "old", Value: schemas.SecretVar{Val: "sk-bf-secret"}},
	}}
	reloader := &fakeReloader{}
	result, err := runTool(t, "update_virtual_key", &Deps{Governance: fake, Reloader: reloader}, map[string]any{
		"virtual_key_id": "vk-1",
		"name":           "new",
	})
	require.NoError(t, err)
	out := result.(map[string]any)
	require.Equal(t, "new", out["name"])
	require.NotContains(t, out, "value")
	require.NotContains(t, boundToolResult(result), "sk-bf-secret")
	require.Equal(t, []string{"vk-1"}, reloader.vkIDs)
}

func TestListAndDescribeTeam(t *testing.T) {
	customerID := "cust-1"
	fake := &fakeGovernanceReader{
		teams: map[string]*tables.TableTeam{
			"team-1": {ID: "team-1", Name: "platform", CustomerID: &customerID, VirtualKeyCount: 3},
		},
	}
	listed, err := runTool(t, "list_teams", &Deps{Governance: fake}, map[string]any{})
	require.NoError(t, err)
	rows := listed.(map[string]any)["teams"].([]map[string]any)
	require.Len(t, rows, 1)
	require.Equal(t, "team-1", rows[0]["id"])
	require.Equal(t, int64(3), rows[0]["virtual_key_count"])

	described, err := runTool(t, "describe_team", &Deps{Governance: fake}, map[string]any{"team_id": "team-1"})
	require.NoError(t, err)
	require.Equal(t, "platform", described.(map[string]any)["name"])
	require.Equal(t, "cust-1", described.(map[string]any)["customer_id"])
}

func TestCreateTeamReloads(t *testing.T) {
	fake := &fakeGovernanceReader{}
	reloader := &fakeReloader{}
	result, err := runTool(t, "create_team", &Deps{Governance: fake, Reloader: reloader}, map[string]any{
		"name": "platform",
	})
	require.NoError(t, err)
	out := result.(map[string]any)
	require.Equal(t, "platform", out["name"])
	require.Len(t, reloader.teamIDs, 1)
	require.Equal(t, out["id"], reloader.teamIDs[0])
}

func TestCreateCustomerReloads(t *testing.T) {
	fake := &fakeGovernanceReader{}
	reloader := &fakeReloader{}
	result, err := runTool(t, "create_customer", &Deps{Governance: fake, Reloader: reloader}, map[string]any{
		"name": "acme",
	})
	require.NoError(t, err)
	require.Equal(t, "acme", result.(map[string]any)["name"])
	require.Len(t, reloader.customerIDs, 1)
}

func TestCreateBudgetRequiresExactlyOneOwner(t *testing.T) {
	deps := &Deps{Governance: &fakeGovernanceReader{}}
	_, err := runTool(t, "create_budget", deps, map[string]any{"max_limit": 10.0, "reset_duration": "1d"})
	require.ErrorContains(t, err, "exactly one")
	_, err = runTool(t, "create_budget", deps, map[string]any{
		"max_limit": 10.0, "reset_duration": "1d", "team_id": "t", "customer_id": "c",
	})
	require.ErrorContains(t, err, "exactly one")
}

func TestCreateBudgetOnTeamReloadsTeam(t *testing.T) {
	fake := &fakeGovernanceReader{}
	reloader := &fakeReloader{}
	result, err := runTool(t, "create_budget", &Deps{Governance: fake, Reloader: reloader}, map[string]any{
		"max_limit":      50.0,
		"reset_duration": "1w",
		"team_id":        "team-1",
	})
	require.NoError(t, err)
	out := result.(map[string]any)
	require.InDelta(t, 50.0, out["max_limit"], 0.001)
	require.Equal(t, "team-1", out["team_id"])
	require.Equal(t, []string{"team-1"}, reloader.teamIDs)
	require.NotContains(t, out, "virtual_key_id")
}

func TestListProvidersOmitsKeyMaterial(t *testing.T) {
	fake := &fakeGovernanceReader{
		providers: []tables.TableProvider{
			{Name: "openai", Keys: []tables.TableKey{{Name: "prod-key", Value: schemas.SecretVar{Val: "sk-openai-secret"}}}},
		},
	}
	result, err := runTool(t, "list_providers", &Deps{Governance: fake}, map[string]any{})
	require.NoError(t, err)
	rows := result.(map[string]any)["providers"].([]map[string]any)
	require.Len(t, rows, 1)
	require.Equal(t, "openai", rows[0]["name"])
	require.Equal(t, 1, rows[0]["key_count"])
	require.NotContains(t, rows[0], "keys")
	serialized := boundToolResult(result)
	require.NotContains(t, serialized, "sk-openai-secret")
	require.NotContains(t, serialized, "prod-key")
}

func TestListMCPClientsOmitsConnectionSecrets(t *testing.T) {
	conn := schemas.NewSecretVar("https://example.internal/mcp?token=secret")
	fake := &fakeGovernanceReader{
		mcpClients: []tables.TableMCPClient{
			{ClientID: "c-1", Name: "github", EndpointSlug: "github", ConnectionType: "http", AuthType: "headers", ConnectionString: conn},
		},
	}
	result, err := runTool(t, "list_mcp_clients", &Deps{Governance: fake}, map[string]any{})
	require.NoError(t, err)
	rows := result.(map[string]any)["clients"].([]map[string]any)
	require.Len(t, rows, 1)
	require.Equal(t, "github", rows[0]["name"])
	require.NotContains(t, rows[0], "connection_string")
	require.NotContains(t, boundToolResult(result), "token=secret")
}

func TestRotateVirtualKeyReturnsNewSecretAndReloads(t *testing.T) {
	fake := &fakeGovernanceReader{byID: map[string]*tables.TableVirtualKey{
		"vk-1": {ID: "vk-1", Name: "prod", Value: schemas.SecretVar{Val: "sk-bf-old"}},
	}}
	reloader := &fakeReloader{}
	type scopeKey struct{}
	ctx := context.WithValue(context.Background(), scopeKey{}, "caller-scope")
	result, err := runToolCtx(t, ctx, "rotate_virtual_key", &Deps{Governance: fake, Reloader: reloader}, map[string]any{
		"virtual_key_id": "vk-1",
	})
	require.NoError(t, err)
	out := result.(map[string]any)
	value, ok := out["value"].(string)
	require.True(t, ok)
	require.True(t, strings.HasPrefix(value, VirtualKeyPrefix))
	require.NotEqual(t, "sk-bf-old", value)
	require.Equal(t, "prod", out["name"])
	require.NotContains(t, out, "previous_value_expires_at")
	require.Equal(t, []string{"vk-1"}, reloader.vkIDs)
	require.Equal(t, "caller-scope", fake.sawContext.Value(scopeKey{}))
	require.NotNil(t, fake.updatedVK.RotatedAt)
	require.False(t, fake.updatedVK.HasActivePreviousValue(Now()))
	require.NotContains(t, boundToolResult(result), "sk-bf-old")
}

func TestRotateVirtualKeyCooldownKeepsPreviousUntilExpiry(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	Now = func() time.Time { return now }
	t.Cleanup(func() { Now = func() time.Time { return time.Now().UTC() } })

	fake := &fakeGovernanceReader{
		byID: map[string]*tables.TableVirtualKey{
			"vk-1": {ID: "vk-1", Name: "prod", Value: schemas.SecretVar{Val: "sk-bf-old"}},
		},
		clientConfig: &configstore.ClientConfig{VKRotationCooldown: schemas.Duration(5 * time.Minute)},
	}
	result, err := runTool(t, "rotate_virtual_key", &Deps{Governance: fake, Reloader: &fakeReloader{}}, map[string]any{
		"virtual_key_id": "vk-1",
	})
	require.NoError(t, err)
	out := result.(map[string]any)
	require.Equal(t, now.Add(5*time.Minute), out["previous_value_expires_at"])
	require.True(t, fake.updatedVK.HasActivePreviousValue(now))
	require.Equal(t, "sk-bf-old", fake.updatedVK.PreviousValue.GetValue())
	require.NotContains(t, boundToolResult(result), "sk-bf-old")
}

func TestRotateVirtualKeyZeroCooldownClearsPrevious(t *testing.T) {
	exp := time.Now().UTC().Add(time.Hour)
	fake := &fakeGovernanceReader{byID: map[string]*tables.TableVirtualKey{
		"vk-1": {
			ID: "vk-1", Name: "prod", Value: schemas.SecretVar{Val: "sk-bf-current"},
			PreviousValue:          *schemas.NewSecretVar("sk-bf-older"),
			PreviousValueExpiresAt: &exp,
		},
	}}
	result, err := runTool(t, "rotate_virtual_key", &Deps{Governance: fake, Reloader: &fakeReloader{}}, map[string]any{
		"virtual_key_id": "vk-1",
	})
	require.NoError(t, err)
	require.False(t, fake.updatedVK.HasActivePreviousValue(Now()))
	require.NotContains(t, boundToolResult(result), "sk-bf-older")
	require.NotContains(t, boundToolResult(result), "sk-bf-current")
}

func TestRotateVirtualKeyReportsUnknownID(t *testing.T) {
	_, err := runTool(t, "rotate_virtual_key", &Deps{Governance: &fakeGovernanceReader{}}, map[string]any{
		"virtual_key_id": "vk-missing",
	})
	require.ErrorContains(t, err, "vk-missing")
}
