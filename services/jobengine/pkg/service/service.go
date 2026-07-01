package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/opencloud-eu/opencloud/services/jobengine/pkg/config"
)

// JobStatus represents the state of a job
type JobStatus string

const (
	StatusQueued    JobStatus = "queued"
	StatusRunning   JobStatus = "running"
	StatusCompleted JobStatus = "completed"
	StatusFailed    JobStatus = "failed"
	StatusCancelled JobStatus = "cancelled"
	StatusExpired   JobStatus = "expired"
)

// JobResult is the outcome of a single resource in a batch job
type JobResult struct {
	SourceName string    `json:"source"`
	TargetName string    `json:"target"`
	Status     JobStatus `json:"status"`
	Error      string    `json:"error,omitempty"`
}

// Job is a queued/running/completed job
type Job struct {
	ID        string      `json:"jobId"`
	Pipeline  string      `json:"pipeline"`
	Status    JobStatus   `json:"status"`
	Progress  int         `json:"progress"`
	Completed int         `json:"completed"`
	Total     int         `json:"total"`
	Results   []JobResult `json:"results,omitempty"`
	Error       string      `json:"error,omitempty"`
	UserID      string      `json:"userId"`
	CreatedAt   time.Time   `json:"createdAt"`
	ValidTill   time.Time   `json:"validTill,omitempty"`
	WorkerID    string      `json:"workerId,omitempty"`
	PickedAt    time.Time   `json:"pickedAt,omitempty"`
	CompletedAt time.Time   `json:"completedAt,omitempty"`

	// internal
	resources  []string
	targetPath string
	createDirs bool
	ctx        context.Context
	cancel     context.CancelFunc
}

// JobItem is the work unit for a single file in a job
type JobItem struct {
	SourcePath string
	TargetPath string
	TargetDir  string
	Vars       *TemplateVars
}

// JobEngine is the core service
type JobEngine struct {
	cfg         *config.PipelineConfig
	jobs        map[string]*Job
	mu          sync.RWMutex
	workCh      chan *jobWork
	wg          sync.WaitGroup
	stopCleanup chan struct{}

	// Worker polling state
	heartbeats map[string]time.Time    // workerID → last poll time
	pipeMatrix map[string]map[string]int // workerID → { jobType → slots }
}

// cleanupInterval removes completed/failed jobs older than 1 hour
const jobRetention = 1 * time.Hour

type jobWork struct {
	job      *Job
	pipeline config.Pipeline
	index    int
	resource string
}

// New creates a new JobEngine
func New(cfg *config.PipelineConfig) *JobEngine {
	e := &JobEngine{
		cfg:         cfg,
		jobs:        make(map[string]*Job),
		workCh:      make(chan *jobWork, cfg.Service.QueueSize),
		stopCleanup: make(chan struct{}),
		heartbeats:  make(map[string]time.Time),
		pipeMatrix:  make(map[string]map[string]int),
	}

	// ensure temp dir with restrictive permissions
	os.MkdirAll(cfg.Service.TempDir, 0700)

	// clean stale temp dirs from previous runs
	entries, _ := os.ReadDir(cfg.Service.TempDir)
	for _, entry := range entries {
		os.RemoveAll(fmt.Sprintf("%s/%s", cfg.Service.TempDir, entry.Name()))
	}

	// start workers
	for i := 0; i < cfg.Service.MaxWorkers; i++ {
		e.wg.Add(1)
		go e.worker(i)
	}

	// start cleanup goroutine
	go e.cleanupLoop()

	return e
}

// Submit creates and queues a new job
func (e *JobEngine) Submit(pipelineID string, resources []string, userID string, targetPath string, createDirs bool) (*Job, error) {
	pipeline, ok := e.cfg.Pipelines[pipelineID]
	if !ok {
		return nil, fmt.Errorf("unknown pipeline: %s", pipelineID)
	}

	if !pipeline.Batch && len(resources) > 1 {
		return nil, fmt.Errorf("pipeline %s does not support batch", pipelineID)
	}

	ctx, cancel := context.WithCancel(context.Background())

	job := &Job{
		ID:         uuid.New().String(),
		Pipeline:   pipelineID,
		Status:     StatusQueued,
		Total:      len(resources),
		Results:    make([]JobResult, len(resources)),
		UserID:     userID,
		CreatedAt:  time.Now(),
		resources:  resources,
		targetPath: targetPath,
		createDirs: createDirs,
		ctx:        ctx,
		cancel:     cancel,
	}

	for i, res := range resources {
		job.Results[i] = JobResult{SourceName: res, Status: StatusQueued}
	}

	e.mu.Lock()
	e.jobs[job.ID] = job
	e.mu.Unlock()

	// Queue work items
	for i, res := range resources {
		e.workCh <- &jobWork{
			job:      job,
			pipeline: pipeline,
			index:    i,
			resource: res,
		}
	}

	return job, nil
}

// GetJob returns a job by ID
func (e *JobEngine) GetJob(jobID string) (*Job, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	job, ok := e.jobs[jobID]
	return job, ok
}

