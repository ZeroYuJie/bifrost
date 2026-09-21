package mcptools

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/maximhq/bifrost/framework/logstore"
)

const MCPFilterSchema = `{
  "type": "object",
  "description": "Narrows which MCP tool executions are considered. Omit a field to leave that dimension unfiltered. If start_time is omitted the last 24 hours are used.",
  "properties": {
    "start_time": {"type": "string", "description": "A relative offset like -7d, -24h, -30m, or an RFC3339 timestamp."},
    "end_time": {"type": "string", "description": "RFC3339 timestamp. Defaults to now."},
    "tool_names": {"type": "array", "items": {"type": "string"}},
    "server_labels": {"type": "array", "items": {"type": "string"}, "description": "MCP server that provided the tool."},
    "status": {"type": "array", "items": {"type": "string"}, "description": "success, error, or processing."},
    "virtual_key_ids": {"type": "array", "items": {"type": "string"}},
    "apps": {"type": "array", "items": {"type": "string"}},
    "min_latency": {"type": "number", "description": "Milliseconds."},
    "max_latency": {"type": "number", "description": "Milliseconds."},
    "content_search": {"type": "string", "description": "Substring match against tool arguments and results."}
  }
}`

func parseMCPFilters(raw map[string]any, now time.Time) (*logstore.MCPToolLogSearchFilters, error) {
	filters := &logstore.MCPToolLogSearchFilters{}
	if raw == nil {
		raw = map[string]any{}
	}
	known := map[string]bool{
		"start_time": true, "end_time": true, "tool_names": true, "server_labels": true,
		"status": true, "virtual_key_ids": true, "apps": true,
		"min_latency": true, "max_latency": true, "content_search": true,
	}
	unknown := []string{}
	for key := range raw {
		if !known[key] {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("unknown filter fields: %s. Supported fields are: start_time, end_time, tool_names, server_labels, status, virtual_key_ids, apps, min_latency, max_latency, content_search", strings.Join(unknown, ", "))
	}
	start, err := parseTime(raw["start_time"], now)
	if err != nil {
		return nil, fmt.Errorf("start_time: %w", err)
	}
	end, err := parseTime(raw["end_time"], now)
	if err != nil {
		return nil, fmt.Errorf("end_time: %w", err)
	}
	if end == nil {
		end = &now
	}
	if start == nil {
		defaulted := end.Add(-DefaultLookback)
		start = &defaulted
	}
	if start.After(*end) {
		return nil, fmt.Errorf("start_time must be before end_time")
	}
	filters.StartTime, filters.EndTime = start, end
	for key, target := range map[string]*[]string{
		"tool_names": &filters.ToolNames, "server_labels": &filters.ServerLabels,
		"status": &filters.Status, "virtual_key_ids": &filters.VirtualKeyIDs,
		"apps": &filters.Apps,
	} {
		values, err := stringSliceField(raw, key)
		if err != nil {
			return nil, err
		}
		*target = values
	}
	if filters.MinLatency, err = floatField(raw, "min_latency"); err != nil {
		return nil, err
	}
	if filters.MaxLatency, err = floatField(raw, "max_latency"); err != nil {
		return nil, err
	}
	if search, ok := raw["content_search"].(string); ok {
		filters.ContentSearch = search
	}
	return filters, nil
}

func mcpFilterArg(args map[string]any, now time.Time) (*logstore.MCPToolLogSearchFilters, error) {
	raw, _ := args["filters"].(map[string]any)
	return parseMCPFilters(raw, now)
}

func mcpResolvedWindow(filters *logstore.MCPToolLogSearchFilters) map[string]string {
	return formatWindow(*filters.StartTime, *filters.EndTime)
}

func queryMCPLogsTool() Tool {
	return Tool{
		name: "query_mcp_logs",
		description: "List individual MCP tool executions matching a filter. Returns compact rows (timestamp, tool, server, status, latency, cost), not full argument or result bodies. " +
			"Use this for which tools failed or were slowest. For totals use count_mcp_logs or query_mcp_metrics. This is a different table from query_logs, which only sees LLM requests.",
		schemaJSON: `{
  "type": "object",
  "properties": {
    "filters": ` + MCPFilterSchema + `,
    "limit": {"type": "integer", "minimum": 1, "maximum": 25, "description": "Rows to return. Capped at 25."}
  },
  "required": ["filters"]
}`,
		execute: func(ctx context.Context, deps *Deps, args map[string]any) (any, error) {
			if err := requireLogs(deps); err != nil {
				return nil, err
			}
			filters, err := mcpFilterArg(args, Now())
			if err != nil {
				return nil, err
			}
			limit, err := intArg(args, "limit", 10, MaxLogRows)
			if err != nil {
				return nil, err
			}
			result, err := deps.LogManager.SearchMCPToolLogs(ctx, filters, &logstore.PaginationOptions{
				Limit: limit, Offset: 0, SortBy: "timestamp", Order: "desc",
			})
			if err != nil {
				return nil, fmt.Errorf("mcp log search failed: %w", err)
			}
			rows := make([]map[string]any, 0, len(result.Logs))
			for i := range result.Logs {
				rows = append(rows, projectMCPLog(&result.Logs[i], false))
			}
			return map[string]any{
				"rows":           rows,
				"returned":       len(rows),
				"total_matching": result.Pagination.TotalCount,
				"sampled":        int64(len(rows)) < result.Pagination.TotalCount,
				"window":         mcpResolvedWindow(filters),
			}, nil
		},
	}
}

func countMCPLogsTool() Tool {
	return Tool{
		name: "count_mcp_logs",
		description: "Count matching MCP tool executions and summarise them, without fetching rows. " +
			"Call this before query_mcp_logs when the window is wide or the filters are loose.",
		schemaJSON: `{
  "type": "object",
  "properties": {
    "filters": ` + MCPFilterSchema + `
  },
  "required": ["filters"]
}`,
		execute: func(ctx context.Context, deps *Deps, args map[string]any) (any, error) {
			if err := requireLogs(deps); err != nil {
				return nil, err
			}
			filters, err := mcpFilterArg(args, Now())
			if err != nil {
				return nil, err
			}
			stats, err := deps.LogManager.GetMCPToolLogStats(ctx, filters)
			if err != nil {
				return nil, fmt.Errorf("mcp count failed: %w", err)
			}
			out := map[string]any{
				"total_executions":   stats.TotalExecutions,
				"success_rate":       stats.SuccessRate,
				"average_latency_ms": stats.AverageLatency,
				"total_cost":         stats.TotalCost,
				"window":             mcpResolvedWindow(filters),
			}
			switch {
			case stats.TotalExecutions == 0:
				out["guidance"] = "Nothing matched. Widen the time range or check tool_names and server_labels before concluding there is no MCP traffic."
			case stats.TotalExecutions > LargeResultThreshold:
				out["too_many_to_list"] = true
				out["guidance"] = fmt.Sprintf("%d executions match - too many to list. Use query_mcp_metrics or query_mcp_usage_by, or a sorted query_mcp_logs top-N.", stats.TotalExecutions)
			default:
				out["guidance"] = "Small enough to list with query_mcp_logs if individual rows are needed."
			}
			return out, nil
		},
	}
}

func getMCPLogDetailTool() Tool {
	return Tool{
		name:        "get_mcp_log_detail",
		description: "Fetch one MCP tool execution by id with a truncated preview of its arguments and result. Use after query_mcp_logs to investigate a specific call.",
		schemaJSON: `{
  "type": "object",
  "properties": {
    "log_id": {"type": "string"}
  },
  "required": ["log_id"]
}`,
		execute: func(ctx context.Context, deps *Deps, args map[string]any) (any, error) {
			if err := requireLogs(deps); err != nil {
				return nil, err
			}
			id, err := stringArg(args, "log_id", true)
			if err != nil {
				return nil, err
			}
			entry, err := deps.LogManager.GetMCPToolLog(ctx, id)
			if err != nil {
				return nil, fmt.Errorf("could not load mcp log %s: %w", id, err)
			}
			if entry == nil {
				return nil, fmt.Errorf("no mcp log found with id %s", id)
			}
			return projectMCPLog(entry, true), nil
		},
	}
}

// maxMCPQueryMetrics caps how many metrics one query_mcp_metrics call may
// request. The schema's maxItems and enumSliceArg's runtime check both read
// it, as with maxQueryMetrics.
const maxMCPQueryMetrics = 3

func queryMCPMetricsTool() Tool {
	return Tool{
		name: "query_mcp_metrics",
		description: "Aggregate statistics and time series over MCP tool executions: totals, volume, cost. " +
			"This is the cheapest way to answer how much MCP traffic there was. For LLM request metrics use query_metrics.",
		schemaJSON: `{
  "type": "object",
  "properties": {
    "filters": ` + MCPFilterSchema + `,
    "metrics": {
      "type": "array",
      "minItems": 1,
      "maxItems": ` + strconv.Itoa(maxMCPQueryMetrics) + `,
      "items": {"type": "string", "enum": ["summary", "volume", "cost"]},
      "description": "'summary' returns overall totals and is usually the right starting point."
    }
  },
  "required": ["filters", "metrics"]
}`,
		execute: func(ctx context.Context, deps *Deps, args map[string]any) (any, error) {
			if err := requireLogs(deps); err != nil {
				return nil, err
			}
			filters, err := mcpFilterArg(args, Now())
			if err != nil {
				return nil, err
			}
			metrics, err := enumSliceArg(args, "metrics", "metric", []string{"summary", "volume", "cost"}, maxMCPQueryMetrics)
			if err != nil {
				return nil, err
			}
			window := logstore.SearchFilters{StartTime: filters.StartTime, EndTime: filters.EndTime}
			bucket, err := bucketSize(&window)
			if err != nil {
				return nil, err
			}
			out := map[string]any{"window": mcpResolvedWindow(filters)}
			for _, metric := range metrics {
				switch metric {
				case "summary":
					stats, err := deps.LogManager.GetMCPToolLogStats(ctx, filters)
					if err != nil {
						return nil, fmt.Errorf("mcp stats query failed: %w", err)
					}
					out["summary"] = stats
				case "volume":
					result, err := deps.LogManager.GetMCPHistogram(ctx, *filters, bucket)
					if err != nil {
						return nil, fmt.Errorf("mcp volume histogram failed: %w", err)
					}
					out["volume"] = result
				case "cost":
					result, err := deps.LogManager.GetMCPCostHistogram(ctx, *filters, bucket)
					if err != nil {
						return nil, fmt.Errorf("mcp cost histogram failed: %w", err)
					}
					out["cost"] = result
				}
			}
			return out, nil
		},
	}
}

func queryMCPUsageByTool() Tool {
	return Tool{
		name:        "query_mcp_usage_by",
		description: "Rank MCP tools by call count and cost over a window. Answers which tools are hottest.",
		schemaJSON: `{
  "type": "object",
  "properties": {
    "filters": ` + MCPFilterSchema + `,
    "limit": {"type": "integer", "minimum": 1, "maximum": 20}
  },
  "required": ["filters"]
}`,
		execute: func(ctx context.Context, deps *Deps, args map[string]any) (any, error) {
			if err := requireLogs(deps); err != nil {
				return nil, err
			}
			filters, err := mcpFilterArg(args, Now())
			if err != nil {
				return nil, err
			}
			limit, err := intArg(args, "limit", 10, MaxRankingRows)
			if err != nil {
				return nil, err
			}
			result, err := deps.LogManager.GetMCPTopTools(ctx, *filters, limit)
			if err != nil {
				return nil, fmt.Errorf("mcp top tools failed: %w", err)
			}
			tools := []logstore.MCPTopToolResult{}
			if result != nil {
				tools = result.Tools
			}
			return map[string]any{
				"tools":  tools,
				"window": mcpResolvedWindow(filters),
			}, nil
		},
	}
}

func projectMCPLog(entry *logstore.MCPToolLog, includePayload bool) map[string]any {
	row := map[string]any{
		"id":        entry.ID,
		"timestamp": entry.Timestamp.UTC().Format(time.RFC3339),
		"tool_name": entry.ToolName,
		"status":    entry.Status,
		"link":      mcpLogDetailLink(entry.ID),
	}
	if entry.ServerLabel != "" {
		row["server_label"] = entry.ServerLabel
	}
	if entry.Latency != nil {
		row["latency_ms"] = *entry.Latency
	}
	if entry.Cost != nil {
		row["cost"] = *entry.Cost
	}
	if entry.VirtualKeyName != nil && *entry.VirtualKeyName != "" {
		row["virtual_key_name"] = *entry.VirtualKeyName
	}
	if entry.UserID != nil && *entry.UserID != "" {
		row["user_id"] = *entry.UserID
	}
	if be := entry.ErrorDetailsParsed; be != nil {
		row["error_message"] = truncateText(be.GetErrorString(), 300)
	}
	if includePayload {
		if entry.Arguments != "" {
			row["arguments"] = truncateText(entry.Arguments, DetailContentChars)
		}
		if entry.Result != "" {
			row["result"] = truncateText(entry.Result, DetailContentChars)
		}
	}
	return row
}
