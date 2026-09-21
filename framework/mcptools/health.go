package mcptools

import (
	"context"
	"sync"
	"time"
)

const healthPingTimeout = 10 * time.Second

func getHealthTool() Tool {
	return Tool{
		name: "get_health",
		description: "Ping this deployment's config, log and vector stores. " +
			"Reports ok, degraded, or that store pings are disabled. This is process health, not LLM-provider uptime.",
		schemaJSON: `{
  "type": "object",
  "properties": {}
}`,
		execute: func(ctx context.Context, deps *Deps, args map[string]any) (any, error) {
			if deps != nil && deps.DisableDBPings {
				return map[string]any{
					"status": "ok",
					"components": map[string]string{
						"config_store": "disabled",
						"log_store":    "disabled",
						"vector_store": "disabled",
					},
				}, nil
			}
			var configPing, logsPing, vectorPing Pinger
			if deps != nil {
				configPing, logsPing, vectorPing = deps.ConfigPing, deps.LogsPing, deps.VectorPing
			}
			pingCtx, cancel := context.WithTimeout(ctx, healthPingTimeout)
			defer cancel()
			components := pingStores(pingCtx, map[string]Pinger{
				"config_store": configPing,
				"log_store":    logsPing,
				"vector_store": vectorPing,
			})
			status := "ok"
			for _, state := range components {
				if state == "error" {
					status = "degraded"
					break
				}
			}
			return map[string]any{"status": status, "components": components}, nil
		},
	}
}

func pingStores(ctx context.Context, stores map[string]Pinger) map[string]string {
	components := make(map[string]string, len(stores))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for name, pinger := range stores {
		wg.Add(1)
		go func(name string, pinger Pinger) {
			defer wg.Done()
			state := pingStore(ctx, pinger)
			mu.Lock()
			components[name] = state
			mu.Unlock()
		}(name, pinger)
	}
	wg.Wait()
	return components
}

func pingStore(ctx context.Context, pinger Pinger) string {
	if pinger == nil {
		return "not_configured"
	}
	if err := pinger.Ping(ctx); err != nil {
		return "error"
	}
	return "ok"
}

func getVersionTool() Tool {
	return Tool{
		name:        "get_version",
		description: "The running Bifrost transport version.",
		schemaJSON: `{
  "type": "object",
  "properties": {}
}`,
		execute: func(ctx context.Context, deps *Deps, args map[string]any) (any, error) {
			version := ""
			if deps != nil {
				version = deps.Version
			}
			if version == "" {
				version = "unknown"
			}
			return map[string]any{"version": version}, nil
		},
	}
}
