# Major Refactor Plan

## Goal

Improve readability, maintainability, and safety while preserving current CLI behavior and path-containment guarantees.

## Guiding Principles

- Keep changes incremental and reviewable (small PRs, no big-bang rewrite).
- Preserve user-facing behavior unless a change is a documented correctness fix.
- Keep command handlers thin; move orchestration and business logic into packages.
- Keep all filesystem mutations root-contained and dry-run safe.
- Prefer deterministic behavior (stable ordering, stable keep/delete decisions).

## Scope

In scope:

- CLI architecture cleanup (`cmd/` split, shared runner/output helpers).
- Duplicate logic reduction across rename/flatten/duplicate/manifest flows.
- Correctness hardening for duplicate detection and safe deletes.
- Worker/config consistency for hash-heavy operations.
- Test strategy improvements for regression safety.

Out of scope (for this refactor cycle):

- New end-user commands/features not required for architecture cleanup.
- Broad dependency changes or framework replacement.

## Refactor Phases

### Phase 1: Split and Simplify CLI Layer

Tasks:

- Split `cmd/main.go` into:
  - `cmd/root.go`
  - `cmd/rename.go`
  - `cmd/flatten.go`
  - `cmd/duplicate.go`
  - `cmd/manifest.go`
  - `cmd/common.go` (shared helpers)
- Extract repeated command flow into reusable helpers:
  - path validation
  - file collection setup
  - progress lifecycle
  - summary rendering
- Centralize default skip files in one place.

Deliverables:

- Smaller command files with focused responsibility.
- Shared constants/helpers replacing repeated literals.

Acceptance criteria:

- No behavior changes for existing commands except formatting cleanup.
- `go run ./cmd --help` and per-command help remain accurate.

### Phase 2: Introduce Use-Case Orchestration Layer

Tasks:

- Add application/service layer that orchestrates workflows and returns structured results.
- Keep `cmd/` responsible only for flag parsing + output formatting.
- Move repeated orchestration patterns out of CLI and into package-level services.

Deliverables:

- Clear separation:
  - CLI adapters in `cmd/`
  - operation logic in `pkg/`

Acceptance criteria:

- Core flows testable without Cobra command invocation.
- Less duplication in command handlers.

### Phase 3: Correctness and Safety Hardening

Tasks:

- Replace metadata-only duplicate checks in rename path with content-hash checks.
- Ensure all deletes/renames use `safepath.Validator` operations.
- Normalize and consistently use canonical root paths.
- Strengthen symlink escape handling policy for mutating operations.

Deliverables:

- Updated renamer duplicate logic.
- Consistent path-safe mutation flow across packages.

Acceptance criteria:

- Tests prove no false duplicate deletions for same size/mtime but different bytes.
- Path containment tests cover edge and symlink cases.

### Phase 4: Performance and Configuration Consistency

Tasks:

- Wire `--workers` consistently across manifest, flatten, and duplicate operations.
- Use parallel hashing APIs where beneficial and deterministic.
- Ensure large dataset behavior remains predictable and memory-safe.

Deliverables:

- Unified worker configuration behavior.
- Benchmarks or measured before/after notes for hot paths (where changed).

Acceptance criteria:

- Worker count changes affect all hash-heavy commands.
- No regressions in race tests and package benchmarks.

### Phase 5: Cleanup, Docs, and Contract Alignment

Tasks:

- Remove dead/legacy package paths that are no longer used.
- Align README/help text with implemented behavior only.
- Update `docs/ARCHITECTURE.md` to reflect new structure and flow.

Deliverables:

- Clean package map and up-to-date docs.

Acceptance criteria:

- No stale command references in help/docs.
- Architecture doc matches actual package boundaries.

## PR Plan (Recommended Sequence)

1. CLI file split with zero behavior change.
2. Shared CLI helpers/constants extraction.
3. Use-case layer extraction for one command (pilot, e.g., manifest).
4. Use-case extraction for rename/flatten/duplicate.
5. Renamer correctness hardening (hash-based duplicate safety).
6. Worker/config unification and performance pass.
7. Cleanup dead code and docs alignment.

## Regression Risk Areas and Guards

- Duplicate selection correctness: add deterministic keep-file tests.
- Dry-run behavior: assert no FS mutations in each command.
- Path containment: test nested paths, relative roots, and symlink escapes.
- Output compatibility: keep summary blocks stable or intentionally versioned.

## Quality Gate for Every Refactor PR

- `make fmt`
- `make vet`
- `make lint`
- `make test-race`

If behavior changed:

- Update `README.md` and `docs/ARCHITECTURE.md` in the same PR.

## Definition of Done

- Command handlers are small, readable, and orchestration-focused.
- Duplicate logic is centralized and content-hash based for destructive actions.
- Path safety and dry-run invariants are enforced consistently.
- Docs and CLI help accurately describe implemented behavior.
- Full quality gate passes.
