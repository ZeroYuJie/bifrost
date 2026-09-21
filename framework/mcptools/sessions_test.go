package mcptools

import (
	"context"
	"testing"
	"time"

	"github.com/maximhq/bifrost/framework/logstore"
	"github.com/stretchr/testify/require"
)

func TestGetSessionReportsUnavailableWithoutLogs(t *testing.T) {
	_, err := runTool(t, "get_session", &Deps{}, map[string]any{"session_id": "s-1"})
	require.ErrorContains(t, err, "not available")
}

func TestGetSessionListsOldestFirst(t *testing.T) {
	fake := &fakeLogReader{
		sessionResult: &logstore.SessionDetailResult{
			SessionID: "s-1",
			Count:     2,
			HasMore:   false,
			Logs: []logstore.Log{
				{ID: "r-1", Timestamp: time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC), Provider: "openai", Model: "gpt-4o", Status: "success"},
				{ID: "r-2", Timestamp: time.Date(2026, 9, 18, 10, 1, 0, 0, time.UTC), Provider: "openai", Model: "gpt-4o", Status: "success"},
			},
		},
	}
	type scopeKey struct{}
	ctx := context.WithValue(context.Background(), scopeKey{}, "caller-scope")
	result, err := runToolCtx(t, ctx, "get_session", &Deps{LogManager: fake}, map[string]any{"session_id": "s-1"})
	require.NoError(t, err)
	out := result.(map[string]any)
	require.Equal(t, "s-1", out["session_id"])
	require.Equal(t, 2, out["returned"])
	require.Equal(t, int64(2), out["total_matching"])
	rows := out["rows"].([]LogRow)
	require.Equal(t, "r-1", rows[0].ID)
	require.Equal(t, "s-1", fake.sessionIDSeen)
	require.Equal(t, "caller-scope", fake.sawContext.Value(scopeKey{}))
}

func TestGetSessionSummary(t *testing.T) {
	fake := &fakeLogReader{
		sessionSummary: &logstore.SessionSummaryResult{SessionID: "s-1", Count: 3, TotalCost: 0.12, TotalTokens: 400},
	}
	result, err := runTool(t, "get_session_summary", &Deps{LogManager: fake}, map[string]any{"session_id": "s-1"})
	require.NoError(t, err)
	summary := result.(*logstore.SessionSummaryResult)
	require.Equal(t, int64(3), summary.Count)
	require.InDelta(t, 0.12, summary.TotalCost, 0.0001)
}

func TestGetDroppedRequests(t *testing.T) {
	fake := &fakeLogReader{droppedRequests: 7}
	result, err := runTool(t, "get_dropped_requests", &Deps{LogManager: fake}, map[string]any{})
	require.NoError(t, err)
	require.Equal(t, int64(7), result.(map[string]any)["dropped_requests"])
}
