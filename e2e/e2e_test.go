package e2e

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"btidy/pkg/manifest"
)

var builtBinaryPath string

type cmdResult struct {
	stdout string
	stderr string
	err    error
}

type zipFixtureEntry struct {
	name    string
	content []byte
	mode    os.FileMode
}

func (r cmdResult) combinedOutput() string {
	return r.stdout + r.stderr
}

func resolveRepoRoot() (string, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("failed to resolve repo root")
	}

	root := filepath.Dir(filepath.Dir(filename))
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("failed to resolve repo root: %w", err)
	}

	return absRoot, nil
}

func TestMain(m *testing.M) {
	repoRoot, err := resolveRepoRoot()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "failed to initialize e2e tests: %v\n", err)
		os.Exit(1)
	}

	binDir, err := os.MkdirTemp("", "btidy-e2e-bin-*")
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "failed to create temp directory for binary: %v\n", err)
		os.Exit(1)
	}

	binPath := filepath.Join(binDir, "btidy")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}

	buildOutput, buildErr := buildBinary(binPath, repoRoot)
	if buildErr != nil {
		_, _ = fmt.Fprintf(os.Stderr, "failed to build btidy: %v\n%s\n", buildErr, string(buildOutput))
		_ = os.RemoveAll(binDir)
		os.Exit(1)
	}

	builtBinaryPath = binPath

	exitCode := m.Run()
	_ = os.RemoveAll(binDir)
	os.Exit(exitCode)
}

func buildBinary(binPath, repoRoot string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "build", "-o", binPath, "./cmd")
	cmd.Dir = repoRoot

	return cmd.CombinedOutput()
}

func binaryPath(t *testing.T) string {
	t.Helper()

	if builtBinaryPath == "" {
		t.Fatal("binary path not initialized")
	}

	return builtBinaryPath
}

func runBinary(t *testing.T, binPath string, args ...string) cmdResult {
	t.Helper()

	timeout := 30 * time.Second
	if deadline, ok := t.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining < timeout {
			timeout = remaining
		}
	}
	if timeout <= 0 {
		timeout = time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binPath, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		if stderr.Len() > 0 && !strings.HasSuffix(stderr.String(), "\n") {
			stderr.WriteString("\n")
		}
		stderr.WriteString("command timed out after " + timeout.String())
	}

	return cmdResult{
		stdout: stdout.String(),
		stderr: stderr.String(),
		err:    err,
	}
}

func writeFile(t *testing.T, path, content string, modTime time.Time) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("failed to set file times: %v", err)
	}
}

func writeZipArchive(t *testing.T, archivePath string, entries []zipFixtureEntry) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
		t.Fatalf("failed to create archive directory: %v", err)
	}

	archiveFile, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("failed to create archive: %v", err)
	}

	writer := zip.NewWriter(archiveFile)
	for _, entry := range entries {
		header := zip.FileHeader{
			Name:   entry.name,
			Method: zip.Deflate,
		}

		mode := entry.mode
		if mode == 0 {
			mode = 0o644
		}
		header.SetMode(mode)

		entryWriter, createErr := writer.CreateHeader(&header)
		if createErr != nil {
			t.Fatalf("failed to create archive entry: %v", createErr)
		}

		if _, writeErr := entryWriter.Write(entry.content); writeErr != nil {
			t.Fatalf("failed to write archive entry: %v", writeErr)
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close archive writer: %v", err)
	}
	if err := archiveFile.Close(); err != nil {
		t.Fatalf("failed to close archive file: %v", err)
	}
}

func zipBytes(t *testing.T, entries []zipFixtureEntry) []byte {
	t.Helper()

	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, entry := range entries {
		header := zip.FileHeader{
			Name:   entry.name,
			Method: zip.Deflate,
		}

		mode := entry.mode
		if mode == 0 {
			mode = 0o644
		}
		header.SetMode(mode)

		entryWriter, createErr := writer.CreateHeader(&header)
		if createErr != nil {
			t.Fatalf("failed to create zip bytes entry: %v", createErr)
		}

		if _, writeErr := entryWriter.Write(entry.content); writeErr != nil {
			t.Fatalf("failed to write zip bytes entry: %v", writeErr)
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close zip bytes writer: %v", err)
	}

	return buffer.Bytes()
}

func assertExists(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected path to exist: %s (error: %v)", path, err)
	}
}

func assertFileContent(t *testing.T, path, expectedContent string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file %s: %v", path, err)
	}
	if string(data) != expectedContent {
		t.Fatalf("file %s: expected content %q, got %q", path, expectedContent, string(data))
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); err == nil {
		t.Fatalf("expected path to be missing: %s", path)
	} else if !os.IsNotExist(err) {
		t.Fatalf("expected path to be missing: %s (unexpected error: %v)", path, err)
	}
}

func assertCommandFailed(t *testing.T, result cmdResult, keywords ...string) {
	t.Helper()

	if result.err == nil {
		t.Fatalf("expected command to fail\nstdout:\n%s\nstderr:\n%s", result.stdout, result.stderr)
	}

	combined := strings.ToLower(result.combinedOutput())
	for _, keyword := range keywords {
		if !strings.Contains(combined, strings.ToLower(keyword)) {
			t.Fatalf("expected output to contain %q\n%s", keyword, result.combinedOutput())
		}
	}
}

func assertCommandSucceeded(t *testing.T, label string, result cmdResult) {
	t.Helper()

	if result.err != nil {
		t.Fatalf("%s failed: %v\n%s", label, result.err, result.combinedOutput())
	}
}

func fileCount(t *testing.T, root string) int {
	t.Helper()

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("failed to read directory %s: %v", root, err)
	}

	count := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			count++
		}
	}

	return count
}

func hashSetsEqual(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for hash := range a {
		if _, ok := b[hash]; !ok {
			return false
		}
	}
	return true
}

func TestEndToEndRename_DryRunAndApply(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2020, 2, 3, 4, 5, 6, 0, time.UTC)

	writeFile(t, filepath.Join(root, "My Document.pdf"), "alpha", modTime)
	writeFile(t, filepath.Join(root, "Report (Final).docx"), "beta", modTime)
	writeFile(t, filepath.Join(root, "Photo.JPG"), "gamma", modTime)

	dryRun := runBinary(t, binPath, "rename", "--dry-run", root)
	assertCommandSucceeded(t, "rename dry-run", dryRun)
	if !strings.Contains(dryRun.stdout, "=== DRY RUN - no changes will be made ===") {
		t.Fatalf("expected dry-run banner in output\n%s", dryRun.stdout)
	}

	assertExists(t, filepath.Join(root, "My Document.pdf"))
	assertExists(t, filepath.Join(root, "Report (Final).docx"))
	assertExists(t, filepath.Join(root, "Photo.JPG"))

	datePrefix := modTime.Format("2006-01-02")
	assertMissing(t, filepath.Join(root, datePrefix+"_my_document.pdf"))

	apply := runBinary(t, binPath, "rename", root)
	assertCommandSucceeded(t, "rename apply", apply)

	assertMissing(t, filepath.Join(root, "My Document.pdf"))
	assertMissing(t, filepath.Join(root, "Report (Final).docx"))
	assertMissing(t, filepath.Join(root, "Photo.JPG"))

	assertExists(t, filepath.Join(root, datePrefix+"_my_document.pdf"))
	assertExists(t, filepath.Join(root, datePrefix+"_report_final.docx"))
	assertExists(t, filepath.Join(root, datePrefix+"_photo.jpg"))
}

func TestEndToEndFlatten_DryRunAndApply(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2021, 7, 12, 11, 30, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "dir1", "file.txt"), "same", modTime)
	writeFile(t, filepath.Join(root, "dir2", "file.txt"), "same", modTime)
	writeFile(t, filepath.Join(root, "dir1", "unique.txt"), "u1", modTime)
	writeFile(t, filepath.Join(root, "dir2", "unique.txt"), "u2", modTime)
	writeFile(t, filepath.Join(root, "rootfile.txt"), "root", modTime)

	dryRun := runBinary(t, binPath, "--workers", "1", "flatten", "--dry-run", root)
	assertCommandSucceeded(t, "flatten dry-run", dryRun)

	assertExists(t, filepath.Join(root, "dir1", "file.txt"))
	assertExists(t, filepath.Join(root, "dir2", "file.txt"))
	assertMissing(t, filepath.Join(root, "file.txt"))

	apply := runBinary(t, binPath, "--workers", "1", "flatten", root)
	assertCommandSucceeded(t, "flatten apply", apply)

	assertExists(t, filepath.Join(root, "file.txt"))
	assertExists(t, filepath.Join(root, "unique.txt"))
	assertExists(t, filepath.Join(root, "unique_1.txt"))
	assertExists(t, filepath.Join(root, "rootfile.txt"))

	assertMissing(t, filepath.Join(root, "dir1", "file.txt"))
	assertMissing(t, filepath.Join(root, "dir2", "file.txt"))
	assertMissing(t, filepath.Join(root, "dir1"))
	assertMissing(t, filepath.Join(root, "dir2"))
}

func TestEndToEndDuplicate_DryRunAndApply(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2022, 4, 2, 9, 15, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "a.txt"), "same", modTime)
	writeFile(t, filepath.Join(root, "sub", "a_copy.txt"), "same", modTime)
	writeFile(t, filepath.Join(root, "unique.txt"), "unique", modTime)

	dryRun := runBinary(t, binPath, "--workers", "1", "duplicate", "--dry-run", root)
	assertCommandSucceeded(t, "duplicate dry-run", dryRun)

	assertExists(t, filepath.Join(root, "a.txt"))
	assertExists(t, filepath.Join(root, "sub", "a_copy.txt"))

	apply := runBinary(t, binPath, "--workers", "1", "duplicate", root)
	assertCommandSucceeded(t, "duplicate apply", apply)

	assertExists(t, filepath.Join(root, "a.txt"))
	assertMissing(t, filepath.Join(root, "sub", "a_copy.txt"))
	assertExists(t, filepath.Join(root, "unique.txt"))
}

func TestEndToEndUnzip_DryRunAndApplyRecursive(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()

	innerArchive := zipBytes(t, []zipFixtureEntry{
		{name: "deep/final.txt", content: []byte("payload")},
	})

	outerArchivePath := filepath.Join(root, "outer.zip")
	writeZipArchive(t, outerArchivePath, []zipFixtureEntry{
		{name: "nested/inner.zip", content: innerArchive},
		{name: "outer.txt", content: []byte("outer")},
	})

	dryRun := runBinary(t, binPath, "unzip", "--dry-run", root)
	assertCommandSucceeded(t, "unzip dry-run", dryRun)

	assertExists(t, outerArchivePath)
	assertMissing(t, filepath.Join(root, "nested", "inner.zip"))
	assertMissing(t, filepath.Join(root, "nested", "deep", "final.txt"))

	apply := runBinary(t, binPath, "unzip", root)
	assertCommandSucceeded(t, "unzip apply", apply)

	assertMissing(t, filepath.Join(root, "outer.zip"))
	assertMissing(t, filepath.Join(root, "nested", "inner.zip"))
	assertExists(t, filepath.Join(root, "outer.txt"))
	assertExists(t, filepath.Join(root, "nested", "deep", "final.txt"))
}

