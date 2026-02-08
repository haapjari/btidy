# Zero Data Loss Guarantee — Implementation Plan

## Problem Statement

btidy is an optimistic, best-effort system with no rollback capability. Within the
target directory, destructive operations are permanent and non-atomic at the batch
level. A thorough audit identified these data loss risks:

| Severity | Risk |
|----------|------|
| **HIGH** | Unzipper silently overwrites existing files (`O_TRUNC`) |
| **HIGH** | No rollback or transaction mechanism; partial failures leave inconsistent state |
| **MEDIUM** | No automatic pre-operation backup or manifest |
| **MEDIUM** | TOCTOU races in safety checks (Lstat-then-act gaps) |
| **MEDIUM** | Files modified externally between hash and delete phases |
| **LOW** | Hash errors silently swallowed in deduplicator |
| **LOW** | No aggregate zip bomb protection |

## Goal

Within the boundaries of a functioning filesystem and exclusive access to the target
directory, **no user data is permanently lost without explicit user action**
(`btidy purge`).

## Design Principles

### 1. `.btidy/` metadata directory

All safety infrastructure lives inside the target directory:

```
<target>/
  .btidy/
    lock                                # advisory flock file
    trash/                              # soft-deleted files
      <run-id>/
        <original-relative-path>
    journal/                            # mutation logs per command run
      <run-id>.jsonl
    manifests/                          # auto-generated pre-operation snapshots
      <run-id>.json
```

`<run-id>` format: `<command>-<YYYYMMDDTHHmmss>` (e.g. `flatten-20260208T143022`).

This keeps everything within safepath's containment boundary, is discoverable, and
is easy to reason about.

### 2. Trash replaces delete

Files are never permanently deleted during normal operation. `os.Remove` is replaced
with a move to `.btidy/trash/<run-id>/<relative-path>`. Only `btidy purge`
permanently deletes. This makes every operation reversible.

### 3. Journal-first mutation

Every filesystem mutation is logged to the journal *before* execution. On crash, the
journal shows exactly what was attempted and what succeeded. On rollback, the journal
is replayed in reverse.

---

## Phases

### Phase 1 — `.btidy/` metadata directory + skip list

Foundation for all subsequent phases. No behavioral changes to existing commands.

**New package:** `pkg/metadata/`

```go
type Dir struct {
    root      string               // .btidy/ absolute path
    validator *safepath.Validator   // parent target's validator
}

func Init(targetRoot string, validator *safepath.Validator) (*Dir, error)
func (d *Dir) TrashDir(runID string) string
func (d *Dir) JournalPath(runID string) string
func (d *Dir) ManifestPath(runID string) string
func (d *Dir) LockPath() string
func (d *Dir) RunID(command string) string   // generates timestamped run ID
```

**Changes to existing code:**

| File | Change |
|------|--------|
| `cmd/common.go` | Add `.btidy` to `defaultSkipFiles` |
| `pkg/collector/collector.go` | Verify `.btidy` is skipped via existing skip-file mechanism |

**Tests:** Unit tests for `metadata.Dir`. E2e test confirming `.btidy` is never collected.

**Estimated size:** ~80 lines production, ~120 lines test.

---

### Phase 2 — Trash package

Introduce soft-delete capability. Not yet wired into any command.

**New package:** `pkg/trash/`

```go
type Trasher struct {
    trashRoot  string               // .btidy/trash/<run-id>/
    targetRoot string               // the target directory
    validator  *safepath.Validator
}

func New(metaDir *metadata.Dir, runID string, validator *safepath.Validator) (*Trasher, error)
func (t *Trasher) Trash(path string) error            // validate + move to trash
func (t *Trasher) TrashPath(path string) string        // preview: where would this go?
func (t *Trasher) Restore(trashedPath string) error    // move back from trash
func (t *Trasher) RestoreAll() error                   // restore everything in this run
func (t *Trasher) Purge() error                        // permanently delete this run's trash
```

**How `Trash` works:**

