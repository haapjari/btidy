// Package usecase provides application-level orchestration for CLI workflows.
package usecase

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"file-organizer/pkg/collector"
	"file-organizer/pkg/deduplicator"
	"file-organizer/pkg/flattener"
	"file-organizer/pkg/manifest"
	"file-organizer/pkg/renamer"
)

// Options configures a Service.
type Options struct {
	SkipFiles []string
}

// Service orchestrates command workflows without Cobra dependencies.
type Service struct {
	skipFiles []string
}

// New creates a use-case service.
func New(opts Options) *Service {
	return &Service{
		skipFiles: append([]string(nil), opts.SkipFiles...),
	}
}

// RenameRequest contains inputs for the rename workflow.
type RenameRequest struct {
	TargetDir string
	DryRun    bool
}

// RenameExecution contains rename workflow outputs.
type RenameExecution struct {
	RootDir         string
	FileCount       int
	CollectDuration time.Duration
	Result          renamer.Result
}

// FlattenRequest contains inputs for the flatten workflow.
type FlattenRequest struct {
	TargetDir string
	DryRun    bool
}

// FlattenExecution contains flatten workflow outputs.
type FlattenExecution struct {
	RootDir         string
	FileCount       int
	CollectDuration time.Duration
	Result          flattener.Result
}

// DuplicateRequest contains inputs for the duplicate workflow.
type DuplicateRequest struct {
	TargetDir string
	DryRun    bool
}

// DuplicateExecution contains duplicate workflow outputs.
type DuplicateExecution struct {
	RootDir         string
	FileCount       int
	CollectDuration time.Duration
	Result          deduplicator.Result
}

// ManifestRequest contains inputs for the manifest workflow.
type ManifestRequest struct {
	TargetDir  string
	OutputPath string
	Workers    int
	OnProgress manifest.ProgressCallback
}

// ManifestExecution contains manifest workflow outputs.
type ManifestExecution struct {
	RootDir    string
	Duration   time.Duration
	Manifest   *manifest.Manifest
	OutputPath string
	Workers    int
}

// RunRename executes the rename workflow.
func (s *Service) RunRename(req RenameRequest) (RenameExecution, error) {
	workflowResult, err := runFileWorkflow(s, req.TargetDir, renameExecutor(req.DryRun))
	if err != nil {
		return RenameExecution{}, err
	}

	return renameExecutionFromWorkflow(workflowResult), nil
}

// RunFlatten executes the flatten workflow.
func (s *Service) RunFlatten(req FlattenRequest) (FlattenExecution, error) {
	workflowResult, err := runFileWorkflow(s, req.TargetDir, flattenExecutor(req.DryRun))
	if err != nil {
		return FlattenExecution{}, err
	}

	return flattenExecutionFromWorkflow(workflowResult), nil
}

// RunDuplicate executes the duplicate workflow.
func (s *Service) RunDuplicate(req DuplicateRequest) (DuplicateExecution, error) {
	workflowResult, err := runFileWorkflow(s, req.TargetDir, duplicateExecutor(req.DryRun))
	if err != nil {
		return DuplicateExecution{}, err
	}

	return duplicateExecutionFromWorkflow(workflowResult), nil
}

// RunManifest executes the manifest workflow.
func (s *Service) RunManifest(req ManifestRequest) (ManifestExecution, error) {
	rootDir, err := resolveTargetDir(req.TargetDir)
	if err != nil {
		return ManifestExecution{}, err
	}

	startTime := time.Now()

	g, err := manifest.NewGenerator(rootDir, req.Workers)
	if err != nil {
		return ManifestExecution{}, fmt.Errorf("failed to create manifest generator: %w", err)
	}

	generatedManifest, err := g.Generate(manifest.GenerateOptions{
		SkipFiles:  s.skipFileList(),
		OnProgress: req.OnProgress,
	})
	if err != nil {
		return ManifestExecution{}, fmt.Errorf("failed to generate manifest: %w", err)
	}

	if err := generatedManifest.Save(req.OutputPath); err != nil {
		return ManifestExecution{}, fmt.Errorf("failed to save manifest: %w", err)
	}

	return ManifestExecution{
		RootDir:    rootDir,
		Duration:   time.Since(startTime),
		Manifest:   generatedManifest,
		OutputPath: req.OutputPath,
		Workers:    req.Workers,
	}, nil
}