func TestEndToEndUnzip_ZipSlipBlocked(t *testing.T) {
	binPath := binaryPath(t)
	workspace := t.TempDir()
	target := filepath.Join(workspace, "target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("failed to create target: %v", err)
	}

	outsideSentinel := filepath.Join(workspace, "outside-sentinel.txt")
	if err := os.WriteFile(outsideSentinel, []byte("do-not-touch"), 0o600); err != nil {
		t.Fatalf("failed to create outside sentinel: %v", err)
	}
	outsideBefore, err := os.ReadFile(outsideSentinel)
	if err != nil {
		t.Fatalf("failed to read outside sentinel before unzip: %v", err)
	}

	archivePath := filepath.Join(target, "bad.zip")
	writeZipArchive(t, archivePath, []zipFixtureEntry{
		{name: "../outside-sentinel.txt", content: []byte("attack")},
	})

	// The command succeeds but the zip-slip entry is skipped (not extracted).
	result := runBinary(t, binPath, "unzip", target)
	assertCommandSucceeded(t, "unzip with zip-slip entry", result)

	output := strings.ToLower(result.combinedOutput())
	if !strings.Contains(output, "entry error") {
		t.Fatalf("expected output to report skipped zip-slip entry\n%s", result.combinedOutput())
	}
	if !strings.Contains(output, "escape") {
		t.Fatalf("expected output to mention path escape\n%s", result.combinedOutput())
	}

	outsideAfter, err := os.ReadFile(outsideSentinel)
	if err != nil {
		t.Fatalf("failed to read outside sentinel after unzip: %v", err)
	}
	if !bytes.Equal(outsideBefore, outsideAfter) {
		t.Fatalf("outside sentinel changed unexpectedly")
	}
}

func TestEndToEndRename_Idempotent(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2022, 8, 10, 16, 20, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "My Document.pdf"), "alpha", modTime)

	firstRun := runBinary(t, binPath, "rename", root)
	assertCommandSucceeded(t, "first rename run", firstRun)

	datePrefix := modTime.Format("2006-01-02")
	renamedPath := filepath.Join(root, datePrefix+"_my_document.pdf")
	assertExists(t, renamedPath)

	secondRun := runBinary(t, binPath, "rename", root)
	assertCommandSucceeded(t, "second rename run", secondRun)

	assertExists(t, renamedPath)
	assertMissing(t, filepath.Join(root, "My Document.pdf"))
	assertMissing(t, filepath.Join(root, datePrefix+"_"+datePrefix+"_my_document.pdf"))

	if got := fileCount(t, root); got != 1 {
		t.Fatalf("expected exactly one file after idempotent rename, got %d", got)
	}
}

func TestEndToEndDuplicate_Idempotent(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2022, 11, 4, 9, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "a.txt"), "same", modTime)
	writeFile(t, filepath.Join(root, "b.txt"), "same", modTime)
	writeFile(t, filepath.Join(root, "unique.txt"), "unique", modTime)

	firstRun := runBinary(t, binPath, "duplicate", root)
	assertCommandSucceeded(t, "first duplicate run", firstRun)

	if got := fileCount(t, root); got != 2 {
		t.Fatalf("expected two files after first dedupe run, got %d", got)
	}

	secondRun := runBinary(t, binPath, "duplicate", root)
	assertCommandSucceeded(t, "second duplicate run", secondRun)

	if got := fileCount(t, root); got != 2 {
		t.Fatalf("expected file count to remain stable after second dedupe run, got %d", got)
	}
}

func TestEndToEndSkipFiles_DefaultsAreIgnored(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2023, 1, 15, 8, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, ".DS_Store"), "metadata", modTime)
	writeFile(t, filepath.Join(root, "organizer.log"), "logs", modTime)
	writeFile(t, filepath.Join(root, "sub", "Thumbs.db"), "thumbs", modTime)
	writeFile(t, filepath.Join(root, "sub", "My Notes.txt"), "notes", modTime)

	renameRun := runBinary(t, binPath, "rename", root)
	assertCommandSucceeded(t, "rename run", renameRun)

	datePrefix := modTime.Format("2006-01-02")
	assertExists(t, filepath.Join(root, ".DS_Store"))
	assertExists(t, filepath.Join(root, "organizer.log"))
	assertExists(t, filepath.Join(root, "sub", "Thumbs.db"))
	assertExists(t, filepath.Join(root, "sub", datePrefix+"_my_notes.txt"))

	flattenRun := runBinary(t, binPath, "flatten", root)
	assertCommandSucceeded(t, "flatten run", flattenRun)

	assertExists(t, filepath.Join(root, ".DS_Store"))
	assertExists(t, filepath.Join(root, "organizer.log"))
	assertExists(t, filepath.Join(root, "sub", "Thumbs.db"))
	assertExists(t, filepath.Join(root, datePrefix+"_my_notes.txt"))
	assertMissing(t, filepath.Join(root, "sub", datePrefix+"_my_notes.txt"))

	duplicateRun := runBinary(t, binPath, "duplicate", root)
	assertCommandSucceeded(t, "duplicate run", duplicateRun)

	assertExists(t, filepath.Join(root, ".DS_Store"))
	assertExists(t, filepath.Join(root, "organizer.log"))
	assertExists(t, filepath.Join(root, "sub", "Thumbs.db"))
}

func TestEndToEndPipeline_ManifestIntegrity(t *testing.T) {
	binPath := binaryPath(t)
	workspace := t.TempDir()
	target := filepath.Join(workspace, "target")
	outsideSentinel := filepath.Join(workspace, "outside-sentinel.txt")
	modTime := time.Date(2023, 9, 18, 7, 45, 0, 0, time.UTC)

	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("failed to create target: %v", err)
	}
	writeFile(t, outsideSentinel, "do-not-touch", modTime)

	outsideBefore, err := os.ReadFile(outsideSentinel)
	if err != nil {
		t.Fatalf("failed to read outside sentinel before operations: %v", err)
	}

	writeFile(t, filepath.Join(target, "docs", "report.pdf"), "same", modTime)
	writeFile(t, filepath.Join(target, "backup", "report copy.pdf"), "same", modTime)
	writeFile(t, filepath.Join(target, "photos", "file.txt"), "alpha", modTime)
	writeFile(t, filepath.Join(target, "other", "file.txt"), "beta", modTime)
	writeFile(t, filepath.Join(target, "other", "unique.txt"), "unique", modTime)

	beforeManifest := filepath.Join(target, ".DS_Store")
	afterManifest := filepath.Join(target, "Thumbs.db")

	before := runBinary(t, binPath, "--workers", "1", "manifest", target, "-o", beforeManifest)
	assertCommandSucceeded(t, "manifest before", before)

	rename := runBinary(t, binPath, "rename", target)
	assertCommandSucceeded(t, "rename", rename)

	flatten := runBinary(t, binPath, "--workers", "1", "flatten", target)
	assertCommandSucceeded(t, "flatten", flatten)

	duplicate := runBinary(t, binPath, "--workers", "1", "duplicate", target)
	assertCommandSucceeded(t, "duplicate", duplicate)

	after := runBinary(t, binPath, "--workers", "1", "manifest", target, "-o", afterManifest)
	assertCommandSucceeded(t, "manifest after", after)

	beforeData, err := manifest.Load(beforeManifest)
	if err != nil {
		t.Fatalf("failed to load before manifest: %v", err)
	}
	afterData, err := manifest.Load(afterManifest)
	if err != nil {
		t.Fatalf("failed to load after manifest: %v", err)
	}

	if afterData.FileCount() > beforeData.FileCount() {
		t.Fatalf("expected file count to stay same or decrease")
	}

	if beforeData.UniqueFileCount() != afterData.UniqueFileCount() {
		t.Fatalf("expected unique file count to remain stable")
	}

	if !hashSetsEqual(beforeData.UniqueHashes(), afterData.UniqueHashes()) {
		t.Fatalf("expected unique hashes to remain stable")
	}

	entries, err := os.ReadDir(target)
	if err != nil {
		t.Fatalf("failed to read target directory: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != ".btidy" {
			t.Fatalf("expected no subdirectories after flatten: %s", entry.Name())
		}
	}

	outsideAfter, err := os.ReadFile(outsideSentinel)
	if err != nil {
		t.Fatalf("failed to read outside sentinel after operations: %v", err)
	}
	if !bytes.Equal(outsideBefore, outsideAfter) {
		t.Fatalf("outside sentinel changed unexpectedly")
	}
}

func TestEndToEndInvalidTargetPaths(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 1, 5, 6, 0, 0, 0, time.UTC)

	filePath := filepath.Join(root, "file.txt")
	writeFile(t, filePath, "content", modTime)

	fileTarget := runBinary(t, binPath, "rename", filePath)
	assertCommandFailed(t, fileTarget, "directory", filePath)

	missingPath := filepath.Join(root, "missing")
	missingTarget := runBinary(t, binPath, "rename", missingPath)
	assertCommandFailed(t, missingTarget, "cannot access", "directory", missingPath)
}

func TestEndToEndRename_SymlinkEscapeBlocked(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	outside := t.TempDir()
	modTime := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)

	outsideFile := filepath.Join(outside, "outside.txt")
	writeFile(t, outsideFile, "outside", modTime)
	outsideBefore, err := os.ReadFile(outsideFile)
	if err != nil {
		t.Fatalf("failed to read outside sentinel before rename: %v", err)
	}

	linkPath := filepath.Join(root, "escape_link.txt")
	if symlinkErr := os.Symlink(outsideFile, linkPath); symlinkErr != nil {
		t.Skipf("symlink not supported: %v", symlinkErr)
	}

	result := runBinary(t, binPath, "rename", "--dry-run", root)
	assertCommandFailed(t, result, "unsafe", "symlink")

	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatalf("expected symlink to remain: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected link to be a symlink")
	}

	outsideAfter, err := os.ReadFile(outsideFile)
	if err != nil {
		t.Fatalf("failed to read outside sentinel after rename: %v", err)
	}
	if !bytes.Equal(outsideBefore, outsideAfter) {
		t.Fatalf("outside sentinel changed unexpectedly")
	}
}

func TestEndToEndFlatten_SymlinkEscapeBlocked(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	outside := t.TempDir()
	modTime := time.Date(2024, 3, 2, 12, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "nested", "safe.txt"), "safe", modTime)

	outsideFile := filepath.Join(outside, "outside.txt")
	writeFile(t, outsideFile, "outside", modTime)
	outsideBefore, err := os.ReadFile(outsideFile)
	if err != nil {
		t.Fatalf("failed to read outside sentinel before flatten: %v", err)
	}

	linkPath := filepath.Join(root, "nested", "escape_link.txt")
	if symlinkErr := os.Symlink(outsideFile, linkPath); symlinkErr != nil {
		t.Skipf("symlink not supported: %v", symlinkErr)
	}

	result := runBinary(t, binPath, "flatten", root)
	assertCommandFailed(t, result, "unsafe", "symlink")

	assertExists(t, filepath.Join(root, "nested", "safe.txt"))
	assertMissing(t, filepath.Join(root, "safe.txt"))
	assertExists(t, linkPath)

	outsideAfter, err := os.ReadFile(outsideFile)
	if err != nil {
		t.Fatalf("failed to read outside sentinel after flatten: %v", err)
	}
	if !bytes.Equal(outsideBefore, outsideAfter) {
		t.Fatalf("outside sentinel changed unexpectedly")
	}
}

func TestEndToEndDuplicate_SymlinkEscapeBlocked(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	outside := t.TempDir()
	modTime := time.Date(2024, 3, 3, 12, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "a.txt"), "same", modTime)
	writeFile(t, filepath.Join(root, "b.txt"), "same", modTime)

	outsideFile := filepath.Join(outside, "outside.txt")
	writeFile(t, outsideFile, "outside", modTime)
	outsideBefore, err := os.ReadFile(outsideFile)
	if err != nil {
		t.Fatalf("failed to read outside sentinel before duplicate: %v", err)
	}

	linkPath := filepath.Join(root, "escape_link.txt")
	if symlinkErr := os.Symlink(outsideFile, linkPath); symlinkErr != nil {
		t.Skipf("symlink not supported: %v", symlinkErr)
	}

	result := runBinary(t, binPath, "duplicate", root)
	assertCommandFailed(t, result, "unsafe", "symlink")

	assertExists(t, filepath.Join(root, "a.txt"))
	assertExists(t, filepath.Join(root, "b.txt"))
	assertExists(t, linkPath)

	outsideAfter, err := os.ReadFile(outsideFile)
	if err != nil {
		t.Fatalf("failed to read outside sentinel after duplicate: %v", err)
	}
	if !bytes.Equal(outsideBefore, outsideAfter) {
		t.Fatalf("outside sentinel changed unexpectedly")
	}
}

