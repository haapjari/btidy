# btidy

- Tool to recursively detect and remove duplicate files in a given path.

## The Problem

- I had years of backups, that accumulated into huge blob of data. Blob included archives and archives, and I had multiple instances of same piece of data multiple times.

```
backup-2024/
  photos-2019.zip
  documents.zip         <- contains documents-2018.zip inside
  old-backups.zip       <- contains photos-2019.zip + another old-backups.zip
    old-backups.zip     <- contains yet another layer
      old-backups.zip   <- and another...
        photos-2017.zip
        notes.zip
```

## Overview

- Unzip: extracts .zip files recursively in the directory, in place and removes archives on success.
- Rename: applies a timestamped, sanitized filename in the same directory.
- Flatten: moves files to root and removes content duplicates safely.
- Organize: groups files into subdirectories by file extension.
- Duplicate: removes duplicate content by hash across the tree.
- Manifest: writes a cryptographic inventory for before and after verification.
- Undo: reverses the most recent operation using its journal (restores trashed files, reverses renames).
- Purge: permanently deletes trashed files from `.btidy/trash/`. This is the only irrecoverable command.

## Examples

```bash
# Build
make build

# typical workflow
./btidy unzip /path/to/backup/2018
./btidy rename /path/to/backup/2018
./btidy flatten /path/to/backup/2018
./btidy organize /path/to/backup/2018
./btidy duplicate /path/to/backup/2018
./btidy organize /path/to/backup

# unzip (preview, then apply)
./btidy unzip --dry-run /path/to/backup
./btidy unzip /path/to/backup

# rename (preview, then apply)
./btidy rename --dry-run /path/to/backup
./btidy rename /path/to/backup

# flatten (preview, then apply)
./btidy flatten --dry-run /path/to/backup
./btidy flatten /path/to/backup

# organize by extension (preview, then apply)
./btidy organize --dry-run /path/to/backup
./btidy organize /path/to/backup

# duplicate (preview, then apply)
./btidy duplicate --dry-run /path/to/backup
./btidy duplicate /path/to/backup

# manifest (before and after verification)
./btidy manifest /path/to/backup -o before.json
./btidy unzip /path/to/backup
./btidy rename /path/to/backup
./btidy flatten /path/to/backup
./btidy organize /path/to/backup
./btidy duplicate /path/to/backup
./btidy manifest /path/to/backup -o after.json

# manifest output inside target directory
./btidy manifest /path/to/backup -o manifests/manifest.json
# writes to /path/to/backup/manifests/manifest.json

# undo the last operation
./btidy undo /path/to/backup
./btidy undo --dry-run /path/to/backup       # preview what would be undone
./btidy undo --run <run-id> /path/to/backup   # undo a specific run

# purge trashed files
./btidy purge --older-than 30d /path/to/backup   # purge trash older than 30 days
./btidy purge --run <run-id> /path/to/backup      # purge trash from a specific run
./btidy purge --all --force /path/to/backup        # purge ALL trash (requires --force)
./btidy purge --dry-run /path/to/backup            # preview what would be purged

# skip pre-operation snapshot
./btidy flatten --no-snapshot /path/to/backup

# rename example
# Before: My Document (Final).pdf
# After:  2018-06-15_my_document_final.pdf

# organize example
#
# Before:            After:
#   report.pdf         pdf/report.pdf
#   photo.jpg          jpg/photo.jpg
#   notes.txt          txt/notes.txt
#   Makefile           other/Makefile
#   .gitignore         other/.gitignore
#   data.tar.gz        gz/data.tar.gz
```

## Safety

- Path Containment: All reads and mutations are contained within the target directory. Symlinks that resolve outside the target are rejected.
- Soft-Delete: Files are never permanently deleted. They are moved to `.btidy/trash/<run-id>/` preserving relative paths. Only `purge --force` permanently deletes.
- Operation Journal: Every mutation is logged to `.btidy/journal/<run-id>.jsonl` with write-ahead entries (intent written before action, confirmation after). Enables undo and crash detection.
- Pre-Operation Manifest Snapshots: An automatic manifest snapshot is saved to `.btidy/manifests/` before each non-dry-run mutating operation (disable with `--no-snapshot`).
- Advisory File Locking: `.btidy/lock` prevents concurrent btidy processes on the same directory.
- Pre-delete content verification: Files are re-hashed before deletion to verify content hasn't changed since the operation started.
- Unzipper Overwrite Safety: Existing target files are moved to trash before extraction overwrites them.
- Undo, with Hash Verification: `btidy undo` verifies content hashes before restoring trashed files, skipping any that have been modified.

## `.btidy/` Metadata Directory

```
.btidy/
  lock                                  # Advisory file lock
  trash/<run-id>/...                    # Soft-deleted files (preserving relative paths)
  manifests/<run-id>.json               # Pre-operation manifest snapshots
  journal/<run-id>.jsonl                # Operation journals (write-ahead)
  journal/<run-id>.rolled-back.jsonl    # Journals after successful undo
```

## Tests

```bash
make test
make test-e2e
./scripts/e2e.sh
```

## Third-Party

- deflate64 ZIP decompression support uses zlib contrib/infback9 from https://github.com/madler/zlib
- Upstream Tag: `v1.3.1`
- LICENSE: zlib License
- Notices: `THIRD_PARTY_NOTICES.md`
- Integrity Check: `make verify-third-party`

## CGO Toolchain Requirements

Deflate64 support is built with CGO in all build modes.

- Local build/test needs a C compiler (default: `gcc`) and standard C headers.
- Cross-platform release build (`make release-build`) requires:
  - `x86_64-linux-gnu-gcc` (linux/amd64)
  - `aarch64-linux-gnu-gcc` (linux/arm64)
  - `o64-clang` (darwin/amd64)
  - `oa64-clang` (darwin/arm64)
  - `x86_64-w64-mingw32-gcc` (windows/amd64)
- Quick Checks:
  - `make check-cgo`
  - `make check-release-toolchains`

# LICENSE

MIT
