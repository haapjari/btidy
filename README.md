# File Organizer

A simple tool to organize backup files by flattening directory structures, renaming files with timestamps, and removing duplicates.

## Problem

When you accumulate backup copies over time, you end up with:

- Same files scattered across deeply nested directories
- Inconsistent naming (spaces, special characters, mixed case)
- True duplicates wasting storage space

## Solution

This tool works in three phases:

### Phase 1: Rename

Renames all files **in place** with consistent naming:

```
Before: My Document (Final).pdf
After:  2018-06-15_my_document_final.pdf
```

- Adds modification date prefix (`YYYY-MM-DD_`)
- Converts to lowercase
- Replaces spaces with underscores
- Converts Finnish characters: `ä→a`, `ö→o`, `å→a`
- Removes special characters

### Phase 2: Flatten

Moves all files to root directory and removes duplicates by content hash (SHA256).

```
Before:
  backup/
    Documents/Work/report.pdf
    Photos/Vacation/photo.jpg

After:
  backup/
    report.pdf
    photo.jpg
```

### Phase 3: Duplicate

Finds and removes duplicate files using **content hashing** (SHA256):

- Groups files by size (fast pre-filter)
- Computes SHA256 hash to identify true duplicates
- Uses partial hashing for large files (performance optimization)
- Keeps one copy (alphabetically first), removes the rest

This is the most reliable approach - files are only considered duplicates if their content is byte-for-byte identical.

## Usage

```bash
# Always use --dry-run first to preview changes!

# Phase 1: Rename files with timestamps
./file-organizer rename --dry-run /path/to/backup
./file-organizer rename /path/to/backup

# Phase 2: Flatten directory structure
./file-organizer flatten --dry-run /path/to/backup
./file-organizer flatten /path/to/backup

# Phase 3: Remove duplicate files by content
./file-organizer duplicate --dry-run /path/to/backup
./file-organizer duplicate /path/to/backup

# Verbose output
./file-organizer duplicate -v --dry-run /path/to/backup

# Control parallel hashing workers
./file-organizer manifest --workers 8 /path/to/backup -o manifest.json
./file-organizer flatten --workers 8 /path/to/backup
./file-organizer duplicate --workers 8 /path/to/backup
```

## Typical Workflow

```bash
# 1. First rename files for consistent naming
./file-organizer rename /path/to/backup/2018

# 2. Then flatten the directory structure
./file-organizer flatten /path/to/backup/2018

# 3. Finally remove any remaining duplicates
./file-organizer duplicate /path/to/backup/2018
```

## Safety

- **Dry-run by default mindset**: Always use `--dry-run` first to preview changes
- **Path containment**: The tool will NEVER modify files outside the specified directory
- **Content verification**: Duplicates are verified by SHA256 hash, not just filename

## Building

```bash
make build        # Build the binary
make test         # Run tests
make test-race    # Run tests with race detector
make lint         # Run linter
make check        # Run all checks (fmt, vet, lint, test-race)
```

## License

MIT
