# btidy

Tool to tame messy backup directories, recursively extract archives, deduplicate files by content hash, and organize what's left. Every destructive operation is reversible through soft-delete, journaling, and undo.

## The Problem

Years of backups tend to accumulate into a mess. You back up a folder, then later back up the same folder again — this time the previous backup archive is inside it. Repeat a few times and you end up with something like:

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

Archives within archives within archives, four or five levels deep, full of duplicate files with inconsistent names. Manually untangling this is tedious and error-prone.

btidy solves this in one pipeline: recursively extract every archive (no matter how deeply nested), deduplicate by content hash, normalize filenames, and organize by type. Every step is reversible.

## Overview

- **Unzip** extracts .zip files recursively in the directory, in place and removes archives on success.
- **Rename** applies a timestamped, sanitized filename in the same directory.
- **Flatten** moves files to root and removes content duplicates safely.
- **Organize** groups files into subdirectories by file extension.
- **Duplicate** removes duplicate content by hash across the tree.
- **Manifest** writes a cryptographic inventory for before/after verification.
- **Undo** reverses the most recent operation using its journal (restores trashed files, reverses renames).
- **Purge** permanently deletes trashed files from `.btidy/trash/`. This is the only irrecoverable command.

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

btidy is designed around a zero data loss guarantee:

- **Path containment**: All reads and mutations are contained within the target directory. Symlinks that resolve outside the target are rejected.
- **Soft-delete via trash**: Files are never permanently deleted. They are moved to `.btidy/trash/<run-id>/` preserving relative paths. Only `purge --force` permanently deletes.
- **Operation journal**: Every mutation is logged to `.btidy/journal/<run-id>.jsonl` with write-ahead entries (intent written before action, confirmation after). Enables undo and crash detection.
- **Pre-operation manifest snapshots**: An automatic manifest snapshot is saved to `.btidy/manifests/` before each non-dry-run mutating operation (disable with `--no-snapshot`).
- **Advisory file locking**: `.btidy/lock` prevents concurrent btidy processes on the same directory.
- **Pre-delete content verification**: Files are re-hashed before deletion to verify content hasn't changed since the operation started.
- **Unzipper overwrite protection**: Extraction skips files that already exist at the target path.
- **Undo with hash verification**: `btidy undo` verifies content hashes before restoring trashed files, skipping any that have been modified.

## `.btidy/` Metadata Directory

All btidy metadata lives in a `.btidy/` directory inside the target:

```
.btidy/
  lock                                  # Advisory file lock
  trash/<run-id>/...                    # Soft-deleted files (preserving relative paths)
  manifests/<run-id>.json               # Pre-operation manifest snapshots
  journal/<run-id>.jsonl                # Operation journals (write-ahead)
  journal/<run-id>.rolled-back.jsonl    # Journals after successful undo
```

The `.btidy/` directory and its contents are automatically excluded from all btidy operations (collection, hashing, organizing, etc.).

## Tests

```bash
make test
make test-e2e
./scripts/e2e.sh
```

# License

MIT
