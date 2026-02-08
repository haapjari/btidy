// Package usecase provides application-level orchestration for CLI workflows.
package usecase

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"btidy/pkg/collector"
	"btidy/pkg/deduplicator"
	"btidy/pkg/filelock"
	"btidy/pkg/flattener"
	"btidy/pkg/journal"
	"btidy/pkg/manifest"
	"btidy/pkg/metadata"
	"btidy/pkg/organizer"
	"btidy/pkg/progress"
	"btidy/pkg/renamer"
	"btidy/pkg/safepath"
	"btidy/pkg/trash"
	"btidy/pkg/unzipper"
)

// Options configures a Service.
type Options struct {
	SkipFiles  []string
	SkipDirs   []string
	NoSnapshot bool
}

// ProgressCallback receives workflow stage progress updates.
// Stage names are command-specific and intended for user-facing progress output.
type ProgressCallback func(stage string, processed, total int)

// Service orchestrates command workflows without Cobra dependencies.
type Service struct {
	skipFiles  []string
	skipDirs   []string
	noSnapshot bool
}

// New creates a use-case service.
func New(opts Options) *Service {
	return &Service{
		skipFiles:  append([]string(nil), opts.SkipFiles...),
		skipDirs:   append([]string(nil), opts.SkipDirs...),
		noSnapshot: opts.NoSnapshot,
	}
}

// RenameRequest contains inputs for the rename workflow.
type RenameRequest struct {
	TargetDir  string
	DryRun     bool
	OnProgress ProgressCallback
}

// RenameExecution contains rename workflow outputs.
type RenameExecution struct {
	RootDir         string
	FileCount       int
	CollectDuration time.Duration
	Result          renamer.Result
	SnapshotPath    string
	JournalPath     string
}

// FlattenRequest contains inputs for the flatten workflow.
type FlattenRequest struct {
	TargetDir  string
	DryRun     bool
	Workers    int
	OnProgress ProgressCallback
}

// FlattenExecution contains flatten workflow outputs.
type FlattenExecution struct {
	RootDir         string
	FileCount       int
	CollectDuration time.Duration
	Result          flattener.Result
	SnapshotPath    string
	JournalPath     string
}

// DuplicateRequest contains inputs for the duplicate workflow.
type DuplicateRequest struct {
	TargetDir  string
	DryRun     bool
	Workers    int
	OnProgress ProgressCallback
}

// DuplicateExecution contains duplicate workflow outputs.
type DuplicateExecution struct {
	RootDir         string
	FileCount       int
	CollectDuration time.Duration
	Result          deduplicator.Result
	SnapshotPath    string
	JournalPath     string
}

// UnzipRequest contains inputs for the unzip workflow.
type UnzipRequest struct {
	TargetDir  string
	DryRun     bool
	OnProgress ProgressCallback
}

// UnzipExecution contains unzip workflow outputs.
type UnzipExecution struct {
	RootDir         string
	FileCount       int
	CollectDuration time.Duration
	Result          unzipper.Result
	SnapshotPath    string
	JournalPath     string
}

// ManifestRequest contains inputs for the manifest workflow.
type ManifestRequest struct {
	TargetDir  string
	OutputPath string
	Workers    int
	OnProgress ProgressCallback
}

// ManifestExecution contains manifest workflow outputs.
type ManifestExecution struct {
	RootDir    string
	Duration   time.Duration
	Manifest   *manifest.Manifest
	OutputPath string
	Workers    int
}

// OrganizeRequest contains inputs for the organize workflow.
type OrganizeRequest struct {
	TargetDir  string
	DryRun     bool
	OnProgress ProgressCallback
}

// OrganizeExecution contains organize workflow outputs.
type OrganizeExecution struct {
	RootDir         string
	FileCount       int
	CollectDuration time.Duration
	Result          organizer.Result
	SnapshotPath    string
	JournalPath     string
}

// WorkflowMeta contains the common metadata fields shared by all file workflow executions.
type WorkflowMeta struct {
	RootDir         string
	FileCount       int
	CollectDuration time.Duration
	SnapshotPath    string
	JournalPath     string
}

