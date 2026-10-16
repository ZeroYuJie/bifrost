# Building this fork

Upstream `transports/go.mod` resolves `core`/`framework` from the Go module
proxy, which does NOT contain this fork's patches. Point them at the in-repo
sources before building (equivalent to the upstream go.work dev flow):

```bash
cd transports/bifrost-http
# note: the transports go.mod lives one level up (transports/), so the
# relative paths are ../core and ../framework, not ../../
go mod edit \
  -replace=github.com/maximhq/bifrost/core=../core \
  -replace=github.com/maximhq/bifrost/framework=../framework
```

Build the production executable on the target Linux server with its native Go
and GCC toolchains. Upload the UI assets and patched source to an isolated
build directory first; do not deploy a workstation executable. Then build
with embedded UI:

```bash
# 1. UI assets (needs Node; outputs to transports/bifrost-http/ui)
cd ui && npm ci && npm run build && npm run copy-build

# 2. Binary (needs Go 1.27+, CGO, gcc for sqlite)
cd transports/bifrost-http
CGO_ENABLED=1 go build \
  -ldflags "-w -s -X main.Version=dev-fork.2" \
  -trimpath -tags "sqlite_static" \
  -o bifrost-http .
```

The `go mod edit -replace` changes are local build-tree state — do not commit
them; keep the fork diff limited to the patch itself.

## Syncing upstream

This branch is a private patch branch (no PR upstream). To keep it current:

```bash
git fetch origin
git checkout main && git merge --ff-only origin/main
git push fork main
git checkout fix/bedrock-alias-response-model
git rebase main
git push fork fix/bedrock-alias-response-model --force-with-lease
```

Conflicts, if any, will be near the `responseModel(...)` call sites in
`core/providers/bedrock/bedrock.go` — resolve by keeping the patched call.
Then rebuild and redeploy per the steps above. If upstream ever implements
equivalent behavior (response model echoes the caller-facing name), retire
this branch and go back to stock releases.

## v2.3.0 upgrade and between-tools thinking

The `codex/upgrade-bifrost-v2.3.0` branch merges `transports/v2.3.0`
(`9237ec06672406e437b3cb33fc793ffd0403b3ae`) without rewriting the private
patch history. The Bedrock alias response patch is retained, including the new
Nova embedding branch and Anthropic InvokeModel unary/streaming paths. Build
with the local core/framework replacements above;
an unmodified official binary does not include these patches.

For OpenAI Chat Completions requests targeting Sonnet 5.5, send:

```json
{
  "max_completion_tokens": 4096,
  "reasoning": {"type": "between_tools", "effort": "low"}
}
```

For Responses, use `max_output_tokens` with the same `reasoning` object.
The converter emits `additionalModelRequestFields.thinking` containing only
`{"type":"between_tools"}` and forwards effort independently. Use `low`,
`medium`, or `high`; `none` alone does not select this mode. Do not attach
`display` or `budget_tokens` to the outgoing thinking object. This turns off
up-front thinking; tool workflows can still return progress between tool calls.

An application inference profile with an opaque `model_id` must retain
`model_name: "anthropic.claude-sonnet-5-5"` in its alias configuration so the
converter recognises the model's capabilities.

Run these offline checks from `core/` using Go 1.27:

```bash
go test ./providers/bedrock -run 'BetweenTools|Adaptive|ResponseModel|ConverseReasoning' -count=1
go test ./schemas ./providers/anthropic -run 'BetweenTools|ReasoningTypeSurvivesChatResponsesConversion' -count=1
```

`TestBetweenToolsOpenAIJSONToConverse` covers raw OpenAI Chat/Responses JSON,
token-limit precedence, and opaque profile aliases. It verifies conversion and
serialization without calling AWS. Before replacing a deployed binary, retain
the old binary, snapshot each SQLite database through SQLite's backup API, and
keep `config.json` and the existing startup scripts.
