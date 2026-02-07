# AGENTS.md

## Purpose
- This file is the operating guide for coding agents working in `file-organizer`.
- Follow existing repository patterns first; do not introduce new frameworks or workflow styles unless requested.
- Prefer minimal, safe changes that preserve CLI behavior and path-containment guarantees.

## Rule Sources Checked
- `.cursorrules`: not present in this repository.
- `.cursor/rules/`: not present in this repository.
- `.github/copilot-instructions.md`: not present in this repository.
- This `AGENTS.md` is therefore the primary agent instruction file for repo-local behavior.

## Project Snapshot
- Language: Go (`go 1.25.4` in `go.mod`).
- Module path: `file-organizer`.
- CLI entrypoint: `cmd/main.go` (Cobra-based command wiring).
- Core packages live under `pkg/`.
- High-level architecture reference: `docs/ARCHITECTURE.md`.

## Important Directories
- `cmd/`: CLI command setup and top-level user output.
- `pkg/collector`: recursive/non-recursive file metadata collection.
- `pkg/sanitizer`: filename normalization and timestamped naming.
- `pkg/safepath`: root containment validation and safe filesystem operations.
- `pkg/renamer`: phase 1 rename flow.
- `pkg/flattener`: phase 2 flatten + duplicate-by-content handling.
- `pkg/deduplicator`: phase 3 duplicate detection and deletion.
- `pkg/hasher`: SHA256 hashing + worker pool primitives.
- `pkg/manifest`: manifest generation and JSON persistence.
- `scripts/`: helper shell scripts for local test data setup.

## Standard Commands
- Install local dev tools: `make tools`
- Build binary: `make build`
- Format code: `make fmt`
- Run static checks: `make vet`
- Run lint suite: `make lint`
- Run tests (coverage): `make test`
- Run tests with race detector: `make test-race`
- Full coverage report + HTML: `make test-cover`
- Full gate (fmt + vet + lint + race tests): `make check`
- Pre-commit gate used by project: `make pre-commit`
- Clean build/tool artifacts: `make clean`

## Useful Direct Invocations
- Run CLI without building: `go run ./cmd --help`
- Build directly with Go: `go build -o file-organizer ./cmd`
- Run vet directly: `go vet ./pkg/...`
- Run linter directly (after `make tools`): `.tools/golangci-lint run --timeout 5m`
- Format a specific file quickly: `gofmt -w -s <file> && .tools/goimports -w -local file-organizer <file>`

## Single-Test and Focused Test Commands
- Run one package: `go test ./pkg/renamer -count=1`
- Run one test function: `go test ./pkg/renamer -run '^TestRenamer_RenameFiles_DryRun$' -count=1`
- Run one test with verbose output: `go test ./pkg/renamer -run '^TestRenamer_RenameFiles_DryRun$' -count=1 -v`
- Run one subtest: `go test ./pkg/manifest -run 'TestNewGenerator/valid directory' -count=1 -v`
- Run single test with race detector: `go test ./pkg/flattener -run '^TestFlattener_FlattenFiles_Basic$' -race -count=1`
- Run all package tests directly: `go test ./pkg/... -count=1`
- Run all tests in repo: `go test ./... -count=1`
- Run benchmarks for one package: `go test ./pkg/hasher -run '^$' -bench . -benchmem`

## Lint and Formatting Expectations
- Linter: `golangci-lint` (configured in `.golangci.yml`, version pinned in `Makefile`).
- Formatting is enforced by both `gofmt` and `goimports`.
- Local import prefix is `file-organizer` (configured via `goimports -local file-organizer`).
- Do not hand-format; run `make fmt` after edits.
- Keep functions reasonably small (`funlen` enforced; tests are more permissive).
- Keep cyclomatic/cognitive complexity under configured thresholds.

## Import Conventions
- Use grouped imports in standard Go order:
- 1) standard library,
- 2) third-party modules,
- 3) local module imports (`file-organizer/...`).
- Let `goimports` handle grouping and ordering.
- Avoid unused imports (strictly linted).
- Dot imports are disallowed by linter configuration.

