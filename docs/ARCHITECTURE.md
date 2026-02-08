# Architecture

Short map of how btidy flows data and keeps mutations safe.

## Pipeline

Collect -> Unzip -> Rename -> Flatten -> Duplicate
          \-> Manifest (before/after)

## Phases

- Unzip: find .zip, safe extract, recurse, remove archive on success
- Rename: sanitize + timestamp names, safe rename in place
- Flatten: hash, move to root, remove duplicates, delete empty dirs
- Duplicate: size group, hash, delete duplicate content
- Manifest: hash all files, save JSON inventory

## Data model

- `collector.FileInfo`: `Path`, `Dir`, `Name`, `Size`, `ModTime`

## Safety invariants

- All mutations go through `pkg/safepath.Validator`.
- Paths are validated before read/write/remove.
- Dry-run computes operations only.

## Extension points

- Use `collector` for discovery, `hasher` for identity, `safepath` for safety.