// Meta returns the common workflow metadata for rename executions.
func (e RenameExecution) Meta() WorkflowMeta {
	return WorkflowMeta{
		RootDir: e.RootDir, FileCount: e.FileCount,
		CollectDuration: e.CollectDuration, SnapshotPath: e.SnapshotPath, JournalPath: e.JournalPath,
	}
}

// Meta returns the common workflow metadata for flatten executions.
func (e FlattenExecution) Meta() WorkflowMeta {
	return WorkflowMeta{
		RootDir: e.RootDir, FileCount: e.FileCount,
		CollectDuration: e.CollectDuration, SnapshotPath: e.SnapshotPath, JournalPath: e.JournalPath,
	}
}

// Meta returns the common workflow metadata for duplicate executions.
func (e DuplicateExecution) Meta() WorkflowMeta {
	return WorkflowMeta{
		RootDir: e.RootDir, FileCount: e.FileCount,
		CollectDuration: e.CollectDuration, SnapshotPath: e.SnapshotPath, JournalPath: e.JournalPath,
	}
}

// Meta returns the common workflow metadata for unzip executions.
func (e UnzipExecution) Meta() WorkflowMeta {
	return WorkflowMeta{
		RootDir: e.RootDir, FileCount: e.FileCount,
		CollectDuration: e.CollectDuration, SnapshotPath: e.SnapshotPath, JournalPath: e.JournalPath,
	}
}

// Meta returns the common workflow metadata for organize executions.
func (e OrganizeExecution) Meta() WorkflowMeta {
	return WorkflowMeta{
		RootDir: e.RootDir, FileCount: e.FileCount,
		CollectDuration: e.CollectDuration, SnapshotPath: e.SnapshotPath, JournalPath: e.JournalPath,
	}
}

// RunOrganize executes the organize workflow.
func (s *Service) RunOrganize(req OrganizeRequest) (OrganizeExecution, error) {
	return runCheckedExecution(
		s,
		req.TargetDir,
		req.DryRun,
		organizeExecutor(req.DryRun, req.OnProgress),
		organizeExecutionFromWorkflow,
		"organize",
		func(execution OrganizeExecution) []organizer.MoveOperation {
			return execution.Result.Operations
		},
		func(op organizer.MoveOperation) (string, error) {
			return op.OriginalPath, op.Error
		},
		organizeJournalEntries,
	)
}

// RunRename executes the rename workflow.
func (s *Service) RunRename(req RenameRequest) (RenameExecution, error) {
	return runCheckedExecution(
		s,
		req.TargetDir,
		req.DryRun,
		renameExecutor(req.DryRun, req.OnProgress),
		renameExecutionFromWorkflow,
		"rename",
		func(execution RenameExecution) []renamer.RenameOperation {
			return execution.Result.Operations
		},
		func(op renamer.RenameOperation) (string, error) {
			return op.OriginalPath, op.Error
		},
		renameJournalEntries,
	)
}

// RunFlatten executes the flatten workflow.
func (s *Service) RunFlatten(req FlattenRequest) (FlattenExecution, error) {
	return runCheckedExecution(
		s,
		req.TargetDir,
		req.DryRun,
		flattenExecutor(req.DryRun, req.Workers, req.OnProgress),
		flattenExecutionFromWorkflow,
		"flatten",
		func(execution FlattenExecution) []flattener.MoveOperation {
			return execution.Result.Operations
		},
		func(op flattener.MoveOperation) (string, error) {
			return op.OriginalPath, op.Error
		},
		flattenJournalEntries,
	)
}

// RunDuplicate executes the duplicate workflow.
func (s *Service) RunDuplicate(req DuplicateRequest) (DuplicateExecution, error) {
	return runCheckedExecution(
		s,
		req.TargetDir,
		req.DryRun,
		duplicateExecutor(req.DryRun, req.Workers, req.OnProgress),
		duplicateExecutionFromWorkflow,
		"duplicate",
		func(execution DuplicateExecution) []deduplicator.DeleteOperation {
			return execution.Result.Operations
		},
		func(op deduplicator.DeleteOperation) (string, error) {
			return op.Path, op.Error
		},
		duplicateJournalEntries,
	)
}