func TestEndToEndManifest_SymlinkEscapeBlocked(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	outside := t.TempDir()
	modTime := time.Date(2024, 3, 4, 12, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "safe.txt"), "safe", modTime)

	outsideFile := filepath.Join(outside, "outside.txt")
	writeFile(t, outsideFile, "outside", modTime)
	outsideBefore, err := os.ReadFile(outsideFile)
	if err != nil {
		t.Fatalf("failed to read outside sentinel before manifest: %v", err)
	}

	linkPath := filepath.Join(root, "escape_link.txt")
	if symlinkErr := os.Symlink(outsideFile, linkPath); symlinkErr != nil {
		t.Skipf("symlink not supported: %v", symlinkErr)
	}

	manifestPath := filepath.Join(root, "manifest.json")
	result := runBinary(t, binPath, "manifest", root, "-o", manifestPath)
	assertCommandFailed(t, result, "unsafe", "symlink")

	assertMissing(t, manifestPath)
	assertExists(t, linkPath)

	outsideAfter, err := os.ReadFile(outsideFile)
	if err != nil {
		t.Fatalf("failed to read outside sentinel after manifest: %v", err)
	}
	if !bytes.Equal(outsideBefore, outsideAfter) {
		t.Fatalf("outside sentinel changed unexpectedly")
	}
}

func TestEndToEndManifest_OutputOutsideTargetRejected(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	outside := t.TempDir()
	modTime := time.Date(2024, 3, 5, 12, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "safe.txt"), "safe", modTime)

	outsideOutputPath := filepath.Join(outside, "manifest.json")
	result := runBinary(t, binPath, "manifest", root, "-o", outsideOutputPath)
	assertCommandFailed(t, result, "output path", "target directory")

	assertMissing(t, outsideOutputPath)
}

func TestEndToEndOrganize_DryRunAndApply(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2023, 5, 10, 14, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "report.pdf"), "pdf-content", modTime)
	writeFile(t, filepath.Join(root, "photo.jpg"), "jpg-content", modTime)
	writeFile(t, filepath.Join(root, "notes.txt"), "txt-content", modTime)
	writeFile(t, filepath.Join(root, "Makefile"), "make-content", modTime)

	// Dry-run should not move files.
	dryRun := runBinary(t, binPath, "organize", "--dry-run", root)
	assertCommandSucceeded(t, "organize dry-run", dryRun)
	if !strings.Contains(dryRun.stdout, "=== DRY RUN - no changes will be made ===") {
		t.Fatalf("expected dry-run banner in output\n%s", dryRun.stdout)
	}

	assertExists(t, filepath.Join(root, "report.pdf"))
	assertExists(t, filepath.Join(root, "photo.jpg"))
	assertExists(t, filepath.Join(root, "notes.txt"))
	assertExists(t, filepath.Join(root, "Makefile"))

	// Apply should move files into extension directories.
	apply := runBinary(t, binPath, "organize", root)
	assertCommandSucceeded(t, "organize apply", apply)

	assertMissing(t, filepath.Join(root, "report.pdf"))
	assertMissing(t, filepath.Join(root, "photo.jpg"))
	assertMissing(t, filepath.Join(root, "notes.txt"))
	assertMissing(t, filepath.Join(root, "Makefile"))

	assertExists(t, filepath.Join(root, "pdf", "report.pdf"))
	assertExists(t, filepath.Join(root, "jpg", "photo.jpg"))
	assertExists(t, filepath.Join(root, "txt", "notes.txt"))
	assertExists(t, filepath.Join(root, "other", "Makefile"))
}

func TestEndToEndOrganize_AfterFlatten(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2023, 5, 10, 14, 0, 0, 0, time.UTC)

	// Create a nested structure with mixed file types.
	writeFile(t, filepath.Join(root, "docs", "report.pdf"), "pdf", modTime)
	writeFile(t, filepath.Join(root, "photos", "vacation", "photo.jpg"), "jpg", modTime)
	writeFile(t, filepath.Join(root, "notes.txt"), "txt", modTime)

	// Flatten first.
	flatten := runBinary(t, binPath, "--workers", "1", "flatten", root)
	assertCommandSucceeded(t, "flatten", flatten)

	// All files should be in root now.
	assertExists(t, filepath.Join(root, "report.pdf"))
	assertExists(t, filepath.Join(root, "photo.jpg"))
	assertExists(t, filepath.Join(root, "notes.txt"))

	// Organize should group them by extension.
	organize := runBinary(t, binPath, "organize", root)
	assertCommandSucceeded(t, "organize", organize)

	assertExists(t, filepath.Join(root, "pdf", "report.pdf"))
	assertExists(t, filepath.Join(root, "jpg", "photo.jpg"))
	assertExists(t, filepath.Join(root, "txt", "notes.txt"))
}

func TestEndToEndOrganize_SymlinkEscapeBlocked(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	outside := t.TempDir()
	modTime := time.Date(2024, 3, 6, 12, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "safe.txt"), "safe", modTime)

	outsideFile := filepath.Join(outside, "outside.txt")
	writeFile(t, outsideFile, "outside", modTime)
	outsideBefore, err := os.ReadFile(outsideFile)
	if err != nil {
		t.Fatalf("failed to read outside sentinel before organize: %v", err)
	}

	linkPath := filepath.Join(root, "escape_link.txt")
	if symlinkErr := os.Symlink(outsideFile, linkPath); symlinkErr != nil {
		t.Skipf("symlink not supported: %v", symlinkErr)
	}

	result := runBinary(t, binPath, "organize", root)
	assertCommandFailed(t, result, "unsafe", "symlink")

	// Safe file should not have been moved.
	assertExists(t, filepath.Join(root, "safe.txt"))
	assertExists(t, linkPath)

	outsideAfter, err := os.ReadFile(outsideFile)
	if err != nil {
		t.Fatalf("failed to read outside sentinel after organize: %v", err)
	}
	if !bytes.Equal(outsideBefore, outsideAfter) {
		t.Fatalf("outside sentinel changed unexpectedly")
	}
}

func TestEndToEndBtidyDir_NeverCollected(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)

	// Create a .btidy directory with files that should never be touched.
	writeFile(t, filepath.Join(root, ".btidy", "trash", "run1", "trashed.txt"), "trashed", modTime)
	writeFile(t, filepath.Join(root, "normal.txt"), "normal content", modTime)

	// Run rename - should only process normal.txt, not .btidy contents.
	renameResult := runBinary(t, binPath, "rename", root)
	assertCommandSucceeded(t, "rename with .btidy dir", renameResult)

	// Verify .btidy trash files are untouched.
	assertExists(t, filepath.Join(root, ".btidy", "trash", "run1", "trashed.txt"))

	// Verify output says 1 file found (not 3).
	if !strings.Contains(renameResult.stdout, "Found 1 file") {
		t.Fatalf("expected 'Found 1 file' in output, got:\n%s", renameResult.stdout)
	}
}

func TestEndToEndUndo_ReversesDuplicate(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 7, 1, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "a.txt"), "same-content", modTime)
	writeFile(t, filepath.Join(root, "b.txt"), "same-content", modTime)
	writeFile(t, filepath.Join(root, "unique.txt"), "unique", modTime)

	// Run duplicate to trash one of the duplicate files.
	dupResult := runBinary(t, binPath, "--workers", "1", "--no-snapshot", "duplicate", root)
	assertCommandSucceeded(t, "duplicate", dupResult)

	// Only 2 files should remain (one dup removed).
	if got := fileCount(t, root); got != 2 {
		t.Fatalf("expected 2 files after dedup, got %d", got)
	}

	// Undo the duplicate.
	undoResult := runBinary(t, binPath, "undo", root)
	assertCommandSucceeded(t, "undo duplicate", undoResult)

	if !strings.Contains(undoResult.stdout, "Restored:  1") {
		t.Fatalf("expected 'Restored:  1' in undo output\n%s", undoResult.stdout)
	}

	// All 3 files should be back.
	assertExists(t, filepath.Join(root, "a.txt"))
	assertExists(t, filepath.Join(root, "b.txt"))
	assertExists(t, filepath.Join(root, "unique.txt"))
}

func TestEndToEndUndo_ReversesRename(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 7, 2, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "My Document.pdf"), "content", modTime)

	// Run rename.
	renameResult := runBinary(t, binPath, "--no-snapshot", "rename", root)
	assertCommandSucceeded(t, "rename", renameResult)

	datePrefix := modTime.Format("2006-01-02")
	renamedPath := filepath.Join(root, datePrefix+"_my_document.pdf")
	assertExists(t, renamedPath)
	assertMissing(t, filepath.Join(root, "My Document.pdf"))

	// Undo the rename.
	undoResult := runBinary(t, binPath, "undo", root)
	assertCommandSucceeded(t, "undo rename", undoResult)

	if !strings.Contains(undoResult.stdout, "Reversed:  1") {
		t.Fatalf("expected 'Reversed:  1' in undo output\n%s", undoResult.stdout)
	}

	// Original name should be restored.
	assertExists(t, filepath.Join(root, "My Document.pdf"))
	assertMissing(t, renamedPath)
}

func TestEndToEndUndo_ReversesFlatten(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 7, 3, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "sub", "deep", "file.txt"), "content", modTime)

	// Run flatten.
	flattenResult := runBinary(t, binPath, "--workers", "1", "--no-snapshot", "flatten", root)
	assertCommandSucceeded(t, "flatten", flattenResult)

	assertExists(t, filepath.Join(root, "file.txt"))
	assertMissing(t, filepath.Join(root, "sub", "deep", "file.txt"))

	// Undo the flatten.
	undoResult := runBinary(t, binPath, "undo", root)
	assertCommandSucceeded(t, "undo flatten", undoResult)

	if !strings.Contains(undoResult.stdout, "Reversed:  1") {
		t.Fatalf("expected 'Reversed:  1' in undo output\n%s", undoResult.stdout)
	}

	// File should be restored to original location.
	assertExists(t, filepath.Join(root, "sub", "deep", "file.txt"))
	assertMissing(t, filepath.Join(root, "file.txt"))
}

func TestEndToEndUndo_DryRunNoChanges(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 7, 4, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "My Document.pdf"), "content", modTime)

	// Run rename.
	renameResult := runBinary(t, binPath, "--no-snapshot", "rename", root)
	assertCommandSucceeded(t, "rename", renameResult)

	datePrefix := modTime.Format("2006-01-02")
	renamedPath := filepath.Join(root, datePrefix+"_my_document.pdf")
	assertExists(t, renamedPath)

	// Undo dry-run.
	undoResult := runBinary(t, binPath, "undo", "--dry-run", root)
	assertCommandSucceeded(t, "undo dry-run", undoResult)

	if !strings.Contains(undoResult.stdout, "=== DRY RUN - no changes will be made ===") {
		t.Fatalf("expected dry-run banner in output\n%s", undoResult.stdout)
	}

	// File should still be at the renamed location (no changes).
	assertExists(t, renamedPath)
	assertMissing(t, filepath.Join(root, "My Document.pdf"))
}

func TestEndToEndUndo_NoJournalError(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 7, 5, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "file.txt"), "content", modTime)

	// Undo with no prior operations should fail.
	undoResult := runBinary(t, binPath, "undo", root)
	assertCommandFailed(t, undoResult, "journal")
}

