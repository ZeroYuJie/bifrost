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

Then build (Linux target, with embedded UI):

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
