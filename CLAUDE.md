# CLAUDE.md

This repo already has a full agent-instruction set — read these before making changes, in this order:

1. **[AGENTS.md](AGENTS.md)** — the workflow: research phase, planning conventions, implementation guidelines (stdout/stderr discipline, config auto-migration rules), and verification steps.
2. **[SPEC.md](SPEC.md)** — architecture, `config.json`/`cache.json` schemas, every implemented feature in detail, and the roadmap. This is the authoritative design doc; keep it in sync with the code.
3. **[SKILLS.md](SKILLS.md)** — the command reference written for an agent driving the CLI (non-interactive flag patterns, stdout/stderr conventions, automation examples).
4. **[README.md](README.md)** — human-facing install/usage docs.

These apply regardless of which agent or tool is doing the work; nothing here is Claude-specific beyond this file itself.

## Project-specific notes for Claude Code

- **Go module**: `plaid-cli`, imports `github.com/plaid/plaid-go/vNN/plaid` — check `go.mod` for the current major version before assuming API shapes; Plaid bumps the SDK's major version with nearly every API change, and struct fields/constructors shift between versions (e.g. `NewLinkTokenCreateRequest` dropped its `user` positional arg around v2x in favor of `SetUser`). Verify against the installed SDK source in `$(go env GOMODCACHE)/github.com/plaid/plaid-go/vNN@vNN.x.x/plaid` rather than assuming.
- **Build/verify loop**: `go build -o plaid-cli.exe . && go vet ./... && go test ./... && gofmt -l .` — matches what AGENTS.md Phase 4 expects.
- **Two Plaid account models exist side by side.** Teams created before Dec 10, 2025 get a `user_token` from `/user/create`; teams created after (including any Trial-plan team) get only a `user_id`, and Income Verification's `/link/token/create` call still requires the legacy `user_token` unless Plaid support has explicitly granted "user token access" to that team. If a Credit/Income call fails with `user_token is required for income_verification product`, that's an account entitlement gap, not a code bug — see the "Payroll Income" section in SPEC.md.
- **Never commit real Plaid credentials.** `~/.plaid-cli/config.json` (outside the repo) holds `client_id`/`secret`; treat sandbox credentials as low-sensitivity but production credentials as secrets. When testing live API calls, prefer a throwaway `go run` scratch program checked against `cfg.Environment == "sandbox"` before making any request, and delete it when done — don't leave scratch files in the tree.