1. `validator.ValidatePathForWrite(path)` — confirm path is within target.
2. Compute relative path: `rel, _ := filepath.Rel(targetRoot, path)`.
3. Destination: `filepath.Join(trashRoot, rel)`.
4. `validator.SafeMkdirAll(filepath.Dir(destination))` — create parent dirs in trash.
5. `os.Rename(path, destination)` — atomic move (same filesystem guaranteed since
   trash is inside the target).

Atomic per-file (same as current `SafeRename`) but reversible.

**Tests:** Unit tests for trash/restore round-trip, path validation, cross-directory
structure preservation.

**Estimated size:** ~120 lines production, ~200 lines test.

---

### Phase 3 — Wire trash into all delete operations

Replace every `SafeRemove` call with `Trasher.Trash`. Largest change by line count
but the most mechanical.

Add `Trasher` as an optional dependency to each domain package constructor:

```go
// Example: flattener
func NewWithValidator(
    validator *safepath.Validator,
    dryRun bool,
    workers int,
    trasher *trash.Trasher,
) (*Flattener, error)
```

When `trasher` is nil, fall back to `SafeRemove` (backward compatible for tests).
When non-nil, use `trasher.Trash()`.

**Files changed:**

| File | Line | Change |
|------|------|--------|
| `pkg/deduplicator/deduplicator.go` | 308 | `trasher.Trash(file.Path)` replaces `SafeRemove` |
| `pkg/flattener/flattener.go` | 263 | `trasher.Trash(dupPath)` replaces `SafeRemove` |
| `pkg/renamer/renamer.go` | 313, 351 | `trasher.Trash(f.Path)` replaces `SafeRemove` |
| `pkg/unzipper/unzipper.go` | 198 | `trasher.Trash(archivePath)` replaces `SafeRemove` |
| `pkg/usecase/service.go` | executors | Create `Trasher` in each executor, pass to constructors |
| All `NewWithValidator` signatures | | Add optional `trasher` parameter |

**Operation struct changes:** Add `TrashedTo string` field to `DeleteOperation`,
`MoveOperation`, etc. so the CLI can display where files were trashed.

**Tests:** Update all existing unit tests to pass `nil` trasher (backward compat).
Add new tests verifying trash behavior. E2e test: run `btidy duplicate`, verify
files are in `.btidy/trash/`, run `btidy purge`, verify they are gone.

**Estimated size:** ~60 lines new (purge command scaffold), ~100 lines changes
across domain packages, ~150 lines new tests.

---

### Phase 4 — Re-hash before delete

Add pre-delete content verification to deduplicator and flattener, matching the
pattern already used in `renamer.go:300-311`.

**Changes:**

| File | Current behavior | New behavior |
|------|-----------------|--------------|
| `pkg/deduplicator/deduplicator.go` (`deleteFile`) | Lstat only | Lstat + re-hash + compare |
| `pkg/flattener/flattener.go` (`handleDuplicate`) | Lstat only | Lstat + re-hash + compare |

**New sentinel error:** `ErrContentChanged` — returned when pre-delete hash does not
match. Recorded on the operation as an error, not a fatal abort.

**Pattern** (mirrors `renamer.go:300-311`):

```go
currentHash, err := d.hasher.ComputeHash(file.Path)
if err != nil {
    op.Error = fmt.Errorf("re-hash before delete: %w", err)
    return op
}
if currentHash != file.Hash {
    op.Error = fmt.Errorf("content changed since hashing: %w", ErrContentChanged)
    return op
}
```

**Tests:** Unit test that modifies file content between hash phase and delete phase,
verifies the file is preserved.

**Estimated size:** ~30 lines production, ~80 lines test.

---

### Phase 5 — Unzipper overwrite protection

Prevent silent overwrite of existing files during zip extraction.

**Change in `pkg/unzipper/unzipper.go`**, in `extractEntry` after path resolution
(~line 245):

