# Architecture

Concise map of how btidy flows data and keeps mutations safe.

## Big picture

```
           +----------------+
CLI (cmd/) | Cobra commands |
           +--------+-------+
                    |
                    v
           +----------------+
           | pkg/usecase    |
           | workflow svc   |
           +--+--+--+--+-----+
              |  |  |  |
              |  |  |  +------------------+
              |  |  |                     |
              v  v  v                     v
          renamer flattener           deduplicator
             |       |                     |
             v       v                     v
         sanitizer  hasher              hasher
             |       |                     |
             +---+---+---------------------+
                 |
                 v
              safepath
```

## Data model (shared)

- `collector.FileInfo`: `Path`, `Dir`, `Name`, `Size`, `ModTime`
- Used by rename, flatten, duplicate to avoid repeated stats.

## Phase pipeline

```
Collect -> Rename -> Flatten -> Duplicate
              \-> Manifest (before/after)
```

## Phase flows (short)

```
Rename:    Collect -> Sanitize name -> SafeRename
Flatten:   Collect -> Hash (SHA256) -> SafeRename or SafeRemove
Duplicate: Collect -> Size group -> Partial hash -> Full hash -> SafeRemove
Manifest:  Collect -> Hash -> JSON save
```

## Safety invariants

- All mutations go through `pkg/safepath.Validator`.
- Source and destination paths are validated before rename/remove.
- Dry-run computes operations only.

## Examples

```
Rename input:
  My Document (Final).pdf
Rename output:
  2018-06-15_my_document_final.pdf
```

```
Flatten before:
  backup/Photos/Vacation/photo.jpg
  backup/Documents/Work/report.pdf
Flatten after:
  backup/photo.jpg
  backup/report.pdf
```

```
Duplicate keep rule:
  /backup/a/report.pdf
  /backup/z/report_copy.pdf
Result:
  keep /backup/a/report.pdf (path sort)
  delete /backup/z/report_copy.pdf
```

```
CLI examples:
  btidy rename --dry-run /backup
  btidy flatten /backup
  btidy duplicate --workers 8 /backup
  btidy manifest /backup -o before.json
```

## Extension points

- Use `collector` for discovery.
- Use `hasher` when content identity matters.
- Use `safepath` for all mutations.
