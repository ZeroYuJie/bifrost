package mcptools

import (
	"context"
	"fmt"

	"github.com/maximhq/bifrost/framework/configstore"
)

func listProvidersTool() Tool {
	return Tool{
		name: "list_providers",
		description: "List configured LLM providers on this deployment (name and how many keys each has). " +
			"Never returns key material. This is the configured catalog, not traffic; a provider with no recent requests still appears here.",
		schemaJSON: `{
  "type": "object",
  "properties": {}
}`,
		execute: func(ctx context.Context, deps *Deps, args map[string]any) (any, error) {
			if err := requireGovernance(deps); err != nil {
				return nil, err
			}
			providers, err := deps.Governance.GetProviders(ctx)
			if err != nil {
				return nil, fmt.Errorf("could not list providers: %w", err)
			}
			rows := make([]map[string]any, 0, len(providers))
			for i := range providers {
				p := providers[i]
				rows = append(rows, map[string]any{
					"name":      p.Name,
					"key_count": len(p.Keys),
				})
			}
			return map[string]any{
				"providers": rows,
				"returned":  len(rows),
			}, nil
		},
	}
}

func listMCPClientsTool() Tool {
	return Tool{
		name: "list_mcp_clients",
		description: "List configured MCP clients (upstream servers this deployment connects to): name, slug, connection type, auth type, whether disabled. " +
			"Never returns connection strings, headers or tokens. The built-in bifrostmcp client is this server itself.",
		schemaJSON: `{
  "type": "object",
  "properties": {
    "search": {"type": "string", "description": "Optional substring to narrow by name."},
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
			clients, total, err := deps.Governance.GetMCPClientsPaginated(ctx, configstore.MCPClientsQueryParams{
				Limit:  limit,
				Offset: 0,
				Search: search,
			})
			if err != nil {
				return nil, fmt.Errorf("could not list mcp clients: %w", err)
			}
			rows := make([]map[string]any, 0, len(clients))
			for i := range clients {
				c := clients[i]
				row := map[string]any{
					"id":               c.ClientID,
					"name":             c.Name,
					"endpoint_slug":    c.EndpointSlug,
					"connection_type":  c.ConnectionType,
					"auth_type":        c.AuthType,
					"disabled":         c.Disabled,
					"allow_by_default": c.AllowByDefault,
				}
				rows = append(rows, row)
			}
			return map[string]any{
				"clients":        rows,
				"returned":       len(rows),
				"total_matching": total,
			}, nil
		},
	}
}
