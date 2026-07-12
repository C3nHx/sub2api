# Issue #8 repository gate record

This file is the tracked record format for repository-wide verification. Record results only after running each command against the code-and-assets commit from a clean worktree. The evidence-only commit that fills in this record may follow without changing the verified code or runner. Keep raw command output under `output/issue-8/` and link its relative path in the evidence column.

## Run metadata

- Verified code/assets commit: `NOT RECORDED`
- Worktree state: `NOT VERIFIED`
- Started UTC: `NOT RECORDED`
- Finished UTC: `NOT RECORDED`
- Operator/environment: `NOT RECORDED`

Allowed statuses are `PASS`, `FAIL`, and `NOT RUN`. Do not mark a gate `PASS` from an earlier code/assets commit or a dirty-worktree run.

| Gate | Status | Command or scope | Evidence |
| --- | --- | --- | --- |
| Issue #8 runner self-test | NOT RUN | `verification/issue-8/run.tests.ps1` | |
| Issue #8 Full acceptance | NOT RUN | `verification/issue-8/run.ps1 -Mode Full` | `output/issue-8/summary.md` |
| Issue #8 Docker cleanup | NOT RUN | `resource_cleanup.passed` and all `resource_cleanup.remaining` arrays are empty | `output/issue-8/run-status.json` |
| Post-suite Docker resource audit | NOT RUN | Remove only run-labeled/test-owned resources; verify pre-existing running container IDs remain running and healthy | `output/issue-8/docker-final-state.txt` |
| Backend unit tests | NOT RUN | `go test -tags=unit ./...` | |
| Backend integration tests | NOT RUN | `SUB2API_TEST_RUN_ID=<unique> go test -tags=integration ./...` | |
| Backend vet | NOT RUN | `go vet ./...` | |
| Wire generation drift | NOT RUN | `go generate ./cmd/server` then `git diff --exit-code -- backend/cmd/server/wire_gen.go` | |
| Issue #8 integration race | NOT RUN | `SUB2API_TEST_RUN_ID=<same unique run> go test -race -tags=integration ./internal/repository -run '^TestIssue8GatewayAdmission' -count=1` using `Dockerfile.race` | |
| Frontend typecheck | NOT RUN | `vue-tsc --noEmit` | |
| Frontend lint | NOT RUN | `eslint` | |
| Frontend tests | NOT RUN | `vitest run` | |
| Frontend production build | NOT RUN | `vue-tsc -b` and `vite build` | |

## Failures and follow-up

Record the failing gate, the first actionable error, the owner, and the rerun evidence here. Leave this section empty when no failure has been observed on the final commit.