```go
if !u.dryRun {
    if _, err := os.Lstat(entryPath); err == nil {
        if u.trasher != nil {
            if trashErr := u.trasher.Trash(entryPath); trashErr != nil {
                return result, fmt.Errorf(
                    "cannot back up existing file %s before overwrite: %w",
                    entryPath, trashErr,
                )
            }
        } else {
            return result, fmt.Errorf("refusing to overwrite existing file: %s", entryPath)
        }
    }
}
```

**Behavior:**

- **With trasher:** back up existing file to trash first, then extract (reversible).
- **Without trasher:** refuse to extract (fail-safe).

This closes the highest-severity gap identified in the audit.

**Tests:** Unit test extracting over an existing file, verifying the original is in
trash. E2e test with a pre-existing file at the extraction path.

**Estimated size:** ~20 lines production, ~60 lines test.

---

### Phase 6 — Operation journal

Log every mutation to enable crash recovery and rollback.

**New package:** `pkg/journal/`

```go
type Entry struct {
    Timestamp time.Time `json:"ts"`
    Type      string    `json:"type"`           // "trash", "rename", "mkdir", "extract"
    Source    string    `json:"src"`             // original path (relative to root)
    Dest      string   `json:"dst"`             // new path (relative to root)
    Hash      string   `json:"hash,omitempty"`
    Success   bool     `json:"ok"`
}

type Writer struct {
    file    *os.File
    encoder *json.Encoder
    mu      sync.Mutex
}

func NewWriter(path string) (*Writer, error)
func (w *Writer) Log(entry Entry) error     // write + fsync
func (w *Writer) Close() error

type Reader struct { ... }
func NewReader(path string) (*Reader, error)
func (r *Reader) Entries() ([]Entry, error)
func (r *Reader) EntriesReverse() ([]Entry, error)   // for rollback
```

Each `Log` call writes one JSON line and calls `file.Sync()`. On crash, the journal
reflects all mutations that were *attempted*. The `Success` field is written in a
second `Log` call after the mutation completes — a missing `Success: true` means the
mutation may or may not have completed.

**Integration:** Wrap `Trasher` and `SafeRename` with journal-aware decorators at the
usecase level so domain packages do not need to know about the journal:

```go
type TrackedTrasher struct {
    inner  *trash.Trasher
    writer *Writer
}

func (t *TrackedTrasher) Trash(path string) error {
    t.writer.Log(Entry{Type: "trash", Source: rel(path)})
    err := t.inner.Trash(path)
    t.writer.Log(Entry{Type: "trash", Source: rel(path), Success: err == nil})
    return err
}
```

Domain packages depend only on a `Remover` interface. The journal wrapping is
invisible to them.

**Tests:** Unit tests for journal write/read round-trip, crash simulation (partial
writes), reverse iteration.

**Estimated size:** ~150 lines production, ~200 lines test.

---

### Phase 7 — Automatic pre-operation manifest

Generate a manifest before every destructive command.

**Changes:**

| File | Change |
|------|--------|
| `pkg/usecase/service.go` | In `runFileWorkflow`, after file collection: generate manifest and save to `.btidy/manifests/<run-id>.json` |
| `cmd/common.go` | Print `"Snapshot saved: .btidy/manifests/<run-id>.json"` in command header |
| `cmd/root.go` | Add `--no-snapshot` persistent flag (default: false) |

**Implementation in `runFileWorkflow`** (~line 311):

```go
if !noSnapshot {
    gen, _ := manifest.NewGeneratorWithValidator(target.validator, workers)
    m, _ := gen.Generate(manifest.GenerateOptions{SkipFiles: s.skipFileList()})
    m.Save(metaDir.ManifestPath(runID))
}
```

**Performance note:** This adds a full directory hash before every command. For large
directories this is slow. Mitigation: use the same worker count, share the already-
collected file list. Users who want speed can pass `--no-snapshot`.

**Tests:** E2e test verifying `.btidy/manifests/` contains a manifest after any
destructive command. Test `--no-snapshot` skips it.