// RunUnzip executes the unzip workflow.
func (s *Service) RunUnzip(req UnzipRequest) (UnzipExecution, error) {
	return runCheckedExecution(
		s,
		req.TargetDir,
		req.DryRun,
		unzipExecutor(req.DryRun, req.OnProgress),
		unzipExecutionFromWorkflow,
		"unzip",
		func(execution UnzipExecution) []unzipper.ExtractOperation {
			return execution.Result.Operations
		},
		func(op unzipper.ExtractOperation) (string, error) {
			return op.ArchivePath, op.Error
		},
		unzipJournalEntries,
	)
}

func renameExecutionFromWorkflow(workflowResult fileWorkflowResult[renamer.Result]) RenameExecution {
	return RenameExecution(workflowResult)
}

func flattenExecutionFromWorkflow(workflowResult fileWorkflowResult[flattener.Result]) FlattenExecution {
	return FlattenExecution(workflowResult)
}

func duplicateExecutionFromWorkflow(workflowResult fileWorkflowResult[deduplicator.Result]) DuplicateExecution {
	return DuplicateExecution(workflowResult)
}

func unzipExecutionFromWorkflow(workflowResult fileWorkflowResult[unzipper.Result]) UnzipExecution {
	return UnzipExecution(workflowResult)
}

func organizeExecutionFromWorkflow(workflowResult fileWorkflowResult[organizer.Result]) OrganizeExecution {
	return OrganizeExecution(workflowResult)
}

// RunManifest executes the manifest workflow.
func (s *Service) RunManifest(req ManifestRequest) (ManifestExecution, error) {
	target, err := resolveWorkflowTarget(req.TargetDir)
	if err != nil {
		return ManifestExecution{}, err
	}

	resolvedOutputPath, err := resolveManifestOutputPath(target, req.OutputPath)
	if err != nil {
		return ManifestExecution{}, err
	}

	startTime := time.Now()

	g, err := manifest.NewGeneratorWithValidator(target.validator, req.Workers)
	if err != nil {
		return ManifestExecution{}, fmt.Errorf("failed to create manifest generator: %w", err)
	}

	generatedManifest, err := g.Generate(manifest.GenerateOptions{
		SkipFiles: s.skipFileList(),
		SkipDirs:  s.skipDirList(),
		OnProgress: func(processed, total int, _ string) {
			progress.EmitStage(req.OnProgress, "hashing", processed, total)
		},
	})
	if err != nil {
		return ManifestExecution{}, fmt.Errorf("failed to generate manifest: %w", err)
	}

	if err := generatedManifest.Save(resolvedOutputPath); err != nil {
		return ManifestExecution{}, fmt.Errorf("failed to save manifest: %w", err)
	}

	return ManifestExecution{
		RootDir:    target.rootDir,
		Duration:   time.Since(startTime),
		Manifest:   generatedManifest,
		OutputPath: resolvedOutputPath,
		Workers:    req.Workers,
	}, nil
}

func (s *Service) collectFiles(rootDir string) ([]collector.FileInfo, time.Duration, error) {
	startTime := time.Now()

	c := collector.New(collector.Options{
		SkipFiles: s.skipFileList(),
		SkipDirs:  s.skipDirList(),
	})

	files, err := c.Collect(rootDir)
	if err != nil {
		return nil, 0, err
	}

	return files, time.Since(startTime), nil
}

type fileWorkflowResult[T any] struct {
	RootDir         string
	FileCount       int
	CollectDuration time.Duration
	Result          T
	SnapshotPath    string
	JournalPath     string
}

// Workflow invariant: no path is opened or mutated before validator approval.
type workflowTarget struct {
	rootDir   string
	validator *safepath.Validator
}