// GetUserJobs returns all jobs for a user, optionally filtered by status
func (e *JobEngine) GetUserJobs(userID string, statusFilter JobStatus) []*Job {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var result []*Job
	for _, job := range e.jobs {
		if job.UserID != userID {
			continue
		}
		if statusFilter != "" && job.Status != statusFilter {
			continue
		}
		result = append(result, job)
	}
	return result
}

// CancelJob cancels a running job
func (e *JobEngine) CancelJob(jobID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	job, ok := e.jobs[jobID]
	if !ok {
		return fmt.Errorf("job not found: %s", jobID)
	}
	if job.cancel != nil {
		job.cancel()
	}
	job.Status = StatusCancelled
	return nil
}

// Pipelines returns all registered pipelines
func (e *JobEngine) Pipelines() map[string]config.Pipeline {
	return e.cfg.Pipelines
}

// SetPipeMatrix sets the capability matrix for workers
func (e *JobEngine) SetPipeMatrix(matrix map[string]map[string]int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pipeMatrix = matrix
}

// SetWorkerSlots sets the slots for a single worker in the pipe matrix
func (e *JobEngine) SetWorkerSlots(workerID string, slots map[string]int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pipeMatrix[workerID] = slots
}

// Shutdown stops workers and waits for completion
func (e *JobEngine) Shutdown() {
	close(e.stopCleanup)
	close(e.workCh)
	e.wg.Wait()
}

func (e *JobEngine) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			e.mu.Lock()
			now := time.Now()
			for id, job := range e.jobs {
				if (job.Status == StatusCompleted || job.Status == StatusFailed || job.Status == StatusCancelled) &&
					now.Sub(job.CreatedAt) > jobRetention {
					delete(e.jobs, id)
				}
			}
			e.mu.Unlock()
		case <-e.stopCleanup:
			return
		}
	}
}

func (e *JobEngine) worker(id int) {
	defer e.wg.Done()

	for work := range e.workCh {
		e.processWork(work)
	}
}

func (e *JobEngine) processWork(work *jobWork) {
	job := work.job
	pipeline := work.pipeline

	// Skip if job already cancelled
	if job.Status == StatusCancelled {
		return
	}

	e.mu.Lock()
	if job.Status == StatusQueued {
		job.Status = StatusRunning
	}
	job.Results[work.index].Status = StatusRunning
	e.mu.Unlock()

	// Create temp dir for this work item
	workDir := filepath.Join(e.cfg.Service.TempDir, job.ID, fmt.Sprintf("%d", work.index))
	os.MkdirAll(workDir, 0750)
	defer os.RemoveAll(workDir)

	sourcePath := filepath.Join(workDir, "source"+filepath.Ext(work.resource))
	targetPath := filepath.Join(workDir, "target"+pipeline.Target.Extension)
	targetDir := filepath.Join(workDir, "target_dir")
	os.MkdirAll(targetDir, 0750)

	// TODO: Download source file via Gateway
	// For now, treat resource as a local path (development mode)

	vars := &TemplateVars{
		Source:     sourcePath,
		SourceName: filepath.Base(work.resource),
		SourceExt:  filepath.Ext(work.resource),
		Target:     targetPath,
		TargetDir:  targetDir,
		Options:    pipeline.Options,
	}

	// Create executor
	executor, err := NewExecutor(pipeline.Executor)
	if err != nil {
		e.failWorkItem(job, work.index, err)
		return
	}

	// Set script dir for script executor
	if se, ok := executor.(*ScriptExecutor); ok {
		if len(e.cfg.Service.PipelineDirs) > 0 {
			se.ScriptDir = e.cfg.Service.PipelineDirs[0]
		}
	}

	// Check if job was cancelled before starting
	if job.ctx.Err() != nil {
		e.failWorkItem(job, work.index, fmt.Errorf("job cancelled"))
		return
	}

	// Execute with job context + optional timeout
	ctx := job.ctx
	if pipeline.Executor.Timeout > 0 {
		var timeoutCancel context.CancelFunc
		ctx, timeoutCancel = context.WithTimeout(ctx, pipeline.Executor.Timeout)
		defer timeoutCancel()
	}

	item := &JobItem{
		SourcePath: sourcePath,
		TargetPath: targetPath,
		TargetDir:  targetDir,
		Vars:       vars,
	}

	if err := executor.Execute(ctx, item, pipeline.Executor); err != nil {
		e.failWorkItem(job, work.index, err)
		return
	}

	// TODO: Upload result file via Gateway to target location

	e.mu.Lock()
	job.Results[work.index].Status = StatusCompleted
	job.Results[work.index].TargetName = filepath.Base(targetPath)
	job.Completed++
	job.Progress = (job.Completed * 100) / job.Total
	if job.Completed == job.Total {
		job.Status = StatusCompleted
	}
	e.mu.Unlock()
}

func (e *JobEngine) failWorkItem(job *Job, index int, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	job.Results[index].Status = StatusFailed
	job.Results[index].Error = err.Error()
	job.Completed++
	job.Progress = (job.Completed * 100) / job.Total

	// If all items are done, mark job as failed if any failed
	if job.Completed == job.Total {
		allFailed := true
		for _, r := range job.Results {
			if r.Status == StatusCompleted {
				allFailed = false
				break
			}
		}
		if allFailed {
			job.Status = StatusFailed
			job.Error = err.Error()
		} else {
			job.Status = StatusCompleted
		}
	}
}
