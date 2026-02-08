package metadata

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"btidy/pkg/safepath"
)

func newValidator(t *testing.T, root string) *safepath.Validator {
	t.Helper()
	v, err := safepath.New(root)
	require.NoError(t, err)
	return v
}

func TestInit_CreatesMetadataDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	v := newValidator(t, root)

	d, err := Init(root, v)
	require.NoError(t, err)

	expectedPath := filepath.Join(root, DirName)
	assert.Equal(t, expectedPath, d.Root(), "metadata root should be .btidy inside target")

	info, err := os.Stat(expectedPath)
	require.NoError(t, err, ".btidy directory should exist")
	assert.True(t, info.IsDir(), ".btidy should be a directory")
}

func TestInit_Idempotent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	v := newValidator(t, root)

	d1, err := Init(root, v)
	require.NoError(t, err)

	d2, err := Init(root, v)
	require.NoError(t, err)

	assert.Equal(t, d1.Root(), d2.Root(), "repeated Init should return same root")
}

func TestDir_TrashDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	v := newValidator(t, root)
	d, err := Init(root, v)
	require.NoError(t, err)

	runID := "flatten-20260208T143022"
	expected := filepath.Join(root, DirName, "trash", runID)
	assert.Equal(t, expected, d.TrashDir(runID))
}

func TestDir_JournalPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	v := newValidator(t, root)
	d, err := Init(root, v)
	require.NoError(t, err)

	runID := "duplicate-20260208T150000"
	expected := filepath.Join(root, DirName, "journal", runID+".jsonl")
	assert.Equal(t, expected, d.JournalPath(runID))
}

func TestDir_ManifestPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	v := newValidator(t, root)
	d, err := Init(root, v)
	require.NoError(t, err)

	runID := "rename-20260208T160000"
	expected := filepath.Join(root, DirName, "manifests", runID+".json")
	assert.Equal(t, expected, d.ManifestPath(runID))
}

func TestDir_LockPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	v := newValidator(t, root)
	d, err := Init(root, v)
	require.NoError(t, err)

	expected := filepath.Join(root, DirName, "lock")
	assert.Equal(t, expected, d.LockPath())
}

func TestDir_RunID_Format(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	v := newValidator(t, root)
	d, err := Init(root, v)
	require.NoError(t, err)

	runID := d.RunID("flatten")
	assert.True(t, strings.HasPrefix(runID, "flatten-"), "run ID should start with command name")

	// Extract timestamp portion and validate format.
	parts := strings.SplitN(runID, "-", 2)
	require.Len(t, parts, 2, "run ID should have command-timestamp format")

	_, parseErr := time.Parse("20060102T150405", parts[1])
	assert.NoError(t, parseErr, "timestamp portion should parse as YYYYMMDDTHHmmss")
}

func TestDir_RunID_UniquePerCommand(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	v := newValidator(t, root)
	d, err := Init(root, v)
	require.NoError(t, err)

	flattenID := d.RunID("flatten")
	renameID := d.RunID("rename")

	assert.True(t, strings.HasPrefix(flattenID, "flatten-"), "flatten run ID prefix")
	assert.True(t, strings.HasPrefix(renameID, "rename-"), "rename run ID prefix")
	assert.NotEqual(t, flattenID, renameID, "different commands produce different IDs")
}

func TestDirName_Constant(t *testing.T) {
	t.Parallel()

	assert.Equal(t, ".btidy", DirName, "metadata directory name should be .btidy")
}