func runFileWorkflow[T any](
	s *Service,
	targetDir, command string,
	dryRun bool,
	execute func(rootDir string, validator *safepath.Validator, files []collector.FileInfo) (T, error),
	toJournalEntries func(T, string) []journal.Entry,
) (fileWorkflowResult[T], error) {
	target, err := resolveWorkflowTarget(targetDir)
	if err != nil {
		return fileWorkflowResult[T]{}, err
	}

	// Acquire advisory lock to prevent concurrent btidy processes on the same directory.
	lock, lockErr := acquireWorkflowLock(target)
	if lockErr != nil {
		return fileWorkflowResult[T]{}, lockErr
	}
	defer lock.Close()

	files, collectDuration, err := s.collectFiles(target.rootDir)
	if err != nil {
		return fileWorkflowResult[T]{}, fmt.Errorf("failed to collect files: %w", err)
	}

	workflowResult := fileWorkflowResult[T]{
		RootDir:         target.rootDir,
		FileCount:       len(files),
		CollectDuration: collectDuration,
	}
	if len(files) == 0 {
		return workflowResult, nil
	}

	// Generate pre-operation snapshot unless disabled or in dry-run mode.
	if !s.noSnapshot && !dryRun {
		snapshotPath, snapshotErr := s.generateSnapshot(target, command)
		if snapshotErr != nil {
			return fileWorkflowResult[T]{}, fmt.Errorf("failed to generate pre-operation snapshot: %w", snapshotErr)
		}
		workflowResult.SnapshotPath = snapshotPath
	}

	operationResult, err := execute(target.rootDir, target.validator, files)
	if err != nil {
		return fileWorkflowResult[T]{}, err
	}

	workflowResult.Result = operationResult

	// Write operation journal unless in dry-run mode.
	if !dryRun && toJournalEntries != nil {
		journalPath, journalErr := writeJournal(target, command, toJournalEntries(operationResult, target.rootDir))
		if journalErr != nil {
			return fileWorkflowResult[T]{}, fmt.Errorf("failed to write operation journal: %w", journalErr)
		}
		workflowResult.JournalPath = journalPath
	}

	return workflowResult, nil
}

func runCheckedExecution[T any, E any, O any](
	s *Service,
	targetDir string,
	dryRun bool,
	execute func(rootDir string, validator *safepath.Validator, files []collector.FileInfo) (T, error),
	toExecution func(fileWorkflowResult[T]) E,
	command string,
	operations func(E) []O,
	operationData func(O) (path string, err error),
	toJournalEntries func(T, string) []journal.Entry,
) (E, error) {
	workflowResult, err := runFileWorkflow(s, targetDir, command, dryRun, execute, toJournalEntries)
	if err != nil {
		var zero E
		return zero, err
	}

	execution := toExecution(workflowResult)
	if err := failOnUnsafeOperation(operations(execution), command, operationData); err != nil {
		return execution, err
	}

	return execution, nil
}

func renameExecutor(dryRun bool, onProgress ProgressCallback) func(rootDir string, validator *safepath.Validator, files []collector.FileInfo) (renamer.Result, error) {
	return func(rootDir string, validator *safepath.Validator, files []collector.FileInfo) (renamer.Result, error) {
		trasher, err := initTrasher(rootDir, validator, "rename")
		if err != nil {
			return renamer.Result{}, fmt.Errorf("failed to initialize trash: %w", err)
		}

		r, err := renamer.NewWithValidator(validator, dryRun, trasher)
		if err != nil {
			return renamer.Result{}, fmt.Errorf("failed to create renamer: %w", err)
		}

		return r.RenameFilesWithProgress(files, func(processed, total int) {
			progress.EmitStage(onProgress, "renaming", processed, total)
		}), nil
	}
}

func flattenExecutor(dryRun bool, workers int, onProgress ProgressCallback) func(rootDir string, validator *safepath.Validator, files []collector.FileInfo) (flattener.Result, error) {
	return trashedWorkerExecutor(
		dryRun, workers, onProgress, "flatten",
		flattener.NewWithValidator,
		"failed to create flattener",
		func(f *flattener.Flattener, files []collector.FileInfo, cb func(string, int, int)) flattener.Result {
			return f.FlattenFilesWithProgress(files, cb)
		},
	)
}

func duplicateExecutor(dryRun bool, workers int, onProgress ProgressCallback) func(rootDir string, validator *safepath.Validator, files []collector.FileInfo) (deduplicator.Result, error) {
	return trashedWorkerExecutor(
		dryRun, workers, onProgress, "duplicate",
		deduplicator.NewWithValidator,
		"failed to create deduplicator",
		func(d *deduplicator.Deduplicator, files []collector.FileInfo, cb func(string, int, int)) deduplicator.Result {
			return d.FindDuplicatesWithProgress(files, cb)
		},
	)
}