func (s *Service) collectFiles(rootDir string) ([]collector.FileInfo, time.Duration, error) {
	startTime := time.Now()

	c := collector.New(collector.Options{
		SkipFiles: s.skipFileList(),
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
}

func runFileWorkflow[T any](s *Service, targetDir string, execute func(rootDir string, files []collector.FileInfo) (T, error)) (fileWorkflowResult[T], error) {
	rootDir, err := resolveTargetDir(targetDir)
	if err != nil {
		return fileWorkflowResult[T]{}, err
	}

	files, collectDuration, err := s.collectFiles(rootDir)
	if err != nil {
		return fileWorkflowResult[T]{}, fmt.Errorf("failed to collect files: %w", err)
	}

	workflowResult := fileWorkflowResult[T]{
		RootDir:         rootDir,
		FileCount:       len(files),
		CollectDuration: collectDuration,
	}
	if len(files) == 0 {
		return workflowResult, nil
	}

	operationResult, err := execute(rootDir, files)
	if err != nil {
		return fileWorkflowResult[T]{}, err
	}

	workflowResult.Result = operationResult

	return workflowResult, nil
}

func renameExecutor(dryRun bool) func(rootDir string, files []collector.FileInfo) (renamer.Result, error) {
	return func(rootDir string, files []collector.FileInfo) (renamer.Result, error) {
		r, err := renamer.New(rootDir, dryRun)
		if err != nil {
			return renamer.Result{}, fmt.Errorf("failed to create renamer: %w", err)
		}

		return r.RenameFiles(files), nil
	}
}

func flattenExecutor(dryRun bool) func(rootDir string, files []collector.FileInfo) (flattener.Result, error) {
	return func(rootDir string, files []collector.FileInfo) (flattener.Result, error) {
		f, err := flattener.New(rootDir, dryRun)
		if err != nil {
			return flattener.Result{}, fmt.Errorf("failed to create flattener: %w", err)
		}

		return f.FlattenFiles(files), nil
	}
}

func duplicateExecutor(dryRun bool) func(rootDir string, files []collector.FileInfo) (deduplicator.Result, error) {
	return func(rootDir string, files []collector.FileInfo) (deduplicator.Result, error) {
		d, err := deduplicator.New(rootDir, dryRun)
		if err != nil {
			return deduplicator.Result{}, fmt.Errorf("failed to create deduplicator: %w", err)
		}

		return d.FindDuplicates(files), nil
	}
}

func renameExecutionFromWorkflow(workflowResult fileWorkflowResult[renamer.Result]) RenameExecution {
	return RenameExecution{
		RootDir:         workflowResult.RootDir,
		FileCount:       workflowResult.FileCount,
		CollectDuration: workflowResult.CollectDuration,
		Result:          workflowResult.Result,
	}
}

func flattenExecutionFromWorkflow(workflowResult fileWorkflowResult[flattener.Result]) FlattenExecution {
	return FlattenExecution{
		RootDir:         workflowResult.RootDir,
		FileCount:       workflowResult.FileCount,
		CollectDuration: workflowResult.CollectDuration,
		Result:          workflowResult.Result,
	}
}

func duplicateExecutionFromWorkflow(workflowResult fileWorkflowResult[deduplicator.Result]) DuplicateExecution {
	return DuplicateExecution{
		RootDir:         workflowResult.RootDir,
		FileCount:       workflowResult.FileCount,
		CollectDuration: workflowResult.CollectDuration,
		Result:          workflowResult.Result,
	}
}

func (s *Service) skipFileList() []string {
	return append([]string(nil), s.skipFiles...)
}

func resolveTargetDir(targetDir string) (string, error) {
	info, err := os.Stat(targetDir)
	if err != nil {
		return "", fmt.Errorf("cannot access directory: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", targetDir)
	}

	absPath, err := filepath.Abs(targetDir)
	if err != nil {
		return "", fmt.Errorf("cannot resolve path: %w", err)
	}

	return absPath, nil
}