## Naming and API Design
- Exported identifiers use `PascalCase`; unexported use `camelCase`.
- Prefer descriptive names over abbreviations unless domain-standard (`ctx`, `err`, `op`).
- Keep receiver names short and consistent (`r`, `f`, `d`, `h`, `v`, `g`).
- Sentinel errors are declared as package vars (`Err...`) when callers need `errors.Is` behavior.
- Error variable names should follow Go conventions (`err`, `statErr`, `walkErr` when needed).
- Tests typically use `TestType_Method_Scenario` or `TestFeature_Scenario` naming.

## Types and Data Structures
- Prefer explicit structs for operation/result payloads (`Result`, `MoveOperation`, `DeleteOperation`).
- Keep struct fields typed clearly and avoid `interface{}`.
- Use `map[string]struct{}` for sets (see `manifest.UniqueHashes`).
- Initialize slices/maps with capacity when size is known.
- Keep JSON structures stable (`Manifest`, `ManifestEntry`) and tagged fields explicit.

## Error Handling Rules
- Always check returned errors (`errcheck` is enabled).
- Wrap errors with context using `%w` when propagating.
- Error strings should be lowercase and without trailing punctuation.
- Prefer early returns to reduce nesting.
- Avoid returning both nil values in ambiguous ways (`nilnil` lint enabled).
- Do not ignore type assertion failures; checked assertions are expected.

## Context and Resource Management
- Prefer context-aware APIs for new blocking work (`noctx` and `fatcontext` are enabled).
- Close files/channels/tickers promptly; place `defer` immediately after successful open.
- Ensure goroutines have a clear shutdown path (close channels, stop tickers, wait on workers).
- Avoid hidden global mutable state; prefer constructor options and explicit dependencies.

## Filesystem and Safety Invariants
- Never mutate filesystem paths outside the user-provided root directory.
- Use `pkg/safepath.Validator` for mutating operations (`SafeRename`, `SafeRemove`, `SafeRemoveDir`).
- Validate both source and destination paths before operations.
- Preserve dry-run behavior: compute/report planned operations without mutating state.
- Common skip files used by commands: `.DS_Store`, `Thumbs.db`, `organizer.log`.
- Keep deterministic behavior where possible (sorted output, stable "keep" choices for duplicates).

## Concurrency and Performance
- Hashing should use `pkg/hasher` worker pool APIs.
- Default worker count is CPU-based; expose worker controls via options/flags when relevant.
- When concurrency is used, ensure channels are closed correctly and goroutines can terminate.
- Preserve race-safety; run `make test-race` after concurrency-related changes.

## CLI and User Output Conventions
- Keep command handlers in `cmd/main.go` thin and orchestration-focused.
- Print clear command banners and summary blocks, consistent with existing commands.
- Keep dry-run messaging explicit (`=== DRY RUN - no changes will be made ===`).
- Verbose mode should add details, not change behavior.
- Keep progress reporting non-blocking and stoppable.

## Testing Conventions
- Test framework: `testing` + `github.com/stretchr/testify` (`assert` and `require`).
- Use `require` for preconditions/fatal setup failures, `assert` for value checks.
- Mark helpers with `t.Helper()`.
- Many tests use `t.Parallel()`; preserve/add it when tests are isolation-safe.
- Use `t.TempDir()` where possible for filesystem isolation.
- Prefer deterministic test data and assertions (sorted paths, exact counts).
- Add/adjust tests in the same package as changes.

## NOLINT Usage
- `nolint` directives must be specific and justified.
- `nolintlint` requires both a specific linter name and an explanation.
- Avoid adding `nolint` if code can be rewritten to satisfy the linter.

## Agent Change Checklist
- Run `make fmt`.
- Run `make lint`.
- Run targeted tests for touched package(s).
- Run `make test-race` for concurrency-sensitive changes.
- If behavior changes, update `README.md` and/or `docs/ARCHITECTURE.md`.
- Keep scope focused; avoid incidental refactors unless requested.