// trashedWorkerExecutor creates an executor for domain packages that accept
// (validator, dryRun, workers, trasher) and produce staged progress.
func trashedWorkerExecutor[Worker any, Result any](
	dryRun bool,
	workers int,
	onProgress ProgressCallback,
	command string,
	newWorker func(*safepath.Validator, bool, int, *trash.Trasher) (Worker, error),
	createErrContext string,
	run func(Worker, []collector.FileInfo, func(string, int, int)) Result,
) func(string, *safepath.Validator, []collector.FileInfo) (Result, error) {
	return func(rootDir string, validator *safepath.Validator, files []collector.FileInfo) (Result, error) {
		trasher, err := initTrasher(rootDir, validator, command)
		if err != nil {
			var zero Result
			return zero, fmt.Errorf("failed to initialize trash: %w", err)
		}

		w, err := newWorker(validator, dryRun, workers, trasher)
		if err != nil {
			var zero Result
			return zero, fmt.Errorf("%s: %w", createErrContext, err)
		}

		return run(w, files, func(stage string, processed, total int) {
			progress.EmitStage(onProgress, stage, processed, total)
		}), nil
	}
}

func unzipExecutor(dryRun bool, onProgress ProgressCallback) func(rootDir string, validator *safepath.Validator, files []collector.FileInfo) (unzipper.Result, error) {
	return func(rootDir string, validator *safepath.Validator, files []collector.FileInfo) (unzipper.Result, error) {
		trasher, err := initTrasher(rootDir, validator, "unzip")
		if err != nil {
			return unzipper.Result{}, fmt.Errorf("failed to initialize trash: %w", err)
		}

		u, err := unzipper.NewWithValidator(validator, dryRun, trasher)
		if err != nil {
			return unzipper.Result{}, fmt.Errorf("failed to create unzipper: %w", err)
		}

		return u.ExtractArchivesWithProgress(files, func(stage string, processed, total int) {
			progress.EmitStage(onProgress, stage, processed, total)
		}), nil
	}
}

func organizeExecutor(dryRun bool, onProgress ProgressCallback) func(rootDir string, validator *safepath.Validator, files []collector.FileInfo) (organizer.Result, error) {
	return simpleExecutor(
		dryRun,
		onProgress,
		organizer.NewWithValidator,
		"failed to create organizer",
		"organizing",
		func(w *organizer.Organizer, files []collector.FileInfo, cb func(processed, total int)) organizer.Result {
			return w.OrganizeFilesWithProgress(files, cb)
		},
	)
}

func simpleExecutor[Worker any, Result any](
	dryRun bool,
	onProgress ProgressCallback,
	newWorker func(*safepath.Validator, bool) (Worker, error),
	createErrContext string,
	stageLabel string,
	run func(Worker, []collector.FileInfo, func(processed, total int)) Result,
) func(string, *safepath.Validator, []collector.FileInfo) (Result, error) {
	return func(_ string, validator *safepath.Validator, files []collector.FileInfo) (Result, error) {
		w, err := newWorker(validator, dryRun)
		if err != nil {
			var zero Result
			return zero, fmt.Errorf("%s: %w", createErrContext, err)
		}

		return run(w, files, func(processed, total int) {
			progress.EmitStage(onProgress, stageLabel, processed, total)
		}), nil
	}
}

// initTrasher creates a metadata directory and trasher for a command run.
// In dry-run mode this still initializes the trasher; the domain packages
// skip mutations themselves when dryRun is true.
func initTrasher(rootDir string, validator *safepath.Validator, command string) (*trash.Trasher, error) {
	metaDir, err := metadata.Init(rootDir, validator)
	if err != nil {
		return nil, fmt.Errorf("initialize metadata: %w", err)
	}

	runID := metaDir.RunID(command)

	trasher, err := trash.New(metaDir, runID, validator)
	if err != nil {
		return nil, fmt.Errorf("initialize trasher: %w", err)
	}

	return trasher, nil
}

func (s *Service) skipFileList() []string {
	return append([]string(nil), s.skipFiles...)
}

func (s *Service) skipDirList() []string {
	dirs := append([]string(nil), s.skipDirs...)
	// Always skip the .btidy metadata directory regardless of caller configuration.
	dirs = append(dirs, metadata.DirName)
	return dirs
}

