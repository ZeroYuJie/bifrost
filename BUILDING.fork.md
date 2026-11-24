# Maintaining this Bifrost fork

Read [AGENTS.md](AGENTS.md#first-rule--aws-bedrock-execution-boundary) before
release work. Its Bedrock execution boundary and native server build rule
apply to every command below and to delegated work.

## Main branch and release baseline

| Item | Maintained value |
| --- | --- |
| Fork repository | `ZeroYuJie/bifrost`, remote `fork` |
| Maintained/default branch | `main`, tracking `fork/main` |
| Official upstream | `maximhq/bifrost`, remote `origin` |
| Integrated HTTP release | `transports/v2.6.0` |
| Upstream release commit | `172617bb045c6f43d697843c115e64f0155ed143` |
| Validated production code | `adaca95d2`, version `v2.3.0-fork.adaca95d2` |
| Last production acceptance | 2026-10-16, Asia/Shanghai |

`main` contains the upstream release and the fork's patches. Preserve its
history with ordinary merges and fast-forward pushes. Keep `origin` read-only;
publish this fork to `fork`, never to `origin`. Upstream `main`/`dev` are useful
for investigation but are not automatic deployment targets. Retain old patch
and upgrade branches as history unless the user requests cleanup.

GitHub Actions is disabled for this fork. The inherited workflow files are
retained for upstream comparison, but include automatic releases and scheduled
live tests. Keep them inactive; enabling fork-specific CI requires an explicit
decision and must respect the server execution boundary. The weekly advisory
check runs separately and does not enable Actions.

The receipt on `aws-useast-bala-sub2api-001` is
`/home/webserver/app/bifrost/release-v2.3.0.json`. It records the binary checksum,
source commit, server acceptance, and backup location. A later documentation
commit does not change the deployed binary's code revision. Update this table
and the receipt after each accepted release; never infer production state from
repository HEAD.

The v2.6.0 source upgrade retains the response-model patches and the
OpenAI-to-Converse regression. Its new `sendEvent` closure must call
`responseModel` for ordinary Chat stream chunks; the final usage chunk alone
does not preserve the contract. `TestConverseResponseModel` covers both APIs,
unary/streaming, aliased/plain models, post-hooks, exact thinking fields and
token caps against loopback mocks. Harness folder `372` uses the fully
overridden Bedrock endpoint fixture with fake credentials; it never needs a
real AWS endpoint. Validate collection augment/filter locally; live/provider
sweeps remain prohibited on the workstation.

The required native build toolchain is Go 1.27.4, CGO/GCC and Node 25 (the
checked server toolchain is Node 25.9.0). Its missing `libatomic.so.1` is
extracted from the Amazon Linux package into the isolated build-tools directory
and supplied through `LD_LIBRARY_PATH`, without changing system packages.
Rebuild external Go plugins with the same Go version if any are configured.
Rehearse configstore/logstore migrations against copies of each database that
actually exists. Source integration and a pending binary do not change the
validated-production row above; update that only after authorized cutover and
production acceptance.

## Differences owned by the fork

1. **Bedrock response model aliases.** `responseModel` in
   `core/providers/bedrock/utils.go` and its call sites in `bedrock.go` return
   the caller-facing alias while leaving request/wire models unchanged.
   Preserve Chat/Responses unary and streaming responses, lifecycle/final
   chunks and post-hooks; Titan/Cohere/Nova embeddings; rerank; images;
   CountTokens including estimates; and Anthropic InvokeModel unary/streaming
   paths. Regression coverage lives in `responsemodel_test.go`.
2. **OpenAI-to-Bedrock regression coverage.** `between_tools` is an upstream
   feature, not a private thinking implementation. Keep
   `TestBetweenToolsOpenAIJSONToConverse` in
   `core/providers/bedrock/adaptivethinkingdisplay_test.go`: raw Chat/Responses
   JSON, token-limit precedence, opaque profile aliases, and exact outgoing
   thinking/effort fields.

Inspect new provider branches as well as conflicts at every upgrade. Resolve
by preserving behavior, rather than mechanically retaining an old patched
line. Retire a private implementation only when the new official tag without
it passes the same regressions and necessary authorized server acceptance.
Keep useful regression tests after retiring implementation code.

## Following upstream releases

The weekly check is advisory: inspect official GitHub Releases for
`transports/v*`, exclude drafts/prereleases, and distinguish HTTP releases from
CLI/core/framework/plugin releases. GitHub's generic latest release can refer
to the CLI. Report new versions, official links, dates in Asia/Shanghai,
relevant fixes, migrations, and toolchain changes. Stay quiet when there is no
new version or material new risk.

The check does not merge, push, build, contact production, or deploy. Upgrading
starts from a user-selected formal release. Before changes, record worktree
state, HEAD, remote main, chosen tag and peeled commit, release notes,
dependencies, migration implications, and patch risks. Preserve unrelated
work; do not auto-stash, reset, or force-push it away.

For a chosen version, replace `vX.Y.Z` with that exact release:

```bash
set -eu
git fetch --no-tags fork main
git switch main
git merge --ff-only fork/main
git fetch --no-tags origin refs/tags/transports/vX.Y.Z:refs/tags/transports/vX.Y.Z
git switch -c codex/upgrade-bifrost-vX.Y.Z
git merge --no-ff transports/vX.Y.Z
```

Keep build-time module replacements in an isolated build tree, outside the
maintained source diff. After validation, merge the upgrade branch into
`main`, push `fork/main` without force, fetch it back, and prove local and
remote commit IDs match. If another writer advanced remote main, integrate
those commits and revalidate before pushing.

## Offline verification

From `core/`, using the Go version required by `go.mod`:

```bash
go test ./providers/bedrock -run 'BetweenTools|Adaptive|ResponseModel|ConverseReasoning' -count=1
go test ./schemas ./providers/anthropic -run 'BetweenTools|ReasoningTypeSurvivesChatResponsesConversion' -count=1
go test -race ./providers/bedrock -run '^(TestInvokeResponseModel|TestResponseModel|TestResponseModelPostHookRunnerNilSafety|TestInvokeAnthropicResponsesStream_RetryableException_ChunkIsRetryable)$' -count=1 -timeout=120s
```

These tests use pure conversions or loopback mocks with fake credentials.
Do not substitute broad provider/live commands on the workstation. Run checks
appropriate to changed paths; documentation-only merges do not require
repeating unchanged runtime tests. If the streaming alias wrapper changes,
compare `ns/op` and `allocs/op` from the existing
`BenchmarkResponseModelPostHookRunner`.

## Native server build

Use an isolated source directory on the target server. UI assets are static;
prefer building them there too when Node is available. From that source root,
use separate subshells so paths remain valid:

```bash
set -eu
(cd ui && npm ci && npm run build)

FORK_VERSION="vX.Y.Z-fork.<source-commit>"
(
  cd transports
  go mod edit \
    -replace=github.com/maximhq/bifrost/core=../core \
    -replace=github.com/maximhq/bifrost/framework=../framework
  cd bifrost-http
  CGO_ENABLED=1 go build \
    -ldflags "-w -s -X main.Version=${FORK_VERSION}" \
    -trimpath -tags sqlite_static -o ../../bifrost-candidate .
)
file bifrost-candidate
go version -m bifrost-candidate
sha256sum bifrost-candidate
```

Inspect target OS/architecture and native GCC/CGO dependencies before building.
Verify artifact GOOS/GOARCH and source hashes against the target and reviewed
commit. Local core/framework replacements are essential: published modules
omit the fork's core patches. Never commit the temporary `go.mod` edits. Use
release module requirements rather than lowering Go versions for a workstation.

## Server validation and deployment

First validate the candidate on an isolated server port and database copies.
Authorized real Bedrock checks run there, with credentials remaining there.
Check raw outbound thinking type, caller effort, token cap, returned alias,
text, and affected unary/streaming paths. Check health and database integrity
after migration.

Before production cutover, obtain explicit restart/deployment authorization.
Retain the old binary, config and startup scripts, and consistent snapshots of
every existing SQLite database via SQLite's backup API or while fully stopped.
Do not copy a live `.db` alone while ignoring its WAL. Config/log migrations
can be separate and irreversible; rollback can require restoring databases
as well as the old executable.

Replace the binary atomically and use the existing startup method. Verify
version, health, configuration entity counts, and authorized server inference
acceptance; retain evidence and stop temporary instances. Report merge, push,
build, deployment, and acceptance as separate facts. A repository merge alone
never authorizes another restart.

## Sonnet 5.5 request contract

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

Recheck explicit capability overrides during upgrades. The token cap still
bounds total output, including any thinking generated in tool workflows.
