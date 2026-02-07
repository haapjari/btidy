// Package usecase provides application-level orchestration for CLI workflows.
package usecase

import (
	"errors"
	"fmt"
	"os"
	"time"

	"btidy/pkg/collector"
	"btidy/pkg/deduplicator"
	"btidy/pkg/flattener"
	"btidy/pkg/manifest"
	"btidy/pkg/renamer"
	"btidy/pkg/safepath"
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
	Workers   int
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
	Workers   int
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

	execution := RenameExecution{
		RootDir:         workflowResult.RootDir,
		FileCount:       workflowResult.FileCount,
		CollectDuration: workflowResult.CollectDuration,
		Result:          workflowResult.Result,
	}
	if err := failOnUnsafeOperation(execution.Result.Operations, "rename", func(op renamer.RenameOperation) (string, error) {
		return op.OriginalPath, op.Error
	}); err != nil {
		return execution, err
	}

	return execution, nil
}

// RunFlatten executes the flatten workflow.
func (s *Service) RunFlatten(req FlattenRequest) (FlattenExecution, error) {
	workflowResult, err := runFileWorkflow(s, req.TargetDir, flattenExecutor(req.DryRun, req.Workers))
	if err != nil {
		return FlattenExecution{}, err
	}

	execution := FlattenExecution{
		RootDir:         workflowResult.RootDir,
		FileCount:       workflowResult.FileCount,
		CollectDuration: workflowResult.CollectDuration,
		Result:          workflowResult.Result,
	}
	if err := failOnUnsafeOperation(execution.Result.Operations, "flatten", func(op flattener.MoveOperation) (string, error) {
		return op.OriginalPath, op.Error
	}); err != nil {
		return execution, err
	}

	return execution, nil
}

// RunDuplicate executes the duplicate workflow.
func (s *Service) RunDuplicate(req DuplicateRequest) (DuplicateExecution, error) {
	workflowResult, err := runFileWorkflow(s, req.TargetDir, duplicateExecutor(req.DryRun, req.Workers))
	if err != nil {
		return DuplicateExecution{}, err
	}

	execution := DuplicateExecution{
		RootDir:         workflowResult.RootDir,
		FileCount:       workflowResult.FileCount,
		CollectDuration: workflowResult.CollectDuration,
		Result:          workflowResult.Result,
	}
	if err := failOnUnsafeOperation(execution.Result.Operations, "duplicate", func(op deduplicator.DeleteOperation) (string, error) {
		return op.Path, op.Error
	}); err != nil {
		return execution, err
	}

	return execution, nil
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
		SkipFiles:  s.skipFileList(),
		OnProgress: req.OnProgress,
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

// Workflow invariant: no path is opened or mutated before validator approval.
type workflowTarget struct {
	rootDir   string
	validator *safepath.Validator
}

func runFileWorkflow[T any](s *Service, targetDir string, execute func(rootDir string, validator *safepath.Validator, files []collector.FileInfo) (T, error)) (fileWorkflowResult[T], error) {
	target, err := resolveWorkflowTarget(targetDir)
	if err != nil {
		return fileWorkflowResult[T]{}, err
	}

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

	operationResult, err := execute(target.rootDir, target.validator, files)
	if err != nil {
		return fileWorkflowResult[T]{}, err
	}

	workflowResult.Result = operationResult

	return workflowResult, nil
}

func renameExecutor(dryRun bool) func(rootDir string, validator *safepath.Validator, files []collector.FileInfo) (renamer.Result, error) {
	return func(_ string, validator *safepath.Validator, files []collector.FileInfo) (renamer.Result, error) {
		r, err := renamer.NewWithValidator(validator, dryRun)
		if err != nil {
			return renamer.Result{}, fmt.Errorf("failed to create renamer: %w", err)
		}

		return r.RenameFiles(files), nil
	}
}

func flattenExecutor(dryRun bool, workers int) func(rootDir string, validator *safepath.Validator, files []collector.FileInfo) (flattener.Result, error) {
	return func(_ string, validator *safepath.Validator, files []collector.FileInfo) (flattener.Result, error) {
		f, err := flattener.NewWithValidator(validator, dryRun, workers)
		if err != nil {
			return flattener.Result{}, fmt.Errorf("failed to create flattener: %w", err)
		}

		return f.FlattenFiles(files), nil
	}
}

func duplicateExecutor(dryRun bool, workers int) func(rootDir string, validator *safepath.Validator, files []collector.FileInfo) (deduplicator.Result, error) {
	return func(_ string, validator *safepath.Validator, files []collector.FileInfo) (deduplicator.Result, error) {
		d, err := deduplicator.NewWithValidator(validator, dryRun, workers)
		if err != nil {
			return deduplicator.Result{}, fmt.Errorf("failed to create deduplicator: %w", err)
		}

		return d.FindDuplicates(files), nil
	}
}

func (s *Service) skipFileList() []string {
	return append([]string(nil), s.skipFiles...)
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
