package service

import (
	"testing"
	"time"

	"github.com/opencloud-eu/opencloud/services/jobengine/pkg/config"
)

func TestSubmitAndGetJob(t *testing.T) {
	cfg := config.PipelineDefaults()
	cfg.Pipelines["test-echo"] = config.Pipeline{
		Label:       "Test Echo",
		SourceTypes: []string{"text/plain"},
		Batch:       true,
		Target:      config.TargetConfig{Extension: ".out", Location: "same"},
		Executor: config.ExecutorConfig{
			Type:    "exec",
			Command: "echo",
			Args:    []string{"done"},
			Timeout: 5 * time.Second,
		},
	}

	engine := New(cfg)
	defer engine.Shutdown()

	job, err := engine.Submit("test-echo", []string{"file1.txt", "file2.txt"}, "user1", "", false)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	if job.ID == "" {
		t.Error("job ID should not be empty")
	}
	if job.Total != 2 {
		t.Errorf("total = %d, want 2", job.Total)
	}
	if job.Pipeline != "test-echo" {
		t.Errorf("pipeline = %s, want test-echo", job.Pipeline)
	}

	// Wait for completion
	time.Sleep(time.Second)

	got, ok := engine.GetJob(job.ID)
	if !ok {
		t.Fatal("job not found")
	}
	if got.Status != StatusCompleted {
		t.Errorf("status = %s, want completed", got.Status)
	}
	if got.Progress != 100 {
		t.Errorf("progress = %d, want 100", got.Progress)
	}
}

func TestSubmitUnknownPipeline(t *testing.T) {
	cfg := config.PipelineDefaults()
	engine := New(cfg)
	defer engine.Shutdown()

	_, err := engine.Submit("nonexistent", []string{"file.txt"}, "user1", "", false)
	if err == nil {
		t.Error("expected error for unknown pipeline")
	}
}

func TestBatchNotAllowed(t *testing.T) {
	cfg := config.PipelineDefaults()
	cfg.Pipelines["single-only"] = config.Pipeline{
		Batch: false,
		Executor: config.ExecutorConfig{Type: "exec", Command: "echo"},
	}

	engine := New(cfg)
	defer engine.Shutdown()

	_, err := engine.Submit("single-only", []string{"a.txt", "b.txt"}, "user1", "", false)
	if err == nil {
		t.Error("expected error for batch on non-batch pipeline")
	}
}

func TestCancelJob(t *testing.T) {
	cfg := config.PipelineDefaults()
	cfg.Pipelines["slow"] = config.Pipeline{
		Batch: true,
		Executor: config.ExecutorConfig{
			Type:    "exec",
			Command: "sleep",
			Args:    []string{"10"},
			Timeout: 30 * time.Second,
		},
	}

	engine := New(cfg)
	defer engine.Shutdown()

	job, _ := engine.Submit("slow", []string{"file.txt"}, "user1", "", false)

	time.Sleep(100 * time.Millisecond)
	err := engine.CancelJob(job.ID)
	if err != nil {
		t.Fatalf("CancelJob: %v", err)
	}

	got, _ := engine.GetJob(job.ID)
	if got.Status != StatusCancelled {
		t.Errorf("status = %s, want cancelled", got.Status)
	}
}

func TestGetUserJobs(t *testing.T) {
	cfg := config.PipelineDefaults()
	cfg.Pipelines["test"] = config.Pipeline{
		Batch:    true,
		Executor: config.ExecutorConfig{Type: "exec", Command: "echo"},
	}

	engine := New(cfg)
	defer engine.Shutdown()

	engine.Submit("test", []string{"a.txt"}, "alice", "", false)
	engine.Submit("test", []string{"b.txt"}, "bob", "", false)
	engine.Submit("test", []string{"c.txt"}, "alice", "", false)

	aliceJobs := engine.GetUserJobs("alice", "")
	if len(aliceJobs) != 2 {
		t.Errorf("alice jobs = %d, want 2", len(aliceJobs))
	}

	bobJobs := engine.GetUserJobs("bob", "")
	if len(bobJobs) != 1 {
		t.Errorf("bob jobs = %d, want 1", len(bobJobs))
	}
}
