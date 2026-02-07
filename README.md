# btidy

Organize backup folders by renaming, flattening, and removing true duplicates.

## What it does

- Rename: consistent timestamped names (in place)
- Flatten: move files to root and drop content-duplicates
- Duplicate: remove content-duplicates anywhere under root
- Manifest: generate a SHA256 inventory JSON

## Examples

```bash
# Build
make build

# Rename (preview, then apply)
./btidy rename --dry-run /path/to/backup
./btidy rename /path/to/backup

# Flatten (preview, then apply)
./btidy flatten --dry-run /path/to/backup
./btidy flatten /path/to/backup

# Duplicate (preview, then apply)
./btidy duplicate --dry-run /path/to/backup
./btidy duplicate /path/to/backup

# Manifest
./btidy manifest /path/to/backup -o manifest.json
```

## Typical workflow

```bash
./btidy rename /path/to/backup/2018
./btidy flatten /path/to/backup/2018
./btidy duplicate /path/to/backup/2018
```

## Naming rules (rename)

```
Before: My Document (Final).pdf
After:  2018-06-15_my_document_final.pdf
```

## Safety notes

- Always try `--dry-run` first.
- All mutations are root-contained (safepath validation).
- Duplicates are content-verified with SHA256.