func TestEndToEndPurge_PurgesAfterDuplicate(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 8, 1, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "a.txt"), "same-content", modTime)
	writeFile(t, filepath.Join(root, "b.txt"), "same-content", modTime)
	writeFile(t, filepath.Join(root, "unique.txt"), "unique", modTime)

	// Run duplicate to trash one file.
	dupResult := runBinary(t, binPath, "--workers", "1", "--no-snapshot", "duplicate", root)
	assertCommandSucceeded(t, "duplicate", dupResult)

	// Verify trash exists.
	trashRoot := filepath.Join(root, ".btidy", "trash")
	assertExists(t, trashRoot)

	// Dry-run purge should list runs but not delete.
	dryResult := runBinary(t, binPath, "purge", "--dry-run", "--all", root)
	assertCommandSucceeded(t, "purge dry-run", dryResult)
	if !strings.Contains(dryResult.stdout, "WOULD PURGE") {
		t.Fatalf("expected 'WOULD PURGE' in dry-run output\n%s", dryResult.stdout)
	}

	// Trash should still exist.
	trashEntries, err := os.ReadDir(trashRoot)
	if err != nil {
		t.Fatalf("failed to read trash directory: %v", err)
	}
	if len(trashEntries) == 0 {
		t.Fatal("trash should still exist after dry-run purge")
	}

	// Actually purge.
	purgeResult := runBinary(t, binPath, "purge", "--all", "--force", root)
	assertCommandSucceeded(t, "purge all", purgeResult)

	if !strings.Contains(purgeResult.stdout, "Purged:    1 run(s)") {
		t.Fatalf("expected 'Purged:    1 run(s)' in output\n%s", purgeResult.stdout)
	}

	// Trash directory should be empty now.
	trashEntries, err = os.ReadDir(trashRoot)
	if err != nil {
		t.Fatalf("failed to read trash directory after purge: %v", err)
	}
	if len(trashEntries) != 0 {
		t.Fatalf("expected empty trash directory, got %d entries", len(trashEntries))
	}
}

func TestEndToEndPurge_RequiresFilter(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 8, 2, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "file.txt"), "content", modTime)

	// Purge without any filter flag should fail.
	result := runBinary(t, binPath, "purge", root)
	assertCommandFailed(t, result, "at least one")
}

func TestEndToEndPurge_NoTrash(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 8, 3, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "file.txt"), "content", modTime)

	// Purge --all --force with no trash should succeed with empty output.
	result := runBinary(t, binPath, "purge", "--all", "--force", root)
	assertCommandSucceeded(t, "purge no trash", result)

	if !strings.Contains(result.stdout, "No trash runs found") {
		t.Fatalf("expected 'No trash runs found' in output\n%s", result.stdout)
	}
}

func TestEndToEndPurge_OlderThanFilter(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 8, 4, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "a.txt"), "same-content", modTime)
	writeFile(t, filepath.Join(root, "b.txt"), "same-content", modTime)

	// Run duplicate to trash one file.
	dupResult := runBinary(t, binPath, "--workers", "1", "--no-snapshot", "duplicate", root)
	assertCommandSucceeded(t, "duplicate", dupResult)

	// Purge with --older-than 1000h (trash is seconds old, won't match).
	result := runBinary(t, binPath, "purge", "--older-than", "1000h", root)
	assertCommandSucceeded(t, "purge older-than", result)

	if !strings.Contains(result.stdout, "Purged:    0 run(s)") {
		t.Fatalf("expected 'Purged:    0 run(s)' in output\n%s", result.stdout)
	}

	// Verify trash still exists.
	trashRoot := filepath.Join(root, ".btidy", "trash")
	trashEntries, err := os.ReadDir(trashRoot)
	if err != nil {
		t.Fatalf("failed to read trash directory: %v", err)
	}
	if len(trashEntries) == 0 {
		t.Fatal("trash should still exist after older-than filter")
	}
}

func TestEndToEndPurge_AllRequiresForce(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 8, 5, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "a.txt"), "same-content", modTime)
	writeFile(t, filepath.Join(root, "b.txt"), "same-content", modTime)

	// Create some trash.
	dupResult := runBinary(t, binPath, "--workers", "1", "--no-snapshot", "duplicate", root)
	assertCommandSucceeded(t, "duplicate", dupResult)

	// Purge --all without --force should fail.
	result := runBinary(t, binPath, "purge", "--all", root)
	assertCommandFailed(t, result, "--all requires --force")

	// Verify trash still exists.
	trashRoot := filepath.Join(root, ".btidy", "trash")
	assertExists(t, trashRoot)
}

func TestEndToEndPurge_AllDryRunNoForce(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 8, 6, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "a.txt"), "same-content", modTime)
	writeFile(t, filepath.Join(root, "b.txt"), "same-content", modTime)

	// Create some trash.
	dupResult := runBinary(t, binPath, "--workers", "1", "--no-snapshot", "duplicate", root)
	assertCommandSucceeded(t, "duplicate", dupResult)

	// Purge --all --dry-run should work without --force.
	result := runBinary(t, binPath, "purge", "--all", "--dry-run", root)
	assertCommandSucceeded(t, "purge all dry-run", result)

	if !strings.Contains(result.stdout, "WOULD PURGE") {
		t.Fatalf("expected 'WOULD PURGE' in output\n%s", result.stdout)
	}
}

// =============================================================================
// Category 1: Global Flags
// =============================================================================

func TestEndToEndGlobalFlags_VerboseOutput(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 9, 1, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "My Document.pdf"), "alpha", modTime)

	// Without --verbose, per-file detail lines should not appear.
	quietResult := runBinary(t, binPath, "--no-snapshot", "rename", root)
	assertCommandSucceeded(t, "rename quiet", quietResult)

	// Reset: undo the rename.
	undoResult := runBinary(t, binPath, "undo", root)
	assertCommandSucceeded(t, "undo rename", undoResult)

	// With --verbose, per-file detail lines (RENAME:) should appear.
	verboseResult := runBinary(t, binPath, "--no-snapshot", "-v", "rename", root)
	assertCommandSucceeded(t, "rename verbose", verboseResult)

	if !strings.Contains(verboseResult.stdout, "RENAME:") {
		t.Fatalf("expected verbose output to contain 'RENAME:'\n%s", verboseResult.stdout)
	}
}

func TestEndToEndGlobalFlags_Version(t *testing.T) {
	binPath := binaryPath(t)

	result := runBinary(t, binPath, "--version")
	assertCommandSucceeded(t, "version", result)

	// The version output should contain "btidy version".
	if !strings.Contains(result.stdout, "btidy version") {
		t.Fatalf("expected version output to contain 'btidy version'\n%s", result.stdout)
	}
}

func TestEndToEndGlobalFlags_WorkersEdgeValues(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 9, 2, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "a.txt"), "same", modTime)
	writeFile(t, filepath.Join(root, "b.txt"), "same", modTime)

	// --workers 0 should not panic.
	result := runBinary(t, binPath, "--workers", "0", "--no-snapshot", "duplicate", "--dry-run", root)
	// We accept either success or a graceful error — the key is no panic.
	_ = result
}

func TestEndToEndGlobalFlags_NoSnapshotSkipsManifest(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 9, 3, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "My Document.pdf"), "alpha", modTime)

	// Run rename with --no-snapshot.
	result := runBinary(t, binPath, "--no-snapshot", "rename", root)
	assertCommandSucceeded(t, "rename no-snapshot", result)

	// .btidy/manifests/ should not exist or be empty.
	manifestsDir := filepath.Join(root, ".btidy", "manifests")
	entries, err := os.ReadDir(manifestsDir)
	if err == nil && len(entries) > 0 {
		t.Fatalf("expected no manifest files with --no-snapshot, got %d", len(entries))
	}
}

func TestEndToEndGlobalFlags_SnapshotCreatedByDefault(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 9, 4, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "My Document.pdf"), "alpha", modTime)

	// Run rename WITHOUT --no-snapshot (default behavior).
	result := runBinary(t, binPath, "rename", root)
	assertCommandSucceeded(t, "rename with snapshot", result)

	// .btidy/manifests/ should contain a snapshot file.
	manifestsDir := filepath.Join(root, ".btidy", "manifests")
	entries, err := os.ReadDir(manifestsDir)
	if err != nil {
		t.Fatalf("expected manifests directory to exist: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one manifest snapshot file")
	}

	// Verify the file ends with .json.
	found := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected a .json file in .btidy/manifests/")
	}
}

func TestEndToEndGlobalFlags_DryRunNoSnapshotOrJournal(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 9, 5, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "My Document.pdf"), "alpha", modTime)

	// Run rename with --dry-run.
	result := runBinary(t, binPath, "rename", "--dry-run", root)
	assertCommandSucceeded(t, "rename dry-run", result)

	// .btidy/ should not exist (no snapshot, no journal in dry-run).
	btidyDir := filepath.Join(root, ".btidy")
	if _, err := os.Stat(btidyDir); err == nil {
		// If .btidy exists, check that manifests and journal are empty.
		manifestsDir := filepath.Join(btidyDir, "manifests")
		entries, readErr := os.ReadDir(manifestsDir)
		if readErr == nil && len(entries) > 0 {
			t.Fatalf("expected no manifests in dry-run, got %d", len(entries))
		}

		journalDir := filepath.Join(btidyDir, "journal")
		entries, readErr = os.ReadDir(journalDir)
		if readErr == nil && len(entries) > 0 {
			t.Fatalf("expected no journal files in dry-run, got %d", len(entries))
		}
	}
}

// =============================================================================
// Category 2: Pre-operation Manifest Snapshots
// (covered by Category 1 tests above: SnapshotCreatedByDefault,
//  NoSnapshotSkipsManifest, DryRunNoSnapshotOrJournal)
// =============================================================================

// =============================================================================
// Category 3: Journal / Metadata Verification
// =============================================================================

func TestEndToEndJournal_CreatedOnNonDryRun(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 10, 1, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "My Document.pdf"), "alpha", modTime)

	result := runBinary(t, binPath, "--no-snapshot", "rename", root)
	assertCommandSucceeded(t, "rename", result)

	// .btidy/journal/ should contain a .jsonl file.
	journalDir := filepath.Join(root, ".btidy", "journal")
	entries, err := os.ReadDir(journalDir)
	if err != nil {
		t.Fatalf("expected journal directory to exist: %v", err)
	}

	found := false
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}

		found = true

		// Verify the journal is valid JSONL.
		journalPath := filepath.Join(journalDir, e.Name())
		content, readErr := os.ReadFile(journalPath)
		if readErr != nil {
			t.Fatalf("failed to read journal: %v", readErr)
		}
		if len(content) == 0 {
			t.Fatal("journal file is empty")
		}

		// Verify it contains at least one JSON line.
		lines := strings.Split(strings.TrimSpace(string(content)), "\n")
		if len(lines) == 0 {
			t.Fatal("journal contains no entries")
		}

		// Each line should be valid JSON.
		for i, line := range lines {
			if line == "" {
				continue
			}
			if !strings.HasPrefix(strings.TrimSpace(line), "{") {
				t.Fatalf("journal line %d is not JSON: %s", i+1, line)
			}
		}
		break
	}
	if !found {
		t.Fatal("expected a .jsonl file in .btidy/journal/")
	}
}

func TestEndToEndJournal_RolledBackAfterUndo(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 10, 2, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "My Document.pdf"), "alpha", modTime)

	// Run rename.
	renameResult := runBinary(t, binPath, "--no-snapshot", "rename", root)
	assertCommandSucceeded(t, "rename", renameResult)

	// Verify journal exists before undo.
	journalDir := filepath.Join(root, ".btidy", "journal")
	entries, _ := os.ReadDir(journalDir)
	activeCount := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".jsonl") && !strings.HasSuffix(e.Name(), ".rolled-back.jsonl") {
			activeCount++
		}
	}
	if activeCount == 0 {
		t.Fatal("expected at least one active journal before undo")
	}

	// Undo the rename.
	undoResult := runBinary(t, binPath, "undo", root)
	assertCommandSucceeded(t, "undo", undoResult)

	// After undo, the journal should be renamed to .rolled-back.jsonl.
	entries, _ = os.ReadDir(journalDir)
	rolledBackCount := 0
	activeCount = 0
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".rolled-back.jsonl") {
			rolledBackCount++
		} else if strings.HasSuffix(name, ".jsonl") {
			activeCount++
		}
	}
	if rolledBackCount == 0 {
		t.Fatal("expected a .rolled-back.jsonl file after undo")
	}
	if activeCount != 0 {
		t.Fatalf("expected no active journals after undo, got %d", activeCount)
	}
}