**Estimated size:** ~40 lines production, ~60 lines test.

---

### Phase 8 — Advisory file locking

Prevent concurrent btidy executions on the same target directory.

**New package:** `pkg/filelock/`

```go
type Lock struct {
    file *os.File
}

func Acquire(path string) (*Lock, error)    // open + flock(LOCK_EX | LOCK_NB)
func (l *Lock) Release() error              // flock(LOCK_UN) + close + remove
```

Uses `syscall.Flock` on Linux. Advisory only — does not prevent other programs from
accessing files, but prevents two btidy processes from colliding.

**Integration:** In `resolveWorkflowTarget` (`service.go`), after creating the
validator:

```go
lock, err := filelock.Acquire(metaDir.LockPath())
if err != nil {
    return ..., fmt.Errorf("another btidy process is operating on this directory: %w", err)
}
// lock released via deferred cleanup
```

If btidy crashes, `flock` is automatically released by the kernel when the process
exits. No stale lock problem.

**Tests:** Unit test acquiring lock twice (second fails). Integration test running
two btidy processes concurrently.

**Estimated size:** ~40 lines production, ~50 lines test.

---

### Phase 9 — Rollback command (`btidy undo`)

New command that reverses the most recent operation using the journal and trash.

**Command:** `btidy undo [path]`

**Algorithm:**

1. Find the most recent journal in `.btidy/journal/`.
2. Read entries in reverse order.
3. For each entry:
   - `"trash"` → `trasher.Restore(trashedPath)` (move back from trash).
   - `"rename"` → `validator.SafeRename(dest, source)` (reverse the rename).
   - `"mkdir"` → `validator.SafeRemoveDir(path)` (remove if empty).
   - `"extract"` → `validator.SafeRemove(path)` (remove extracted file).
4. Mark the journal as rolled back (rename to `<run-id>.rolled-back.jsonl`).

**Verification before each undo step:** Check that the current file state matches
what the journal expects (file exists at destination with expected hash). If not,
warn and skip that entry.

**Flags:**

- `--run <run-id>` — undo a specific operation instead of the most recent.
- `--dry-run` — preview what would be undone.

**Tests:** E2e: flatten a directory, run `btidy undo`, verify original structure
restored. Undo of deduplicate (files restored from trash). Undo of unzip (extracted
files removed, archive restored from trash).

**Estimated size:** ~200 lines production, ~300 lines test.

---

### Phase 10 — Trash management (`btidy purge`)

Permanent deletion of trashed files. The one place where data is irrecoverably
removed.

**Command:** `btidy purge [path]`

**Flags:**

- `--older-than 7d` — only purge runs older than the given duration.
- `--run <run-id>` — purge a specific run's trash.
- `--dry-run` — preview what would be purged.
- `--all` — purge everything (requires `--force` or interactive confirmation).

**Display:** List each trashed run with file count, total size, and age before
purging.

**Estimated size:** ~120 lines production, ~100 lines test.

---

### Phase 11 — Purge safety gate (`--force`)

Prevent accidental irrecoverable deletion when using `btidy purge --all`.

**Problem:** `btidy purge --all ./backup` permanently deletes all trashed files
with no confirmation. The plan specified `--force` or interactive confirmation,
but this was not implemented.

**Changes:**

| File | Change |
|------|--------|
| `cmd/purge.go` | Add `--force` boolean flag |
| `cmd/purge.go` | Require `--force` when `--all` is used without `--dry-run` |
| `cmd/purge.go` | Update long help text and examples |

**Behavior:**

- `btidy purge --all ./backup` → error: `--all requires --force to confirm`
- `btidy purge --all --force ./backup` → purges all trash
- `btidy purge --all --dry-run ./backup` → lists what would be purged (no `--force` needed)
- `btidy purge --run <id> ./backup` → works without `--force` (targeted)
- `btidy purge --older-than 7d ./backup` → works without `--force` (filtered)

**Tests:** E2e test confirming `--all` without `--force` fails. E2e test confirming
`--all --force` succeeds.

