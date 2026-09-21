package mcptools

import (
	"context"
	"fmt"

	"github.com/maximhq/bifrost/framework/logstore"
)

func getSessionTool() Tool {
	return Tool{
		name: "get_session",
		description: "List the LLM requests in one session (parent_request_id / conversation id), oldest first. " +
			"Use this after query_logs when a row carries a session you want to walk. Capped at 25 rows.",
		schemaJSON: `{
  "type": "object",
  "properties": {
    "session_id": {"type": "string"},
    "limit": {"type": "integer", "minimum": 1, "maximum": 25}
  },
  "required": ["session_id"]
}`,
		execute: func(ctx context.Context, deps *Deps, args map[string]any) (any, error) {
			if err := requireLogs(deps); err != nil {
				return nil, err
			}
			sessionID, err := stringArg(args, "session_id", true)
			if err != nil {
				return nil, err
			}
			limit, err := intArg(args, "limit", 10, MaxLogRows)
			if err != nil {
				return nil, err
			}
			result, err := deps.LogManager.GetSessionLogs(ctx, sessionID, &logstore.PaginationOptions{
				Limit: limit, Offset: 0, SortBy: "timestamp", Order: "asc",
			})
			if err != nil {
				return nil, fmt.Errorf("session fetch failed: %w", err)
			}
			if result == nil {
				return nil, fmt.Errorf("no session with id %q", sessionID)
			}
			rows := make([]LogRow, 0, len(result.Logs))
			for i := range result.Logs {
				rows = append(rows, ProjectLog(&result.Logs[i], false, LogContentChars))
			}
			return map[string]any{
				"session_id":     result.SessionID,
				"rows":           rows,
				"returned":       len(rows),
				"total_matching": result.Count,
				"has_more":       result.HasMore,
			}, nil
		},
	}
}

func getSessionSummaryTool() Tool {
	return Tool{
		name:        "get_session_summary",
		description: "Aggregate totals for one session: request count, tokens, cost, duration. Cheaper than listing the rows.",
		schemaJSON: `{
  "type": "object",
  "properties": {
    "session_id": {"type": "string"}
  },
  "required": ["session_id"]
}`,
		execute: func(ctx context.Context, deps *Deps, args map[string]any) (any, error) {
			if err := requireLogs(deps); err != nil {
				return nil, err
			}
			sessionID, err := stringArg(args, "session_id", true)
			if err != nil {
				return nil, err
			}
			result, err := deps.LogManager.GetSessionSummary(ctx, sessionID)
			if err != nil {
				return nil, fmt.Errorf("session summary failed: %w", err)
			}
			if result == nil {
				return nil, fmt.Errorf("no session with id %q", sessionID)
			}
			return result, nil
		},
	}
}

func getDroppedRequestsTool() Tool {
	return Tool{
		name: "get_dropped_requests",
		description: "How many log rows this process has dropped because ingest could not keep up. " +
			"This is a process-local counter of lost telemetry, not a count of failed LLM requests. " +
			"A non-zero number means the log store is falling behind; query_logs will under-count.",
		schemaJSON: `{
  "type": "object",
  "properties": {}
}`,
		execute: func(ctx context.Context, deps *Deps, args map[string]any) (any, error) {
			if err := requireLogs(deps); err != nil {
				return nil, err
			}
			return map[string]any{
				"dropped_requests": deps.LogManager.GetDroppedRequests(ctx),
			}, nil
		},
	}
}