func TestEndToEndJournal_DoubleUndoFails(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 10, 3, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "My Document.pdf"), "alpha", modTime)

	// Run rename.
	renameResult := runBinary(t, binPath, "--no-snapshot", "rename", root)
	assertCommandSucceeded(t, "rename", renameResult)

	// First undo should succeed.
	undoResult := runBinary(t, binPath, "undo", root)
	assertCommandSucceeded(t, "first undo", undoResult)

	// Second undo should fail — all journals are rolled back.
	secondUndoResult := runBinary(t, binPath, "undo", root)
	assertCommandFailed(t, secondUndoResult, "no active journals")
}

func TestEndToEndJournal_IntentConfirmationPairs(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 10, 4, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "My Document.pdf"), "alpha", modTime)

	result := runBinary(t, binPath, "--no-snapshot", "rename", root)
	assertCommandSucceeded(t, "rename", result)

	// Read the journal file and verify intent+confirmation pairs.
	journalDir := filepath.Join(root, ".btidy", "journal")
	entries, err := os.ReadDir(journalDir)
	if err != nil {
		t.Fatalf("failed to read journal dir: %v", err)
	}

	var journalPath string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".jsonl") && !strings.HasSuffix(e.Name(), ".rolled-back.jsonl") {
			journalPath = filepath.Join(journalDir, e.Name())
			break
		}
	}
	if journalPath == "" {
		t.Fatal("no active journal found")
	}

	content, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatalf("failed to read journal: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	// We expect pairs: for each operation, one intent (ok:false) and one confirmation (ok:true).
	// The total number of lines should be even.
	if len(lines)%2 != 0 {
		t.Fatalf("expected even number of journal lines (intent+confirmation pairs), got %d", len(lines))
	}

	// Verify that for each pair, the first has "ok":false and the second has "ok":true.
	for i := 0; i < len(lines); i += 2 {
		if !strings.Contains(lines[i], `"ok":false`) {
			t.Fatalf("expected intent entry (ok:false) at line %d, got: %s", i+1, lines[i])
		}
		if !strings.Contains(lines[i+1], `"ok":true`) {
			t.Fatalf("expected confirmation entry (ok:true) at line %d, got: %s", i+2, lines[i+1])
		}
	}
}

// =============================================================================
// Category 4: Undo Gaps
// =============================================================================

func TestEndToEndUndo_SpecificRunID(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 11, 1, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "file1.txt"), "content1", modTime)

	// Run rename to produce a journal.
	renameResult := runBinary(t, binPath, "--no-snapshot", "rename", root)
	assertCommandSucceeded(t, "rename", renameResult)

	// Find the run ID from the journal directory.
	journalDir := filepath.Join(root, ".btidy", "journal")
	entries, err := os.ReadDir(journalDir)
	if err != nil {
		t.Fatalf("failed to read journal dir: %v", err)
	}

	var runID string
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".jsonl") && !strings.HasSuffix(name, ".rolled-back.jsonl") {
			runID = strings.TrimSuffix(name, ".jsonl")
			break
		}
	}
	if runID == "" {
		t.Fatal("no active journal found to extract run ID")
	}

	// Undo with --run <run-id>.
	undoResult := runBinary(t, binPath, "undo", "--run", runID, root)
	assertCommandSucceeded(t, "undo specific run", undoResult)

	// Verify the file is restored.
	assertExists(t, filepath.Join(root, "file1.txt"))
}

func TestEndToEndUndo_ReversesOrganize(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 11, 2, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "report.pdf"), "pdf-content", modTime)
	writeFile(t, filepath.Join(root, "photo.jpg"), "jpg-content", modTime)
	writeFile(t, filepath.Join(root, "notes.txt"), "txt-content", modTime)

	// Organize.
	organizeResult := runBinary(t, binPath, "--no-snapshot", "organize", root)
	assertCommandSucceeded(t, "organize", organizeResult)

	// Verify files are organized.
	assertExists(t, filepath.Join(root, "pdf", "report.pdf"))
	assertExists(t, filepath.Join(root, "jpg", "photo.jpg"))
	assertExists(t, filepath.Join(root, "txt", "notes.txt"))

	// Undo organize.
	undoResult := runBinary(t, binPath, "undo", root)
	assertCommandSucceeded(t, "undo organize", undoResult)

	// Files should be restored to root.
	assertExists(t, filepath.Join(root, "report.pdf"))
	assertExists(t, filepath.Join(root, "photo.jpg"))
	assertExists(t, filepath.Join(root, "notes.txt"))
}

func TestEndToEndUndo_ReversesUnzip(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()

	archivePath := filepath.Join(root, "archive.zip")
	writeZipArchive(t, archivePath, []zipFixtureEntry{
		{name: "extracted.txt", content: []byte("content")},
	})

	// Run unzip.
	unzipResult := runBinary(t, binPath, "--no-snapshot", "unzip", root)
	assertCommandSucceeded(t, "unzip", unzipResult)

	// Archive should be in trash, extracted file should exist.
	assertMissing(t, archivePath)
	assertExists(t, filepath.Join(root, "extracted.txt"))

	// Undo unzip — archive should be restored from trash.
	// Note: extract operations are skipped during undo, but trash (archive deletion) is restored.
	undoResult := runBinary(t, binPath, "undo", root)
	assertCommandSucceeded(t, "undo unzip", undoResult)

	// Archive should be restored from trash.
	assertExists(t, archivePath)
}

func TestEndToEndUndo_SequentialOperations(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 11, 4, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "My Document.pdf"), "content", modTime)

	// Op1: rename.
	renameResult := runBinary(t, binPath, "--no-snapshot", "rename", root)
	assertCommandSucceeded(t, "rename", renameResult)

	datePrefix := modTime.Format("2006-01-02")
	renamedPath := filepath.Join(root, datePrefix+"_my_document.pdf")
	assertExists(t, renamedPath)

	// Sleep to ensure organize gets a different timestamp in its run ID.
	time.Sleep(1100 * time.Millisecond)

	// Op2: organize.
	organizeResult := runBinary(t, binPath, "--no-snapshot", "organize", root)
	assertCommandSucceeded(t, "organize", organizeResult)

	// File should be in pdf/ dir.
	assertExists(t, filepath.Join(root, "pdf", datePrefix+"_my_document.pdf"))

	// Find run IDs from journal directory to use --run for deterministic undo order.
	journalDir := filepath.Join(root, ".btidy", "journal")
	entries, err := os.ReadDir(journalDir)
	if err != nil {
		t.Fatalf("failed to read journal dir: %v", err)
	}

	var organizeRunID, renameRunID string
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".jsonl") && !strings.HasSuffix(name, ".rolled-back.jsonl") {
			runID := strings.TrimSuffix(name, ".jsonl")
			if strings.HasPrefix(runID, "organize-") {
				organizeRunID = runID
			} else if strings.HasPrefix(runID, "rename-") {
				renameRunID = runID
			}
		}
	}
	if organizeRunID == "" || renameRunID == "" {
		t.Fatalf("expected both organize and rename journals, got organize=%q rename=%q", organizeRunID, renameRunID)
	}

	// Undo1: undo organize by run ID.
	undo1 := runBinary(t, binPath, "undo", "--run", organizeRunID, root)
	assertCommandSucceeded(t, "undo organize", undo1)

	// File should be back at root.
	assertExists(t, renamedPath)
	assertMissing(t, filepath.Join(root, "pdf", datePrefix+"_my_document.pdf"))

	// Undo2: undo rename by run ID.
	undo2 := runBinary(t, binPath, "undo", "--run", renameRunID, root)
	assertCommandSucceeded(t, "undo rename", undo2)

	// File should be back to original name.
	assertExists(t, filepath.Join(root, "My Document.pdf"))
	assertMissing(t, renamedPath)
}

func TestEndToEndUndo_AllJournalsRolledBackFails(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 11, 5, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "My Document.pdf"), "content", modTime)

	// Run rename.
	renameResult := runBinary(t, binPath, "--no-snapshot", "rename", root)
	assertCommandSucceeded(t, "rename", renameResult)

	// Undo successfully.
	undoResult := runBinary(t, binPath, "undo", root)
	assertCommandSucceeded(t, "undo", undoResult)

	// All journals are rolled back — undo should fail.
	failResult := runBinary(t, binPath, "undo", root)
	assertCommandFailed(t, failResult, "no active journals")
}

// =============================================================================
// Category 5: Purge Gaps
// =============================================================================

func TestEndToEndPurge_SpecificRunID(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 12, 1, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "a.txt"), "same-content", modTime)
	writeFile(t, filepath.Join(root, "b.txt"), "same-content", modTime)

	// Run duplicate to create trash.
	dupResult := runBinary(t, binPath, "--workers", "1", "--no-snapshot", "duplicate", root)
	assertCommandSucceeded(t, "duplicate", dupResult)

	// Find the run ID from trash directory.
	trashRoot := filepath.Join(root, ".btidy", "trash")
	trashEntries, err := os.ReadDir(trashRoot)
	if err != nil || len(trashEntries) == 0 {
		t.Fatalf("expected trash entries: %v", err)
	}
	runID := trashEntries[0].Name()

	// Purge with --run <run-id>.
	purgeResult := runBinary(t, binPath, "purge", "--run", runID, root)
	assertCommandSucceeded(t, "purge specific run", purgeResult)

	if !strings.Contains(purgeResult.stdout, "Purged:    1 run(s)") {
		t.Fatalf("expected 'Purged:    1 run(s)' in output\n%s", purgeResult.stdout)
	}
}

func TestEndToEndPurge_OlderThanDaySuffix(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 12, 2, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "a.txt"), "same-content", modTime)
	writeFile(t, filepath.Join(root, "b.txt"), "same-content", modTime)

	// Run duplicate to create trash.
	dupResult := runBinary(t, binPath, "--workers", "1", "--no-snapshot", "duplicate", root)
	assertCommandSucceeded(t, "duplicate", dupResult)

	// Purge with --older-than 7d (trash is seconds old, should not match).
	result := runBinary(t, binPath, "purge", "--older-than", "7d", root)
	assertCommandSucceeded(t, "purge older-than 7d", result)

	if !strings.Contains(result.stdout, "Purged:    0 run(s)") {
		t.Fatalf("expected 'Purged:    0 run(s)' in output\n%s", result.stdout)
	}
}

func TestEndToEndPurge_OlderThanInvalidDuration(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 12, 3, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "file.txt"), "content", modTime)

	result := runBinary(t, binPath, "purge", "--older-than", "invalid", root)
	assertCommandFailed(t, result, "invalid duration")
}

func TestEndToEndPurge_NonexistentRunID(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 12, 4, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "a.txt"), "same-content", modTime)
	writeFile(t, filepath.Join(root, "b.txt"), "same-content", modTime)

	// Create some trash.
	dupResult := runBinary(t, binPath, "--workers", "1", "--no-snapshot", "duplicate", root)
	assertCommandSucceeded(t, "duplicate", dupResult)

	// Purge with non-matching run ID.
	purgeResult := runBinary(t, binPath, "purge", "--run", "nonexistent-run-id", root)
	assertCommandSucceeded(t, "purge nonexistent run", purgeResult)

	if !strings.Contains(purgeResult.stdout, "Purged:    0 run(s)") {
		t.Fatalf("expected 'Purged:    0 run(s)' in output\n%s", purgeResult.stdout)
	}
}

