# Architecture

Short map of how btidy flows data and keeps mutations safe.

## Pipeline

```
Collect -> Unzip -> Rename -> Flatten -> Organize -> Duplicate
            \-> Manifest (before/after)
            \-> Undo (reverse any operation via journal)
            \-> Purge (permanently remove trash)
```

All mutating commands (unzip, rename, flatten, organize, duplicate) share
cross-cutting safety behaviors: soft-delete via trash, operation journaling,
pre-operation manifest snapshots, advisory file locking, and pre-delete
content verification.

## Phases

- **Unzip**: find .zip, safe extract (skip existing files), recurse, trash archive on success
- **Rename**: sanitize + timestamp names, safe rename in place
- **Flatten**: hash, move to root, trash duplicates, delete empty dirs
- **Organize**: group files into subdirectories by extension
- **Duplicate**: size group, hash, trash duplicate content
- **Manifest**: hash all files, save JSON inventory
- **Undo**: read journal, reverse operations (restore from trash, reverse renames), verify hashes
- **Purge**: permanently delete trashed files (the only irrecoverable operation)

## Data model

- `collector.FileInfo`: `Path`, `Dir`, `Name`, `Size`, `ModTime`
- `journal.Entry`: `Type` (intent/confirm), `Action` (move/remove/extract/mkdir), `Source`, `Dest`, `Hash`, `Timestamp`
- `trash` items: original files moved to `.btidy/trash/<run-id>/` preserving relative paths
- `manifest` snapshot: JSON inventory with SHA-256 hashes

## `.btidy/` metadata directory

```
.btidy/
  lock                                  # Advisory file lock
  trash/<run-id>/...                    # Soft-deleted files
  manifests/<run-id>.json               # Pre-operation manifest snapshots
  journal/<run-id>.jsonl                # Operation journals (write-ahead)
  journal/<run-id>.rolled-back.jsonl    # Journals after successful undo
```

## Safety invariants

- All mutations go through `pkg/safepath.Validator`.
- Paths are validated before read/write/remove.
- Dry-run computes operations only.
- **Soft-delete**: Files are trashed, never permanently deleted (except by `purge --force`).
- **Write-ahead journal**: Intent entry is written before the action; confirmation entry after. Incomplete intent entries indicate a crash.
- **Advisory file locking**: `.btidy/lock` prevents concurrent btidy processes.
- **Pre-delete verification**: Files are re-hashed before deletion to confirm content hasn't changed.
- **Undo hash verification**: Trashed files are verified by hash before restoring.

## Packages

### CLI layer

- `cmd/` — Cobra commands: `unzip`, `rename`, `flatten`, `organize`, `duplicate`, `manifest`, `undo`, `purge`. Shared helpers in `common.go`.

### Orchestration

- `pkg/usecase/` — `Service` coordinates workflows. Uses Go generics (`runFileWorkflow`, `runCheckedExecution`, `workerExecutor`) to reduce boilerplate.

### Domain packages

- `collector` — Walks directory, returns `[]FileInfo`
- `hasher` — SHA-256 hashing with parallel workers
- `sanitizer` — Filename sanitization rules
- `renamer` — Timestamp-prefixed rename
- `flattener` — Move files to root, deduplicate by hash
- `deduplicator` — Size-group then hash-compare deletion
- `organizer` — Group files into subdirectories by extension
- `unzipper` — ZIP extraction with recursion and overwrite protection
- `manifest` — JSON file inventory with cryptographic hashes
- `safepath` — **Security-critical**: validates all paths stay within target directory
- `trash` — Soft-delete: move files to `.btidy/trash/<run-id>/`, restore on undo
- `journal` — Write-ahead operation journal (JSONL), validation, rollback support
- `metadata` — `.btidy/` directory management, run ID generation
- `filelock` — Advisory file locking via `.btidy/lock`

## Extension points

- Use `collector` for discovery, `hasher` for identity, `safepath` for safety.
- Use `journal` for operation tracking and undo capability.
- Use `trash` for safe file removal with restore support.
