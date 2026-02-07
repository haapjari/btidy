# btidy

Simple application that I use to organize backup folders by renaming, flattening and removing duplicates.

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
./btidy manifest /path/to/backup -o manifests/manifest.json
# writes to /path/to/backup/manifests/manifest.json
```

## Typical Workflow

```bash
./btidy rename /path/to/backup/2018
./btidy flatten /path/to/backup/2018
./btidy duplicate /path/to/backup/2018
```

## Safety

- All file reads and mutations are contained within the target directory.
- Symlink paths that resolve outside the target are rejected.
- `manifest -o` output path must resolve inside the target directory.
- Relative `manifest -o` paths are resolved from the target directory root.

## Tests

```bash
# Unit tests
make test

# End-to-end CLI tests (builds and runs btidy)
make test-e2e
# or
./scripts/e2e.sh
```

## Naming Convention

```
Before: My Document (Final).pdf
After:  2018-06-15_my_document_final.pdf
```