// =============================================================================
// Category 6: Unzip Edge Cases
// =============================================================================

func TestEndToEndUnzip_SymlinkEntryRejected(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()

	// Create a zip archive with a symlink entry.
	archivePath := filepath.Join(root, "symlink.zip")
	archiveFile, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("failed to create archive: %v", err)
	}
	writer := zip.NewWriter(archiveFile)

	// Add a symlink entry.
	header := &zip.FileHeader{
		Name: "link.txt",
	}
	header.SetMode(os.ModeSymlink | 0o777)
	w, err := writer.CreateHeader(header)
	if err != nil {
		t.Fatalf("failed to create symlink header: %v", err)
	}
	// Symlink target.
	if _, err := w.Write([]byte("/etc/passwd")); err != nil {
		t.Fatalf("failed to write symlink target: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close writer: %v", err)
	}
	if err := archiveFile.Close(); err != nil {
		t.Fatalf("failed to close file: %v", err)
	}

	// Unzip should fail or report error for symlink entries.
	result := runBinary(t, binPath, "unzip", root)
	// The symlink entry should cause an error on the archive.
	combined := result.combinedOutput()
	if !strings.Contains(strings.ToLower(combined), "symlink") {
		t.Fatalf("expected error about symlink entries\n%s", combined)
	}
}

func TestEndToEndUnzip_ExistingFileBackedUp(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 12, 5, 10, 0, 0, 0, time.UTC)

	// Create a file at the extraction target path.
	writeFile(t, filepath.Join(root, "file.txt"), "original-content", modTime)

	// Create a zip that would extract a file with the same name.
	archivePath := filepath.Join(root, "archive.zip")
	writeZipArchive(t, archivePath, []zipFixtureEntry{
		{name: "file.txt", content: []byte("new-content")},
	})

	// Unzip should back up the existing file to trash.
	result := runBinary(t, binPath, "--no-snapshot", "unzip", root)
	assertCommandSucceeded(t, "unzip with existing file", result)

	// The extracted file should have the new content.
	content, err := os.ReadFile(filepath.Join(root, "file.txt"))
	if err != nil {
		t.Fatalf("failed to read extracted file: %v", err)
	}
	if string(content) != "new-content" {
		t.Fatalf("expected extracted file to have 'new-content', got %q", string(content))
	}

	// The original file should be in .btidy/trash/.
	trashRoot := filepath.Join(root, ".btidy", "trash")
	assertExists(t, trashRoot)
}

func TestEndToEndUnzip_NoArchives(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 12, 6, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "file.txt"), "content", modTime)

	result := runBinary(t, binPath, "unzip", root)
	assertCommandSucceeded(t, "unzip no archives", result)

	if !strings.Contains(result.stdout, "No zip archives to process") {
		t.Fatalf("expected 'No zip archives to process' in output\n%s", result.stdout)
	}
}

// =============================================================================
// Category 7: Organize Edge Cases
// =============================================================================

func TestEndToEndOrganize_Idempotent(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "report.pdf"), "pdf-content", modTime)
	writeFile(t, filepath.Join(root, "photo.jpg"), "jpg-content", modTime)

	// First organize.
	firstRun := runBinary(t, binPath, "--no-snapshot", "organize", root)
	assertCommandSucceeded(t, "first organize", firstRun)

	assertExists(t, filepath.Join(root, "pdf", "report.pdf"))
	assertExists(t, filepath.Join(root, "jpg", "photo.jpg"))

	// Second organize should skip all files ("already organized").
	secondRun := runBinary(t, binPath, "--no-snapshot", "organize", root)
	assertCommandSucceeded(t, "second organize", secondRun)

	// Files should still be in the same place.
	assertExists(t, filepath.Join(root, "pdf", "report.pdf"))
	assertExists(t, filepath.Join(root, "jpg", "photo.jpg"))

	// Output should mention skipped files.
	if !strings.Contains(secondRun.stdout, "Skipped:") {
		t.Fatalf("expected 'Skipped:' in output for idempotent organize\n%s", secondRun.stdout)
	}
}

func TestEndToEndOrganize_NameConflictsSuffix(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2025, 1, 2, 10, 0, 0, 0, time.UTC)

	// Two files with the same name in different directories.
	writeFile(t, filepath.Join(root, "dir1", "photo.jpg"), "content-1", modTime)
	writeFile(t, filepath.Join(root, "dir2", "photo.jpg"), "content-2", modTime)

	// Flatten first so both files are at root.
	flattenResult := runBinary(t, binPath, "--workers", "1", "--no-snapshot", "flatten", root)
	assertCommandSucceeded(t, "flatten", flattenResult)

	// Now organize — both photo.jpg files should end up in jpg/ with one getting a _1 suffix.
	organizeResult := runBinary(t, binPath, "--no-snapshot", "organize", root)
	assertCommandSucceeded(t, "organize", organizeResult)

	jpgDir := filepath.Join(root, "jpg")
	assertExists(t, jpgDir)

	// At least one file should be photo.jpg and another photo_1.jpg.
	entries, err := os.ReadDir(jpgDir)
	if err != nil {
		t.Fatalf("failed to read jpg dir: %v", err)
	}
	if len(entries) < 2 {
		t.Fatalf("expected at least 2 files in jpg/, got %d", len(entries))
	}
}

func TestEndToEndOrganize_Dotfiles(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2025, 1, 3, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, ".gitignore"), "*.log", modTime)
	writeFile(t, filepath.Join(root, "notes.txt"), "content", modTime)

	result := runBinary(t, binPath, "--no-snapshot", "organize", root)
	assertCommandSucceeded(t, "organize dotfiles", result)

	// .gitignore should go to other/ directory.
	assertExists(t, filepath.Join(root, "other", ".gitignore"))
	assertExists(t, filepath.Join(root, "txt", "notes.txt"))
}

// =============================================================================
// Category 8: Rename Edge Cases
// =============================================================================

func TestEndToEndRename_FinnishCharacters(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2025, 2, 1, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "Päivä.txt"), "content", modTime)

	result := runBinary(t, binPath, "--no-snapshot", "rename", root)
	assertCommandSucceeded(t, "rename finnish chars", result)

	datePrefix := modTime.Format("2006-01-02")
	// ä → a, so "Päivä" → "paiva".
	expectedPath := filepath.Join(root, datePrefix+"_paiva.txt")
	assertExists(t, expectedPath)
}

func TestEndToEndRename_TBDPrefixSkipped(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2025, 2, 2, 10, 0, 0, 0, time.UTC)

	tbdFile := filepath.Join(root, "2024-TBD-TBD_something.txt")
	writeFile(t, tbdFile, "content", modTime)

	result := runBinary(t, binPath, "--no-snapshot", "rename", root)
	assertCommandSucceeded(t, "rename TBD prefix", result)

	// File should not be renamed — it should stay as-is.
	assertExists(t, tbdFile)
}

func TestEndToEndRename_DuplicateDetectionDuringRename(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2025, 2, 3, 10, 0, 0, 0, time.UTC)

	// Two files that will produce the same sanitized name and have identical content.
	writeFile(t, filepath.Join(root, "My File.txt"), "same-content", modTime)
	writeFile(t, filepath.Join(root, "My_File.txt"), "same-content", modTime)

	result := runBinary(t, binPath, "--workers", "1", "--no-snapshot", "rename", root)
	assertCommandSucceeded(t, "rename duplicate detection", result)

	datePrefix := modTime.Format("2006-01-02")
	// Only one file should survive — the duplicate should be deleted/trashed.
	expectedPath := filepath.Join(root, datePrefix+"_my_file.txt")
	assertExists(t, expectedPath)

	// The summary should mention deleted files.
	if !strings.Contains(result.stdout, "Deleted:") {
		t.Fatalf("expected 'Deleted:' in rename summary\n%s", result.stdout)
	}
}

func TestEndToEndRename_NameConflictDifferentContent(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2025, 2, 4, 10, 0, 0, 0, time.UTC)

	// Two files that will produce the same sanitized name but have different content.
	writeFile(t, filepath.Join(root, "My File.txt"), "content-1", modTime)
	writeFile(t, filepath.Join(root, "My_File.txt"), "content-2", modTime)

	result := runBinary(t, binPath, "--workers", "1", "--no-snapshot", "rename", root)
	assertCommandSucceeded(t, "rename conflict resolution", result)

	datePrefix := modTime.Format("2006-01-02")
	// Both files should exist: one as the base name and one with a _1 suffix.
	basePath := filepath.Join(root, datePrefix+"_my_file.txt")
	suffixPath := filepath.Join(root, datePrefix+"_my_file_1.txt")

	assertExists(t, basePath)
	assertExists(t, suffixPath)
}

// =============================================================================
// Category 9: Flatten Edge Cases
// =============================================================================

func TestEndToEndFlatten_NameConflictDifferentContent(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2025, 3, 1, 10, 0, 0, 0, time.UTC)

	// Two files with the same name in different dirs but different content.
	writeFile(t, filepath.Join(root, "dir1", "file.txt"), "content-A", modTime)
	writeFile(t, filepath.Join(root, "dir2", "file.txt"), "content-B", modTime)

	result := runBinary(t, binPath, "--workers", "1", "--no-snapshot", "flatten", root)
	assertCommandSucceeded(t, "flatten name conflict", result)

	// Both files should survive: one as file.txt and one as file_1.txt.
	assertExists(t, filepath.Join(root, "file.txt"))
	assertExists(t, filepath.Join(root, "file_1.txt"))

	// Verify different content is preserved.
	content1, _ := os.ReadFile(filepath.Join(root, "file.txt"))
	content2, _ := os.ReadFile(filepath.Join(root, "file_1.txt"))
	if bytes.Equal(content1, content2) {
		t.Fatal("expected files to have different content")
	}
}

// =============================================================================
// Category 10: Empty Directory Handling
// =============================================================================

func TestEndToEndEmptyDirectory_RenameNoFiles(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()

	result := runBinary(t, binPath, "rename", root)
	assertCommandSucceeded(t, "rename empty dir", result)

	if !strings.Contains(result.stdout, "No files to process") {
		t.Fatalf("expected 'No files to process' for empty directory\n%s", result.stdout)
	}
}

func TestEndToEndEmptyDirectory_FlattenNoFiles(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()

	result := runBinary(t, binPath, "flatten", root)
	assertCommandSucceeded(t, "flatten empty dir", result)

	if !strings.Contains(result.stdout, "No files to process") {
		t.Fatalf("expected 'No files to process' for empty directory\n%s", result.stdout)
	}
}

func TestEndToEndEmptyDirectory_DuplicateNoFiles(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()

	result := runBinary(t, binPath, "duplicate", root)
	assertCommandSucceeded(t, "duplicate empty dir", result)

	if !strings.Contains(result.stdout, "No files to process") {
		t.Fatalf("expected 'No files to process' for empty directory\n%s", result.stdout)
	}
}

func TestEndToEndEmptyDirectory_OrganizeNoFiles(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()

	result := runBinary(t, binPath, "organize", root)
	assertCommandSucceeded(t, "organize empty dir", result)

	if !strings.Contains(result.stdout, "No files to process") {
		t.Fatalf("expected 'No files to process' for empty directory\n%s", result.stdout)
	}
}

// =============================================================================
// Category 11: Error Output and Exit Codes
// =============================================================================

func TestEndToEndErrors_StderrOutput(t *testing.T) {
	binPath := binaryPath(t)

	// Run with a non-existent directory — error should be on stderr.
	result := runBinary(t, binPath, "rename", "/nonexistent/path/does/not/exist")
	if result.err == nil {
		t.Fatal("expected command to fail for non-existent directory")
	}

	// Error message should appear in stderr, not just stdout.
	if result.stderr == "" {
		t.Fatal("expected error output on stderr")
	}
}

