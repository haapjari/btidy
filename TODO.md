# File Organizer Enhancement - Implementation Status

## Goal

- Requirement: Add features, that ensure that no data is lost during the file organization. (400 GB of Data)

---

## Requirements

### Core Requirements

1. "Manifest" Command - Create cryptographic inventory (SHA256) before operations
2. Fix "Flatten" - Use SHA256 hashing instead of weak metadata comparison (CRITICAL BUG FIX)
3. Fix "Renamer" - Use SHA256 hashing instead of weak metadata comparison (CRITICAL BUG FIX)
4. Add Audit Trail (JSON Lines) for all destructive operations.
5. "Verify" Command: Prove no data was lost by comparing against manifest
6. "Unzip": Recursively extract archives during flatten, delete zip after
7. "Merge" Command: Combine directories, keeping only unique files by content hash

### Technical Requirements

- All tests must pass with `--race` flag
- Performance: Handle 150,000+ files efficiently (parallel hashing)
- Use `make check` after each commit (fmt, vet, lint, test-race)
- Comprehensive unit tests for all new functionality
- README examples for all commands

---

## Completed Tasks

<!-- TODO: Currently checking and studying this functionality. -->

### 1. Create pkg/hasher with parallel hashing support (CHECK)

- **Status**: DONE
- **Files created**:
  - `pkg/hasher/hasher.go` - SHA256 hashing with worker pool
  - `pkg/hasher/hasher_test.go` - Comprehensive tests
- **Features**:
  - `ComputeHash()` - Full SHA256 hash
  - `ComputePartialHash()` - Fast pre-filter (first+last 4KB)
  - `HashFiles()` - Parallel hashing with channel-based results
  - `HashFilesWithSizes()` - Parallel hashing with size info
  - Configurable worker count via `WithWorkers()` option
  - Default workers: `runtime.NumCPU()`

### 2. Update deduplicator to use shared hasher package

- **Status**: DONE
- **Files modified**: `pkg/deduplicator/deduplicator.go`
- **Changes**: Refactored to use shared `pkg/hasher` package

### 3. Create pkg/manifest for manifest generation

- **Status**: DONE
- **Files created**:
  - `pkg/manifest/manifest.go` - Manifest types and generation
  - `pkg/manifest/manifest_test.go` - Comprehensive tests
- **Features**:
  - `ManifestEntry` struct with Path, Hash, Size, ModTime
  - `Manifest` struct with Version, CreatedAt, RootPath, Entries
  - `NewGenerator()` - Create manifest generator
  - `Generate()` - Generate manifest with parallel hashing
  - `Save()` / `Load()` - JSON serialization
  - `UniqueHashes()`, `HashIndex()`, `TotalSize()`, `FileCount()`, `UniqueFileCount()`
  - Progress callback support

### 4. Add manifest command to CLI

- **Status**: DONE
- **Files modified**: `cmd/main.go`
- **Usage**: `file-organizer manifest [path] -o manifest.json`
- **Flags**:
  - `-o, --output` - Output path (default: manifest.json)
  - `--workers` - Number of parallel workers
  - `-v, --verbose` - Verbose output

---

## In Progress

### 5. Fix flattener to use content-based hashing
- **Status**: IN PROGRESS (code written, needs `make check`)
- **File**: `pkg/flattener/flattener.go`

#### Bug Being Fixed
The current flattener uses weak duplicate detection:
```go
// OLD (UNSAFE - can cause data loss)
type fileKey struct {
    name  string
    size  int64
    mtime int64
}
```
Two different files with the same name, size, and modification time would be incorrectly identified as duplicates, causing one to be deleted.

#### Fix Applied
Changed to use SHA256 content hash:
```go
// NEW (SAFE)
seenHash := make(map[string]string) // hash -> path
```

#### Changes Made
1. Added `hasher` field to Flattener struct
2. Added `Hash` field to `MoveOperation` struct
3. Added `computeHashes()` for parallel pre-computation
4. Updated `processFile()` to use content hash for duplicate detection
5. Files are only considered duplicates if their SHA256 hash matches

#### Next Step
Run `make check` to verify the fix passes all tests.

---

## Pending Tasks

### 6. Fix renamer to use content-based hashing
- **File**: `pkg/renamer/renamer.go`
- **Bug**: Uses `Size == Size && ModTime == ModTime` for duplicate detection
- **Fix**: Add hasher, compute content hashes, compare by hash
- **Changes needed**:
  - Add `hasher` field to Renamer struct
  - Add `Hash` field to `RenameOperation` struct
  - Update `sameFileInfo()` or replace with hash-based comparison

### 7. Add bug fix tests for flattener and renamer
- **Files**: `pkg/flattener/flattener_test.go`, `pkg/renamer/renamer_test.go`
- **Critical test cases**:
  - `TestFlattener_SameMetadataDifferentContent` - Two files with same name/size/mtime but DIFFERENT content must both be kept
  - `TestRenamer_SameMetadataDifferentContent` - Same scenario for renamer