// generateSnapshot creates a pre-operation manifest in .btidy/manifests/.
func (s *Service) generateSnapshot(target workflowTarget, command string) (string, error) {
	metaDir, err := metadata.Init(target.rootDir, target.validator)
	if err != nil {
		return "", fmt.Errorf("initialize metadata: %w", err)
	}

	runID := metaDir.RunID(command)
	snapshotPath := metaDir.ManifestPath(runID)

	// Ensure the manifests directory exists.
	mkdirErr := target.validator.SafeMkdirAll(filepath.Dir(snapshotPath))
	if mkdirErr != nil {
		return "", fmt.Errorf("create manifests directory: %w", mkdirErr)
	}

	gen, err := manifest.NewGeneratorWithValidator(target.validator, runtime.NumCPU())
	if err != nil {
		return "", fmt.Errorf("create manifest generator: %w", err)
	}

	m, err := gen.Generate(manifest.GenerateOptions{
		SkipFiles: s.skipFileList(),
		SkipDirs:  s.skipDirList(),
	})
	if err != nil {
		return "", fmt.Errorf("generate manifest: %w", err)
	}

	if saveErr := m.Save(snapshotPath); saveErr != nil {
		return "", fmt.Errorf("save manifest: %w", saveErr)
	}

	return snapshotPath, nil
}

func resolveWorkflowTarget(targetDir string) (workflowTarget, error) {
	info, err := os.Stat(targetDir)
	if err != nil {
		return workflowTarget{}, fmt.Errorf("cannot access directory: %w", err)
	}
	if !info.IsDir() {
		return workflowTarget{}, fmt.Errorf("%s is not a directory", targetDir)
	}

	validator, err := safepath.New(targetDir)
	if err != nil {
		return workflowTarget{}, fmt.Errorf("cannot create path validator: %w", err)
	}

	return workflowTarget{
		rootDir:   validator.Root(),
		validator: validator,
	}, nil
}

// acquireWorkflowLock initializes the metadata directory and acquires an
// advisory file lock to prevent concurrent btidy processes on the same target.
func acquireWorkflowLock(target workflowTarget) (*filelock.Lock, error) {
	metaDir, err := metadata.Init(target.rootDir, target.validator)
	if err != nil {
		return nil, fmt.Errorf("initialize metadata for lock: %w", err)
	}

	lock, lockErr := filelock.Acquire(metaDir.LockPath())
	if lockErr != nil {
		return nil, fmt.Errorf("another btidy process is operating on this directory: %w", lockErr)
	}

	return lock, nil
}

func resolveManifestOutputPath(target workflowTarget, outputPath string) (string, error) {
	resolvedPath, err := target.validator.ResolveSafePath(target.rootDir, outputPath)
	if err != nil {
		return "", fmt.Errorf("manifest output path must stay within target directory: %w", err)
	}

	if err := target.validator.ValidatePathForWrite(resolvedPath); err != nil {
		return "", fmt.Errorf("manifest output path must stay within target directory: %w", err)
	}

	return resolvedPath, nil
}

func failOnUnsafeOperation[T any](operations []T, command string, operationData func(T) (path string, err error)) error {
	for _, op := range operations {
		path, err := operationData(op)
		if isUnsafePathError(err) {
			return fmt.Errorf("unsafe path detected in %s command for %q: %w", command, path, err)
		}
	}

	return nil
}

func isUnsafePathError(err error) bool {
	if err == nil {
		return false
	}

	return errors.Is(err, safepath.ErrPathEscape) || errors.Is(err, safepath.ErrSymlinkEscape)
}

// writeJournal creates a journal file in .btidy/journal/ and writes entries to it.
func writeJournal(target workflowTarget, command string, entries []journal.Entry) (string, error) {
	if len(entries) == 0 {
		return "", nil
	}

	metaDir, initErr := metadata.Init(target.rootDir, target.validator)
	if initErr != nil {
		return "", fmt.Errorf("initialize metadata: %w", initErr)
	}

	runID := metaDir.RunID(command)
	journalPath := metaDir.JournalPath(runID)

	mkdirErr := target.validator.SafeMkdirAll(filepath.Dir(journalPath))
	if mkdirErr != nil {
		return "", fmt.Errorf("create journal directory: %w", mkdirErr)
	}

	writer, writerErr := journal.NewWriter(journalPath)
	if writerErr != nil {
		return "", fmt.Errorf("create journal writer: %w", writerErr)
	}
	defer writer.Close()

	for i := range entries {
		if logErr := writer.Log(entries[i]); logErr != nil {
			return journalPath, fmt.Errorf("write journal entry: %w", logErr)
		}
	}

	return journalPath, nil
}