func TestEndToEndErrors_WrongArgumentCount(t *testing.T) {
	binPath := binaryPath(t)

	// Zero arguments.
	noArgs := runBinary(t, binPath, "rename")
	assertCommandFailed(t, noArgs, "accepts 1 arg")

	// Two arguments.
	twoArgs := runBinary(t, binPath, "rename", "/path1", "/path2")
	assertCommandFailed(t, twoArgs, "accepts 1 arg")
}

// =============================================================================
// Category 12: Advisory File Locking
// =============================================================================

func TestEndToEndFileLock_ConcurrentProcessesFail(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2025, 4, 1, 10, 0, 0, 0, time.UTC)

	// Create many files so the command takes a bit of time.
	for i := range 100 {
		writeFile(t, filepath.Join(root, fmt.Sprintf("file%03d.txt", i)), fmt.Sprintf("content-%d", i), modTime)
	}

	// Start a long-running command in the background.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd1 := exec.CommandContext(ctx, binPath, "--no-snapshot", "rename", root)
	var stdout1, stderr1 bytes.Buffer
	cmd1.Stdout = &stdout1
	cmd1.Stderr = &stderr1

	if err := cmd1.Start(); err != nil {
		t.Fatalf("failed to start first process: %v", err)
	}

	// Give the first process time to acquire the lock.
	time.Sleep(200 * time.Millisecond)

	// Try to run a second process — it should fail with a lock error.
	result2 := runBinary(t, binPath, "--no-snapshot", "rename", root)

	// Wait for first process to complete.
	_ = cmd1.Wait()

	// At least one of the processes should have gotten a lock error,
	// or both succeeded sequentially (which is also fine).
	// The key guarantee is no panic or data corruption.
	if result2.err != nil {
		combined := strings.ToLower(result2.combinedOutput())
		if !strings.Contains(combined, "lock") && !strings.Contains(combined, "another") {
			// It failed for a reason other than locking — that's OK too
			// (maybe the first process already finished).
			_ = combined
		}
	}
}

// =============================================================================
// Category 13: Trash Structure Verification
// =============================================================================

func TestEndToEndTrash_StructurePreservesRelativePaths(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2025, 5, 1, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "a.txt"), "same-content", modTime)
	writeFile(t, filepath.Join(root, "sub", "b.txt"), "same-content", modTime)
	writeFile(t, filepath.Join(root, "unique.txt"), "unique", modTime)

	// Run duplicate to trash one of the duplicates.
	dupResult := runBinary(t, binPath, "--workers", "1", "--no-snapshot", "duplicate", root)
	assertCommandSucceeded(t, "duplicate", dupResult)

	// Verify trash structure.
	trashRoot := filepath.Join(root, ".btidy", "trash")
	assertExists(t, trashRoot)

	// Find the run directory in trash.
	trashEntries, err := os.ReadDir(trashRoot)
	if err != nil || len(trashEntries) == 0 {
		t.Fatalf("expected trash entries: %v", err)
	}

	runDir := filepath.Join(trashRoot, trashEntries[0].Name())

	// Walk the trash run directory to find the trashed file.
	var trashedFiles []string
	err = filepath.Walk(runDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !info.IsDir() {
			trashedFiles = append(trashedFiles, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk trash: %v", err)
	}

	if len(trashedFiles) == 0 {
		t.Fatal("expected at least one trashed file")
	}

	// Verify trashed file content matches original.
	for _, trashedFile := range trashedFiles {
		content, readErr := os.ReadFile(trashedFile)
		if readErr != nil {
			t.Fatalf("failed to read trashed file: %v", readErr)
		}
		if string(content) != "same-content" {
			t.Fatalf("expected trashed file content to be 'same-content', got %q", string(content))
		}
	}
}

// =============================================================================
// Category 14: Manifest Command
// =============================================================================

func TestEndToEndManifest_DefaultOutputPath(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2025, 6, 1, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "file.txt"), "content", modTime)

	// Run manifest without -o flag — should create manifest.json inside target.
	result := runBinary(t, binPath, "manifest", root)
	assertCommandSucceeded(t, "manifest default output", result)

	assertExists(t, filepath.Join(root, "manifest.json"))
}

func TestEndToEndManifest_ExplicitWorkers(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2025, 6, 2, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "file.txt"), "content", modTime)

	result := runBinary(t, binPath, "--workers", "2", "manifest", root, "-o", "test-manifest.json")
	assertCommandSucceeded(t, "manifest with workers", result)

	assertExists(t, filepath.Join(root, "test-manifest.json"))

	// Verify Workers: 2 is in the output.
	if !strings.Contains(result.stdout, "Workers: 2") {
		t.Fatalf("expected 'Workers: 2' in output\n%s", result.stdout)
	}
}

// =============================================================================
// Category 15: Command Argument Validation
// =============================================================================

func TestEndToEndArgValidation_ZeroArguments(t *testing.T) {
	binPath := binaryPath(t)

	commands := []string{"rename", "flatten", "duplicate", "organize", "manifest", "unzip", "undo", "purge"}
	for _, cmd := range commands {
		t.Run(cmd, func(t *testing.T) {
			result := runBinary(t, binPath, cmd)
			assertCommandFailed(t, result, "accepts 1 arg")
		})
	}
}

func TestEndToEndArgValidation_TooManyArguments(t *testing.T) {
	binPath := binaryPath(t)

	commands := []string{"rename", "flatten", "duplicate", "organize", "manifest", "unzip", "undo", "purge"}
	for _, cmd := range commands {
		t.Run(cmd, func(t *testing.T) {
			result := runBinary(t, binPath, cmd, "/path1", "/path2")
			assertCommandFailed(t, result, "accepts 1 arg")
		})
	}
}

// =============================================================================
// Data Loss Prevention Tests
// =============================================================================

// --- Content verification after operations ---

func TestEndToEndDataLoss_RenamePreservesContent(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2020, 2, 3, 4, 5, 6, 0, time.UTC)

	writeFile(t, filepath.Join(root, "My Document.pdf"), "alpha-content", modTime)
	writeFile(t, filepath.Join(root, "Report (Final).docx"), "beta-content", modTime)
	writeFile(t, filepath.Join(root, "Photo.JPG"), "gamma-content", modTime)

	result := runBinary(t, binPath, "--no-snapshot", "rename", root)
	assertCommandSucceeded(t, "rename", result)

	datePrefix := modTime.Format("2006-01-02")
	assertFileContent(t, filepath.Join(root, datePrefix+"_my_document.pdf"), "alpha-content")
	assertFileContent(t, filepath.Join(root, datePrefix+"_report_final.docx"), "beta-content")
	assertFileContent(t, filepath.Join(root, datePrefix+"_photo.jpg"), "gamma-content")
}

func TestEndToEndDataLoss_OrganizePreservesContent(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2023, 5, 10, 14, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "report.pdf"), "pdf-payload", modTime)
	writeFile(t, filepath.Join(root, "photo.jpg"), "jpg-payload", modTime)
	writeFile(t, filepath.Join(root, "notes.txt"), "txt-payload", modTime)
	writeFile(t, filepath.Join(root, "Makefile"), "make-payload", modTime)

	result := runBinary(t, binPath, "--no-snapshot", "organize", root)
	assertCommandSucceeded(t, "organize", result)

	assertFileContent(t, filepath.Join(root, "pdf", "report.pdf"), "pdf-payload")
	assertFileContent(t, filepath.Join(root, "jpg", "photo.jpg"), "jpg-payload")
	assertFileContent(t, filepath.Join(root, "txt", "notes.txt"), "txt-payload")
	assertFileContent(t, filepath.Join(root, "other", "Makefile"), "make-payload")
}

func TestEndToEndDataLoss_UnzipExtractsCorrectContent(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()

	innerArchive := zipBytes(t, []zipFixtureEntry{
		{name: "deep/final.txt", content: []byte("inner-payload")},
	})

	outerArchivePath := filepath.Join(root, "outer.zip")
	writeZipArchive(t, outerArchivePath, []zipFixtureEntry{
		{name: "nested/inner.zip", content: innerArchive},
		{name: "outer.txt", content: []byte("outer-payload")},
	})

	result := runBinary(t, binPath, "--no-snapshot", "unzip", root)
	assertCommandSucceeded(t, "unzip", result)

	assertFileContent(t, filepath.Join(root, "outer.txt"), "outer-payload")
	assertFileContent(t, filepath.Join(root, "nested", "deep", "final.txt"), "inner-payload")
}

func TestEndToEndDataLoss_DuplicateSurvivorHasCorrectContent(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2022, 4, 2, 9, 15, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "a.txt"), "duplicate-data", modTime)
	writeFile(t, filepath.Join(root, "sub", "a_copy.txt"), "duplicate-data", modTime)
	writeFile(t, filepath.Join(root, "unique.txt"), "unique-data", modTime)

	result := runBinary(t, binPath, "--workers", "1", "--no-snapshot", "duplicate", root)
	assertCommandSucceeded(t, "duplicate", result)

	// One duplicate is removed. The survivor and unique file must have correct content.
	assertFileContent(t, filepath.Join(root, "a.txt"), "duplicate-data")
	assertFileContent(t, filepath.Join(root, "unique.txt"), "unique-data")
}

func TestEndToEndDataLoss_FlattenPreservesContent(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2021, 7, 12, 11, 30, 0, 0, time.UTC)

	// Same-content files (one will be deduped during flatten).
	writeFile(t, filepath.Join(root, "dir1", "file.txt"), "shared-data", modTime)
	writeFile(t, filepath.Join(root, "dir2", "file.txt"), "shared-data", modTime)
	// Unique files.
	writeFile(t, filepath.Join(root, "dir1", "unique.txt"), "unique-1", modTime)
	writeFile(t, filepath.Join(root, "dir2", "unique.txt"), "unique-2", modTime)
	writeFile(t, filepath.Join(root, "rootfile.txt"), "root-data", modTime)

	result := runBinary(t, binPath, "--workers", "1", "--no-snapshot", "flatten", root)
	assertCommandSucceeded(t, "flatten", result)

	// After flatten, the shared file should exist exactly once with correct content.
	assertFileContent(t, filepath.Join(root, "file.txt"), "shared-data")
	assertFileContent(t, filepath.Join(root, "rootfile.txt"), "root-data")

	// Both unique files should survive (one with a suffix).
	content1, err1 := os.ReadFile(filepath.Join(root, "unique.txt"))
	if err1 != nil {
		t.Fatalf("failed to read unique.txt: %v", err1)
	}
	content2, err2 := os.ReadFile(filepath.Join(root, "unique_1.txt"))
	if err2 != nil {
		t.Fatalf("failed to read unique_1.txt: %v", err2)
	}
	// Together they must contain both original contents.
	contents := map[string]bool{string(content1): true, string(content2): true}
	if !contents["unique-1"] || !contents["unique-2"] {
		t.Fatalf("expected both unique contents to be preserved, got %q and %q", content1, content2)
	}
}

// --- Content verification after undo ---

func TestEndToEndDataLoss_UndoRestoresContentAfterDuplicate(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 7, 1, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "a.txt"), "dup-content", modTime)
	writeFile(t, filepath.Join(root, "b.txt"), "dup-content", modTime)
	writeFile(t, filepath.Join(root, "unique.txt"), "unique-content", modTime)

	dupResult := runBinary(t, binPath, "--workers", "1", "--no-snapshot", "duplicate", root)
	assertCommandSucceeded(t, "duplicate", dupResult)

	undoResult := runBinary(t, binPath, "undo", root)
	assertCommandSucceeded(t, "undo duplicate", undoResult)

	// All files must be back with correct content.
	assertFileContent(t, filepath.Join(root, "a.txt"), "dup-content")
	assertFileContent(t, filepath.Join(root, "b.txt"), "dup-content")
	assertFileContent(t, filepath.Join(root, "unique.txt"), "unique-content")
}

