# btidy

Tool to tame messy backup directories, recursively extract archives, deduplicate files by content hash, and organize what's left.

## Overview

- Unzip extracts .zip files recursively in the directory, in place and removes archives on success.
- Rename applies a timestamped, sanitized filename in the same directory.
- Flatten moves files to root and removes content duplicates safely.
- Organize groups files into subdirectories by file extension.
- Duplicate removes duplicate content by hash across the tree.
- Manifest writes a cryptographic inventory for before/after verification.

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

- All reads and mutations are contained within the target directory.
- Symlinks that resolve outside the target are rejected.
- Manifest output path must resolve inside the target directory.

## Tests

```bash
make test
make test-e2e
./scripts/e2e.sh
```
