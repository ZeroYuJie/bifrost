package bedrock

import (
	"testing"

	schemas "github.com/maximhq/bifrost/core/schemas"
)

// TestResponseModel verifies that when the request model was resolved through
// a key alias, responses echo the caller-facing alias key instead of the wire
// identifier (an opaque resource id for application inference profiles).
func TestResponseModel(t *testing.T) {
	wireModel := "4gnd8iceukh0"

	// Alias resolved — caller-facing name wins.
	ctx := schemas.NewBifrostContext(nil, schemas.NoDeadline)
	ctx.SetValue(schemas.BifrostContextKeyResolvedAlias, &schemas.ResolvedAlias{
		Key: "claude-sonnet-5-660169747010",
		Config: &schemas.AliasConfig{
			ModelID: wireModel,
		},
	})
	if got := responseModel(ctx, wireModel); got != "claude-sonnet-5-660169747010" {
		t.Errorf("alias resolved: got %q, want alias key %q", got, "claude-sonnet-5-660169747010")
	}

	// No alias — wire model passes through.
	emptyCtx := schemas.NewBifrostContext(nil, schemas.NoDeadline)
	if got := responseModel(emptyCtx, wireModel); got != wireModel {
		t.Errorf("no alias: got %q, want wire model %q", got, wireModel)
	}

	// Nil ctx — wire model passes through.
	if got := responseModel(nil, wireModel); got != wireModel {
		t.Errorf("nil ctx: got %q, want wire model %q", got, wireModel)
	}
}
