package e2e

import (
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

	cmd := exec.Command("go", "build", "-o", binPath, "./cmd")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "failed to build btidy: %v\n%s\n", err, string(output))
		_ = os.RemoveAll(binDir)
		os.Exit(1)
	}

	builtBinaryPath = binPath

	exitCode := m.Run()
	_ = os.RemoveAll(binDir)
	os.Exit(exitCode)
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

func assertExists(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected path to exist: %s (error: %v)", path, err)
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
		if entry.IsDir() {
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
	if err := os.Symlink(outsideFile, linkPath); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	result := runBinary(t, binPath, "rename", "--dry-run", root)
	assertCommandFailed(t, result, "unsafe path", "rename", "symlink")

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
	if err := os.Symlink(outsideFile, linkPath); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	result := runBinary(t, binPath, "flatten", root)
	assertCommandFailed(t, result, "unsafe path", "flatten", "symlink")

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
	if err := os.Symlink(outsideFile, linkPath); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	result := runBinary(t, binPath, "duplicate", root)
	assertCommandFailed(t, result, "unsafe path", "duplicate", "symlink")

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
	if err := os.Symlink(outsideFile, linkPath); err != nil {
		t.Skipf("symlink not supported: %v", err)
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
