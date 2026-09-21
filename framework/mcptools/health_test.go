package mcptools

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakePinger struct {
	err        error
	sawContext context.Context
}

func (f *fakePinger) Ping(ctx context.Context) error {
	f.sawContext = ctx
	return f.err
}

func TestGetHealthAllOk(t *testing.T) {
	result, err := runTool(t, "get_health", &Deps{
		ConfigPing: &fakePinger{},
		LogsPing:   &fakePinger{},
		VectorPing: &fakePinger{},
	}, map[string]any{})
	require.NoError(t, err)
	out := result.(map[string]any)
	require.Equal(t, "ok", out["status"])
	components := out["components"].(map[string]string)
	require.Equal(t, "ok", components["config_store"])
	require.Equal(t, "ok", components["log_store"])
	require.Equal(t, "ok", components["vector_store"])
}

func TestGetHealthDegradedOnPingError(t *testing.T) {
	result, err := runTool(t, "get_health", &Deps{
		ConfigPing: &fakePinger{},
		LogsPing:   &fakePinger{err: errors.New("down")},
		VectorPing: &fakePinger{},
	}, map[string]any{})
	require.NoError(t, err)
	out := result.(map[string]any)
	require.Equal(t, "degraded", out["status"])
	components := out["components"].(map[string]string)
	require.Equal(t, "ok", components["config_store"])
	require.Equal(t, "error", components["log_store"])
}

func TestGetHealthDisabledSkipsPings(t *testing.T) {
	failing := &fakePinger{err: errors.New("should not be called")}
	result, err := runTool(t, "get_health", &Deps{
		DisableDBPings: true,
		LogsPing:       failing,
	}, map[string]any{})
	require.NoError(t, err)
	out := result.(map[string]any)
	require.Equal(t, "ok", out["status"])
	components := out["components"].(map[string]string)
	require.Equal(t, "disabled", components["log_store"])
	require.Nil(t, failing.sawContext)
}

func TestGetHealthNotConfigured(t *testing.T) {
	result, err := runTool(t, "get_health", &Deps{}, map[string]any{})
	require.NoError(t, err)
	out := result.(map[string]any)
	require.Equal(t, "ok", out["status"])
	components := out["components"].(map[string]string)
	require.Equal(t, "not_configured", components["config_store"])
	require.Equal(t, "not_configured", components["log_store"])
	require.Equal(t, "not_configured", components["vector_store"])
}

func TestGetHealthPassesCallerContext(t *testing.T) {
	type scopeKey struct{}
	pinger := &fakePinger{}
	ctx := context.WithValue(context.Background(), scopeKey{}, "caller-scope")
	_, err := runToolCtx(t, ctx, "get_health", &Deps{ConfigPing: pinger}, map[string]any{})
	require.NoError(t, err)
	require.Equal(t, "caller-scope", pinger.sawContext.Value(scopeKey{}))
}

func TestGetVersion(t *testing.T) {
	result, err := runTool(t, "get_version", &Deps{Version: "v1.2.3"}, map[string]any{})
	require.NoError(t, err)
	require.Equal(t, "v1.2.3", result.(map[string]any)["version"])
}

func TestGetVersionUnknownWhenEmpty(t *testing.T) {
	result, err := runTool(t, "get_version", &Deps{}, map[string]any{})
	require.NoError(t, err)
	require.Equal(t, "unknown", result.(map[string]any)["version"])
}
