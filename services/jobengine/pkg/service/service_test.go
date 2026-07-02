package service

import (
	"testing"
	"time"

	"github.com/opencloud-eu/opencloud/services/jobengine/pkg/config"
)

func testConfig() *config.PipelineConfig {
	cfg := config.PipelineDefaults()
	cfg.Pipelines["test-echo"] = config.Pipeline{
		Label:       "Test Echo",
		SourceTypes: []string{"text/plain"},
		Target:      config.TargetConfig{Extension: ".out", Location: "same"},
		Job: config.JobConfig{
			Type:    "test-echo",
			Timeout: 5 * time.Minute,
			Params:  map[string]any{"command": "echo", "args": []any{"done"}},
		},
	}
	return cfg
}

func TestSubmitAndGetJob(t *testing.T) {
	engine := New(testConfig())
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
	if job.Status != StatusQueued {
		t.Errorf("status = %s, want queued (dispatcher does not execute)", job.Status)
	}
	if job.ValidTill.IsZero() {
		t.Error("validTill should be set")
	}
}

func TestSubmitUnknownPipeline(t *testing.T) {
	engine := New(testConfig())
	defer engine.Shutdown()

	_, err := engine.Submit("nonexistent", []string{"file.txt"}, "user1", "", false)
	if err == nil {
		t.Error("expected error for unknown pipeline")
	}
}

func TestCancelJob(t *testing.T) {
	engine := New(testConfig())
	defer engine.Shutdown()

	job, _ := engine.Submit("test-echo", []string{"file.txt"}, "user1", "", false)

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
	engine := New(testConfig())
	defer engine.Shutdown()

	engine.Submit("test-echo", []string{"a.txt"}, "alice", "", false)
	engine.Submit("test-echo", []string{"b.txt"}, "bob", "", false)
	engine.Submit("test-echo", []string{"c.txt"}, "alice", "", false)

	aliceJobs := engine.GetUserJobs("alice", "")
	if len(aliceJobs) != 2 {
		t.Errorf("alice jobs = %d, want 2", len(aliceJobs))
	}

	bobJobs := engine.GetUserJobs("bob", "")
	if len(bobJobs) != 1 {
		t.Errorf("bob jobs = %d, want 1", len(bobJobs))
	}
}

func TestJobStaysQueued(t *testing.T) {
	engine := New(testConfig())
	defer engine.Shutdown()

	job, _ := engine.Submit("test-echo", []string{"file.txt"}, "user1", "", false)

	// Job should stay queued — no internal workers, only external poll can pick it
	got, _ := engine.GetJob(job.ID)
	if got.Status != StatusQueued {
		t.Errorf("status = %s, want queued (no internal execution)", got.Status)
	}
}

func TestPipeMatrix(t *testing.T) {
	engine := New(testConfig())
	defer engine.Shutdown()

	engine.SetWorkerSlots("worker-1", map[string]int{"test-echo": 3})

	slots, denied := engine.getWorkerSlots("worker-1", []string{"test-echo", "unknown"})
	if slots["test-echo"] != 3 {
		t.Errorf("slots[test-echo] = %d, want 3", slots["test-echo"])
	}
	if len(denied) != 1 || denied[0] != "unknown" {
		t.Errorf("denied = %v, want [unknown]", denied)
	}
}
