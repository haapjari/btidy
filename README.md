# btidy

CLI to organize backup folders: unzip archives, rename files, flatten
directories, remove duplicates, and verify with manifests.

## Overview

- Unzip extracts .zip files in place (recursive) and removes archives on success.
- Rename applies a timestamped, sanitized filename in the same directory.
- Flatten moves files to root and removes content duplicates safely.
- Duplicate removes duplicate content by hash across the tree.
- Manifest writes a cryptographic inventory for before/after verification.

## Examples

```bash
# Build
make build

# Typical workflow
./btidy unzip /path/to/backup/2018
./btidy rename /path/to/backup/2018
./btidy flatten /path/to/backup/2018
./btidy duplicate /path/to/backup/2018

# Unzip (preview, then apply)
./btidy unzip --dry-run /path/to/backup
./btidy unzip /path/to/backup

# Rename (preview, then apply)
./btidy rename --dry-run /path/to/backup
./btidy rename /path/to/backup

# Flatten (preview, then apply)
./btidy flatten --dry-run /path/to/backup
./btidy flatten /path/to/backup

# Duplicate (preview, then apply)
./btidy duplicate --dry-run /path/to/backup
./btidy duplicate /path/to/backup

# Manifest (before/after verification)
./btidy manifest /path/to/backup -o before.json
./btidy unzip /path/to/backup
./btidy rename /path/to/backup
./btidy flatten /path/to/backup
./btidy duplicate /path/to/backup
./btidy manifest /path/to/backup -o after.json

# Manifest output inside target directory
./btidy manifest /path/to/backup -o manifests/manifest.json
# writes to /path/to/backup/manifests/manifest.json

# Rename example
# Before: My Document (Final).pdf
# After:  2018-06-15_my_document_final.pdf
```

## Safety

- All reads and mutations are contained within the target directory.
- Symlinks that resolve outside the target are rejected.
- Manifest output path must resolve inside the target directory.

## Tests

```bash
make test
make test-e2e
./scripts/e2e.sh
```