### 8. Create pkg/oplog for operation logging
- **Files to create**:
  - `pkg/oplog/oplog.go`
  - `pkg/oplog/oplog_test.go`
- **Features**:
  - `LogEntry` struct with Timestamp, Operation, SourcePath, DestPath, Hash, Size, Reason
  - `OperationType` enum: OpDelete, OpMove, OpRename, OpExtract
  - `JSONLinesLogger` - Writes one JSON per line
  - `NullLogger` - For dry-run mode

### 9. Integrate --log flag into commands
- **Files**: `cmd/main.go`
- **Commands**: flatten, duplicate, rename, (future: merge)
- **Usage**: `file-organizer flatten --log operations.jsonl /path`

### 10. Create verify logic in pkg/manifest
- **File to create**: `pkg/manifest/verify.go`
- **Features**:
  - `VerifyResult` struct with HashesFound, HashesMissing, MissingHashes
  - `Verifier` struct
  - `Verify()` - Compare target directory against manifest
  - `IsDataLoss()` - Returns true if any content hashes are missing

### 11. Add verify command to CLI
- **File**: `cmd/main.go`
- **Usage**: `file-organizer verify --manifest before.json /path`
- **Exit codes**: 0 = all content preserved, 1 = DATA LOSS DETECTED

### 12. Create pkg/archive for zip extraction
- **Files to create**:
  - `pkg/archive/archive.go`
  - `pkg/archive/archive_test.go`
- **Features**:
  - `Extractor` struct with temp directory management
  - `Extract()` - Extract zip to temp dir, return file list
  - `IsArchive()` - Check if file is .zip/.ZIP
  - `Cleanup()` - Remove temp extraction directory

### 13. Integrate zip extraction into flatten command
- **File**: `pkg/flattener/flattener.go`
- **Behavior**:
  - Detect zip archives during flatten
  - Extract to temp directory
  - Recursively process extracted files (handles nested zips)
  - Flatten extracted files to root with other files
  - Delete original zip after successful extraction
  - Log extraction operations

### 14. Create pkg/merger for directory merge
- **Files to create**:
  - `pkg/merger/merger.go`
  - `pkg/merger/merger_test.go`
- **Features**:
  - `Merger` struct with target directory and hasher
  - `Merge()` - Move unique files from sources to target
  - Deduplication across sources by content hash
  - Keep one copy, delete others
  - Clean up empty directories in sources

### 15. Add merge command to CLI
- **File**: `cmd/main.go`
- **Usage**: `file-organizer merge [source1] [source2] [target]`
- **Flags**: `--dry-run`, `--log`, `--workers`, `-v`

### 16. Update README with examples for all commands
- **File**: `README.md`
- **Content**:
  - Examples for manifest, verify, flatten, duplicate, merge
  - Safe workflow example
  - Merge workflow example

### 17. Create manual test script
- **File**: `scripts/test_workflow.sh`
- **Test scenarios**:
  - Nested directories
  - Duplicate files (same content)
  - Files with same metadata but different content (bug fix validation)
  - Nested zip archives
  - Merge scenario with overlapping content

---

## Git Commits Made

1. `feat: add duplicate command with SHA256 content hashing`
2. `chore: flatten history`
3. `refactor: extract hasher package from deduplicator` - Created pkg/hasher
4. `refactor: update deduplicator to use shared hasher package`
5. `feat: add manifest package for cryptographic file inventory`
6. `feat: add manifest command to CLI`

---

## Commands Reference

### Current Commands (working)
```bash
file-organizer rename --dry-run /path      # Rename with date prefix
file-organizer flatten --dry-run /path     # Flatten directories (NEEDS BUG FIX)
file-organizer duplicate --dry-run /path   # Remove duplicates by hash
file-organizer manifest /path -o out.json  # Create file inventory
```

### Planned Commands
```bash
file-organizer verify --manifest before.json /path  # Verify no data loss
file-organizer merge /source1 /source2 /target      # Merge directories
```

### Safe Workflow (after all features complete)
```bash
# 1. Create manifest before any operations
file-organizer manifest /backup -o before.json

# 2. Preview operations
file-organizer flatten --dry-run /backup
file-organizer duplicate --dry-run /backup

# 3. Apply with logging
file-organizer flatten --log operations.jsonl /backup
file-organizer duplicate --log operations.jsonl /backup

# 4. Verify no data was lost
file-organizer verify --manifest before.json /backup
```

---

## Performance Considerations

- **Parallel hashing**: Worker pool with `runtime.NumCPU()` workers
- **Size-based pre-filtering**: Skip hashing for files with unique sizes
- **Two-phase hashing**: Partial hash (8KB) then full hash for large files
- **Progress reporting**: Callbacks for long operations
- **Memory efficiency**: Stream-based processing via channels

---


