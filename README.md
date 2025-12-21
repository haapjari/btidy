# File Organizer

- Just a simple tool to organize my backup files by flattening direct structures, renaming files with timestamps and removing duplicates.

## Problem

When you accumulate backup copies over time, you end up with:

- Same files scattered across deeply nested directories
- Inconsistent naming (spaces, special characters, mixed case)
- True duplicates wasting storage space

## Solution

This tool works in two phases:

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

### Phase 2: Flatten (coming soon)

Moves all files to root directory and removes true duplicates (same size + same modification time).

## Examples

```bash
# See what would be renamed (dry run)
./file-organizer --dry-run --phase rename /path/to/2018-backup

# Example output:
#   RENAME: /backup/docs/CV (Final).docx
#       TO: /backup/docs/2018-03-15_cv_final.docx
#   RENAME: /backup/photos/Työpöytä.jpg  
#       TO: /backup/photos/2018-06-20_tyopoyta.jpg
#   SKIP:   /backup/2018-01-01_already-named.pdf (name unchanged)

# Actually rename the files
./file-organizer --phase rename /path/to/2018-backup

# View operation log
cat /path/to/2018-backup/organizer.log
```
## License

MIT