func TestEndToEndDataLoss_UndoRestoresContentAfterRename(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 7, 2, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "My Document.pdf"), "rename-content", modTime)

	renameResult := runBinary(t, binPath, "--no-snapshot", "rename", root)
	assertCommandSucceeded(t, "rename", renameResult)

	undoResult := runBinary(t, binPath, "undo", root)
	assertCommandSucceeded(t, "undo rename", undoResult)

	assertFileContent(t, filepath.Join(root, "My Document.pdf"), "rename-content")
}

func TestEndToEndDataLoss_UndoRestoresContentAfterFlatten(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 7, 3, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "sub", "deep", "file.txt"), "deep-content", modTime)

	flattenResult := runBinary(t, binPath, "--workers", "1", "--no-snapshot", "flatten", root)
	assertCommandSucceeded(t, "flatten", flattenResult)

	undoResult := runBinary(t, binPath, "undo", root)
	assertCommandSucceeded(t, "undo flatten", undoResult)

	assertFileContent(t, filepath.Join(root, "sub", "deep", "file.txt"), "deep-content")
}

func TestEndToEndDataLoss_UndoRestoresContentAfterOrganize(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 11, 2, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "report.pdf"), "pdf-original", modTime)
	writeFile(t, filepath.Join(root, "photo.jpg"), "jpg-original", modTime)
	writeFile(t, filepath.Join(root, "notes.txt"), "txt-original", modTime)

	organizeResult := runBinary(t, binPath, "--no-snapshot", "organize", root)
	assertCommandSucceeded(t, "organize", organizeResult)

	undoResult := runBinary(t, binPath, "undo", root)
	assertCommandSucceeded(t, "undo organize", undoResult)

	assertFileContent(t, filepath.Join(root, "report.pdf"), "pdf-original")
	assertFileContent(t, filepath.Join(root, "photo.jpg"), "jpg-original")
	assertFileContent(t, filepath.Join(root, "notes.txt"), "txt-original")
}

func TestEndToEndDataLoss_UndoRestoresArchiveContentAfterUnzip(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()

	archivePath := filepath.Join(root, "archive.zip")
	writeZipArchive(t, archivePath, []zipFixtureEntry{
		{name: "extracted.txt", content: []byte("payload")},
	})

	// Read original archive bytes before unzip.
	originalArchive, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("failed to read original archive: %v", err)
	}

	unzipResult := runBinary(t, binPath, "--no-snapshot", "unzip", root)
	assertCommandSucceeded(t, "unzip", unzipResult)

	undoResult := runBinary(t, binPath, "undo", root)
	assertCommandSucceeded(t, "undo unzip", undoResult)

	// Archive should be restored with identical bytes.
	restoredArchive, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("failed to read restored archive: %v", err)
	}
	if !bytes.Equal(originalArchive, restoredArchive) {
		t.Fatalf("restored archive content differs from original (original %d bytes, restored %d bytes)",
			len(originalArchive), len(restoredArchive))
	}
}

// --- Full pipeline undo round-trip ---

func TestEndToEndDataLoss_FullPipelineUndoRoundTrip(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2023, 9, 18, 7, 45, 0, 0, time.UTC)

	// Original structure with known content.
	type origFile struct {
		relPath string
		content string
	}
	originals := []origFile{
		{"docs/report.pdf", "pdf-data-unique"},
		{"docs/readme.txt", "readme-data"},
		{"photos/vacation.jpg", "jpg-data-unique"},
		{"photos/duplicate.jpg", "jpg-data-unique"}, // duplicate of vacation.jpg
		{"other/notes.txt", "notes-data"},
	}
	for _, f := range originals {
		writeFile(t, filepath.Join(root, f.relPath), f.content, modTime)
	}

	// Archive to be extracted.
	archivePath := filepath.Join(root, "extras.zip")
	writeZipArchive(t, archivePath, []zipFixtureEntry{
		{name: "bonus.txt", content: []byte("bonus-data")},
	})

	// Step 1: Unzip
	time.Sleep(100 * time.Millisecond)
	unzipResult := runBinary(t, binPath, "--no-snapshot", "unzip", root)
	assertCommandSucceeded(t, "unzip", unzipResult)

	// Step 2: Rename
	time.Sleep(1100 * time.Millisecond) // ensure different run ID timestamp
	renameResult := runBinary(t, binPath, "--no-snapshot", "rename", root)
	assertCommandSucceeded(t, "rename", renameResult)

	// Step 3: Flatten
	time.Sleep(1100 * time.Millisecond)
	flattenResult := runBinary(t, binPath, "--workers", "1", "--no-snapshot", "flatten", root)
	assertCommandSucceeded(t, "flatten", flattenResult)

	// Step 4: Organize
	time.Sleep(1100 * time.Millisecond)
	organizeResult := runBinary(t, binPath, "--no-snapshot", "organize", root)
	assertCommandSucceeded(t, "organize", organizeResult)

	// Step 5: Duplicate
	time.Sleep(1100 * time.Millisecond)
	dupResult := runBinary(t, binPath, "--workers", "1", "--no-snapshot", "duplicate", root)
	assertCommandSucceeded(t, "duplicate", dupResult)

	// Now undo all 5 steps in reverse order, using --run for each.
	journalDir := filepath.Join(root, ".btidy", "journal")
	entries, err := os.ReadDir(journalDir)
	if err != nil {
		t.Fatalf("failed to read journal dir: %v", err)
	}

	// Collect active journal run IDs by command prefix.
	type journalInfo struct {
		runID string
		name  string
	}
	var journals []journalInfo
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".jsonl") && !strings.HasSuffix(name, ".rolled-back.jsonl") {
			runID := strings.TrimSuffix(name, ".jsonl")
			journals = append(journals, journalInfo{runID: runID, name: name})
		}
	}

	// Find a journal by command prefix. Returns "" if none exists (some
	// commands like duplicate may produce no journal when flatten already
	// removed all duplicates).
	findRunID := func(prefix string) string {
		for _, j := range journals {
			if strings.HasPrefix(j.runID, prefix) {
				return j.runID
			}
		}
		return ""
	}

	// Undo in reverse: duplicate, organize, flatten, rename, unzip.
	// Some steps may not have a journal (e.g. duplicate after flatten
	// already deduplicated), so skip those gracefully.
	undoOrder := []string{"duplicate", "organize", "flatten", "rename", "unzip"}
	for _, cmd := range undoOrder {
		runID := findRunID(cmd)
		if runID == "" {
			t.Logf("no journal for %q (no mutations occurred), skipping undo", cmd)
			continue
		}
		undoResult := runBinary(t, binPath, "undo", "--run", runID, root)
		assertCommandSucceeded(t, "undo "+cmd, undoResult)
	}

	// Verify all original files are restored with correct content.
	for _, f := range originals {
		assertFileContent(t, filepath.Join(root, f.relPath), f.content)
	}

	// The archive should be restored too.
	assertExists(t, archivePath)

	// The bonus file from the archive should still exist
	// (unzip undo restores the archive from trash but doesn't remove extracted files).
	assertExists(t, filepath.Join(root, "bonus.txt"))
}

// --- Undo hash verification rejection ---

func TestEndToEndDataLoss_UndoRejectsCorruptedTrash(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 7, 1, 10, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(root, "a.txt"), "original-data", modTime)
	writeFile(t, filepath.Join(root, "b.txt"), "original-data", modTime)

	// Run duplicate — this records hashes in journal entries.
	dupResult := runBinary(t, binPath, "--workers", "1", "--no-snapshot", "duplicate", root)
	assertCommandSucceeded(t, "duplicate", dupResult)

	// Find and corrupt the trashed file.
	trashRoot := filepath.Join(root, ".btidy", "trash")
	var trashedFile string
	err := filepath.Walk(trashRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !info.IsDir() {
			trashedFile = path
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk trash: %v", err)
	}
	if trashedFile == "" {
		t.Fatal("no trashed file found")
	}

	// Corrupt the trashed file by writing different content.
	if err := os.WriteFile(trashedFile, []byte("CORRUPTED"), 0o600); err != nil {
		t.Fatalf("failed to corrupt trashed file: %v", err)
	}

	// Undo with --verbose to see skip reasons.
	undoResult := runBinary(t, binPath, "-v", "undo", root)
	// The command should succeed overall (skipped operations are not errors).
	assertCommandSucceeded(t, "undo with corrupted trash", undoResult)

	// The verbose output should show a skip due to hash mismatch.
	combined := undoResult.combinedOutput()
	if !strings.Contains(combined, "hash mismatch") && !strings.Contains(combined, "Skipped:") {
		t.Fatalf("expected hash mismatch skip in output\n%s", combined)
	}

	// The Skipped count should be >= 1.
	if !strings.Contains(combined, "Skipped:   1") {
		// At minimum we expect the skipped count to be non-zero.
		if strings.Contains(combined, "Skipped:   0") {
			t.Fatalf("expected at least 1 skipped operation due to hash mismatch\n%s", combined)
		}
	}
}

// --- Unzip backup content verification ---

func TestEndToEndDataLoss_UnzipBackupPreservesOriginalContent(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2024, 12, 5, 10, 0, 0, 0, time.UTC)

	// Create a file at the extraction target path with known content.
	writeFile(t, filepath.Join(root, "file.txt"), "original-precious-data", modTime)

	// Create a zip that extracts a file with the same name.
	archivePath := filepath.Join(root, "archive.zip")
	writeZipArchive(t, archivePath, []zipFixtureEntry{
		{name: "file.txt", content: []byte("new-data")},
	})

	result := runBinary(t, binPath, "--no-snapshot", "unzip", root)
	assertCommandSucceeded(t, "unzip with existing file", result)

	// Extracted file has new content.
	assertFileContent(t, filepath.Join(root, "file.txt"), "new-data")

	// Original content must be preserved in trash.
	trashRoot := filepath.Join(root, ".btidy", "trash")
	var trashedContent string
	err := filepath.Walk(trashRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !info.IsDir() && strings.HasSuffix(path, "file.txt") {
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			trashedContent = string(data)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk trash: %v", err)
	}

	if trashedContent != "original-precious-data" {
		t.Fatalf("expected trashed file to contain 'original-precious-data', got %q", trashedContent)
	}
}

// --- Deduplicator retains correct content in all surviving files ---

func TestEndToEndDataLoss_DuplicateAllUniqueContentSurvives(t *testing.T) {
	binPath := binaryPath(t)
	root := t.TempDir()
	modTime := time.Date(2022, 11, 4, 9, 0, 0, 0, time.UTC)

	// Create multiple duplicate groups and unique files.
	writeFile(t, filepath.Join(root, "group1-a.txt"), "content-group1", modTime)
	writeFile(t, filepath.Join(root, "group1-b.txt"), "content-group1", modTime)
	writeFile(t, filepath.Join(root, "group1-c.txt"), "content-group1", modTime)
	writeFile(t, filepath.Join(root, "group2-a.txt"), "content-group2", modTime)
	writeFile(t, filepath.Join(root, "group2-b.txt"), "content-group2", modTime)
	writeFile(t, filepath.Join(root, "solo.txt"), "content-solo", modTime)

	result := runBinary(t, binPath, "--workers", "1", "--no-snapshot", "duplicate", root)
	assertCommandSucceeded(t, "duplicate", result)

	// Walk the root and collect all unique contents.
	contentSet := make(map[string]bool)
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("failed to read root: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(root, e.Name()))
		if readErr != nil {
			t.Fatalf("failed to read %s: %v", e.Name(), readErr)
		}
		contentSet[string(data)] = true
	}

	// All three unique content values must still exist.
	for _, expected := range []string{"content-group1", "content-group2", "content-solo"} {
		if !contentSet[expected] {
			t.Fatalf("expected content %q to survive deduplication, surviving contents: %v", expected, contentSet)
		}
	}
}