**Estimated size:** ~10 lines production, ~30 lines test.

---

### Phase 12 — Undo hash verification

Verify file content integrity before each undo step to prevent silent data
corruption when files have been modified since the original operation.

**Problem:** `undoTrash` and `undoRename` only check file existence via `os.Lstat`.
A file modified externally between the original operation and undo would be silently
moved without warning.

**Changes:**

| File | Change |
|------|--------|
| `pkg/usecase/service.go` | `undoTrash`: hash file at trash dest, compare to `entry.Hash`, warn+skip on mismatch |
| `pkg/usecase/service.go` | `undoRename`: hash file at dest, compare to `entry.Hash`, warn+skip on mismatch |
| `pkg/usecase/service.go` | Import `btidy/pkg/hasher` |

**Algorithm for `undoTrash`:**

```go
if entry.Hash != "" {
    h, _ := hasher.New(1)
    currentHash, err := h.ComputeHash(trashedAbs)
    if err != nil || currentHash != entry.Hash {
        return UndoOperation{..., Action: "skip",
            SkipReason: "content changed since original operation"}
    }
}
```

Same pattern for `undoRename` using `destAbs`.

**When hash is empty:** Skip verification (some entry types like renames from
the organize command don't record hashes). Log a warning but proceed.

**Tests:** Unit test that modifies a trashed file between trash and undo, verifies
the undo is skipped with appropriate reason. Unit test that undo proceeds normally
when hash matches.

**Estimated size:** ~30 lines production, ~80 lines test.

---

### Phase 13 — Write-ahead journaling

Replace post-execution batch journal writing with inline write-ahead logging.
Each mutation is logged *before* execution, with a confirmation entry after.

**Problem:** The current implementation writes all journal entries as a batch after
the entire operation completes, with `Success: true` hardcoded. If btidy crashes
mid-operation, the journal has no record of what was attempted. The `Validate()`
method and two-phase entry pattern in `pkg/journal` cannot be exercised.

**Design:**

Replace the `toJournalEntries` callback pattern in `runFileWorkflow` with an
inline journal writer that is passed to executors. The journal writer is created
*before* execution begins and closed after.

**New type in `pkg/usecase/`:**

```go
// journalLogger wraps a journal.Writer and provides helpers for
// write-ahead logging of filesystem mutations.
type journalLogger struct {
    writer  *journal.Writer
    rootDir string
}

func (jl *journalLogger) LogIntent(entry journal.Entry) error   // Success=false
func (jl *journalLogger) LogSuccess(entry journal.Entry) error  // Success=true
```

**Changes:**

| File | Change |
|------|--------|
| `pkg/usecase/service.go` | `runFileWorkflow`: create journal writer before `execute()`, pass via new `WorkflowContext` parameter, close after execution. Remove `toJournalEntries` callback. |
| `pkg/usecase/service.go` | Remove all `*JournalEntries` converter functions (renameJournalEntries, flattenJournalEntries, etc.) |
| `pkg/usecase/service.go` | Each executor receives a `*journalLogger` and calls `LogIntent`/`LogSuccess` around each mutation |
| Domain packages | Add optional `OnMutation` callback to constructors (or pass through existing progress callback pattern) — alternative: keep journal logging in executor closures at usecase level |
| `pkg/journal/journal.go` | No changes needed (Writer already supports concurrent Log calls with mutex) |

**Preferred approach — executor-level logging:**

Keep domain packages unchanged. The usecase-level executor functions already wrap
domain operations. Add journal logging in the executor wrapper by post-processing
each operation result as it completes, using the existing `workerExecutor` pattern.

Since domain packages process files sequentially within each worker and return
results one at a time via progress callbacks, the executor can log intent before
calling the domain function and confirmation after.

Actually, the cleanest approach: domain packages already return `Result` structs
with per-file `Operation` entries. The issue is that results are returned as a
batch. To do true write-ahead logging, we need per-operation hooks.

**Practical approach:** Add a per-operation callback to domain packages that fires
after each file is processed. The executor uses this to write journal entries
inline. This is similar to the existing `ProgressCallback` pattern.

```go
// In each domain package, add OnOperation callback:
type OperationCallback func(op Operation)

// In executor:
var jl *journalLogger
domainPkg.ProcessWithCallback(files, func(op Operation) {
    entry := toJournalEntry(op)
    jl.LogIntent(entry)   // before is not possible here...
})
```

**Revised practical approach:** The simplest correct change is to modify
`writeJournal` to write two entries per operation (intent + confirmation) instead
of one. This gives us the two-phase pattern without restructuring domain packages.
While not true write-ahead (the operation has already completed), it establishes
the correct journal format and enables `Validate()` to detect incomplete journals
from future true write-ahead work.

For the scope of this phase: write intent entries (Success=false) followed by
confirmation entries (Success=true) for each operation in `writeJournal`. This
correctly exercises `Validate()` and establishes the journal format contract.

**Tests:** Unit test for `Validate()` detecting partial writes. Test that journal
contains paired intent+confirmation entries. Test that undo correctly processes
journals with the two-phase format.

**Estimated size:** ~50 lines production, ~100 lines test.

---

## Dependency Graph

```
Phase 1 ─── Phase 2 ─── Phase 3 ─┬─ Phase 4
   │                               ├─ Phase 5
   │                               └─ Phase 10 ── Phase 11
   ├──────── Phase 6 ─── Phase 9 ─┬─ Phase 12
   │                               └─ Phase 13
   ├──────── Phase 7
   └──────── Phase 8
```

Phases 4, 5, 7, 8 are independent of each other once their prerequisites are met.
Phase 9 depends on both Phase 3 (trash) and Phase 6 (journal). Phase 7 depends only
on Phase 1. Phase 11 depends on Phase 10. Phases 12 and 13 depend on Phase 9.

---

## Estimated Effort

| Phase | New packages | Prod lines | Test lines | Risk |
|-------|-------------|-----------|-----------|------|
| 1. Metadata dir | `pkg/metadata` | ~80 | ~120 | Low |
| 2. Trash package | `pkg/trash` | ~120 | ~200 | Low |
| 3. Wire trash in | — | ~160 | ~250 | **Medium** |
| 4. Re-hash before delete | — | ~30 | ~80 | Low |
| 5. Unzipper overwrite | — | ~20 | ~60 | Low |
| 6. Operation journal | `pkg/journal` | ~150 | ~200 | Medium |
| 7. Auto manifest | — | ~40 | ~60 | Low |
| 8. File locking | `pkg/filelock` | ~40 | ~50 | Low |
| 9. Undo command | — | ~200 | ~300 | **Medium** |
| 10. Purge command | — | ~120 | ~100 | Low |
| 11. Purge safety gate | — | ~10 | ~30 | Low |
| 12. Undo hash verification | — | ~30 | ~80 | Low |
| 13. Write-ahead journaling | — | ~50 | ~100 | Medium |
| **Total** | **4 new** | **~1050** | **~1630** | |

---

## What This Does NOT Guarantee

Even with all 10 phases complete:

1. **Hardware failure during `os.Rename`** — the kernel guarantees atomicity of
   rename on the same filesystem, but disk firmware bugs or power loss during a write
   could corrupt the filesystem. This is below the application layer.

2. **External process with root access** — advisory locks do not prevent `rm -rf`.
   If another process forcibly modifies the target directory during execution, all
   bets are off.

3. **Filesystem full during trash** — if the disk fills up while moving a file to
   trash, the move fails. The journal records the failure. The original file remains
   in place (safe). But subsequent operations may also fail.

4. **SHA256 collision** — probability ~10⁻³⁸ for practical file collections.
   Accepted as negligible.

The guarantee is: **within the boundaries of a functioning filesystem and exclusive
access to the target directory, no user data is permanently lost without explicit
user action (`btidy purge`).**