// relPath computes a relative path from rootDir, returning the absolute path
// on error as a fallback.
func relPath(rootDir, absPath string) string {
	rel, relErr := filepath.Rel(rootDir, absPath)
	if relErr != nil {
		return absPath
	}
	return rel
}

// renameJournalEntries converts rename operations to journal entries.
func renameJournalEntries(result renamer.Result, rootDir string) []journal.Entry {
	var entries []journal.Entry
	for i := range result.Operations {
		op := &result.Operations[i]
		if op.Error != nil || op.Skipped {
			continue
		}
		if op.NewPath != "" && op.NewPath != op.OriginalPath {
			entries = append(entries, journal.Entry{
				Type:    "rename",
				Source:  relPath(rootDir, op.OriginalPath),
				Dest:    relPath(rootDir, op.NewPath),
				Success: true,
			})
		}
		if op.Deleted && op.TrashedTo != "" {
			entries = append(entries, journal.Entry{
				Type:    "trash",
				Source:  relPath(rootDir, op.OriginalPath),
				Dest:    relPath(rootDir, op.TrashedTo),
				Success: true,
			})
		}
	}
	return entries
}

// flattenJournalEntries converts flatten operations to journal entries.
func flattenJournalEntries(result flattener.Result, rootDir string) []journal.Entry {
	var entries []journal.Entry
	for _, op := range result.Operations {
		if op.Error != nil || op.Skipped {
			continue
		}
		if op.Duplicate && op.TrashedTo != "" {
			entries = append(entries, journal.Entry{
				Type:    "trash",
				Source:  relPath(rootDir, op.OriginalPath),
				Dest:    relPath(rootDir, op.TrashedTo),
				Hash:    op.Hash,
				Success: true,
			})
			continue
		}
		if op.NewPath != "" && op.NewPath != op.OriginalPath {
			entries = append(entries, journal.Entry{
				Type:    "rename",
				Source:  relPath(rootDir, op.OriginalPath),
				Dest:    relPath(rootDir, op.NewPath),
				Success: true,
			})
		}
	}
	return entries
}

// duplicateJournalEntries converts duplicate operations to journal entries.
func duplicateJournalEntries(result deduplicator.Result, rootDir string) []journal.Entry {
	var entries []journal.Entry
	for _, op := range result.Operations {
		if op.Error != nil || op.Skipped {
			continue
		}
		if op.TrashedTo != "" {
			entries = append(entries, journal.Entry{
				Type:    "trash",
				Source:  relPath(rootDir, op.Path),
				Dest:    relPath(rootDir, op.TrashedTo),
				Hash:    op.Hash,
				Success: true,
			})
		}
	}
	return entries
}

// unzipJournalEntries converts unzip operations to journal entries.
func unzipJournalEntries(result unzipper.Result, rootDir string) []journal.Entry {
	var entries []journal.Entry
	for _, op := range result.Operations {
		if op.Error != nil || op.Skipped {
			continue
		}
		if op.ExtractionComplete {
			entries = append(entries, journal.Entry{
				Type:    "extract",
				Source:  relPath(rootDir, op.ArchivePath),
				Success: true,
			})
		}
		if op.DeletedArchive && op.TrashedTo != "" {
			entries = append(entries, journal.Entry{
				Type:    "trash",
				Source:  relPath(rootDir, op.ArchivePath),
				Dest:    relPath(rootDir, op.TrashedTo),
				Success: true,
			})
		}
	}
	return entries
}

// organizeJournalEntries converts organize operations to journal entries.
func organizeJournalEntries(result organizer.Result, rootDir string) []journal.Entry {
	var entries []journal.Entry
	for _, op := range result.Operations {
		if op.Error != nil || op.Skipped {
			continue
		}
		if op.NewPath != "" && op.NewPath != op.OriginalPath {
			entries = append(entries, journal.Entry{
				Type:    "rename",
				Source:  relPath(rootDir, op.OriginalPath),
				Dest:    relPath(rootDir, op.NewPath),
				Success: true,
			})
		}
	}
	return entries
}
