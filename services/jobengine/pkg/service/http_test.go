package service

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/opencloud-eu/opencloud/services/jobengine/pkg/config"
)

func setupTestEngine() (*JobEngine, *chi.Mux) {
	cfg := config.Defaults()
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
	r := chi.NewRouter()
	engine.RegisterRoutes(r)
	return engine, r
}

func TestGetPipelinesAPI(t *testing.T) {
	engine, r := setupTestEngine()
	defer engine.Shutdown()

	req := httptest.NewRequest("GET", "/api/v0/jobs/pipelines", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp PipelinesResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Pipelines) != 1 {
		t.Errorf("pipelines = %d, want 1", len(resp.Pipelines))
	}
	if resp.Pipelines[0].ID != "test-echo" {
		t.Errorf("id = %s, want test-echo", resp.Pipelines[0].ID)
	}
}

func TestSubmitJobNoAuth(t *testing.T) {
	engine, r := setupTestEngine()
	defer engine.Shutdown()

	body, _ := json.Marshal(SubmitRequest{Pipeline: "test-echo", Resources: []string{"file.txt"}})
	req := httptest.NewRequest("POST", "/api/v0/jobs", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestSubmitJobInvalidPipelineID(t *testing.T) {
	engine, r := setupTestEngine()
	defer engine.Shutdown()

	body, _ := json.Marshal(SubmitRequest{Pipeline: "../../etc/passwd", Resources: []string{"file.txt"}})
	req := httptest.NewRequest("POST", "/api/v0/jobs", bytes.NewReader(body))
	req.Header.Set("X-User-Id", "user1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestSubmitJobPathTraversal(t *testing.T) {
	engine, r := setupTestEngine()
	defer engine.Shutdown()

	body, _ := json.Marshal(SubmitRequest{
		Pipeline:  "test-echo",
		Resources: []string{"file.txt"},
		TargetPath: "../../../etc/",
	})
	req := httptest.NewRequest("POST", "/api/v0/jobs", bytes.NewReader(body))
	req.Header.Set("X-User-Id", "user1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for path traversal", w.Code)
	}
}

func TestSubmitJobSuccess(t *testing.T) {
	engine, r := setupTestEngine()
	defer engine.Shutdown()

	body, _ := json.Marshal(SubmitRequest{Pipeline: "test-echo", Resources: []string{"file.txt"}})
	req := httptest.NewRequest("POST", "/api/v0/jobs", bytes.NewReader(body))
	req.Header.Set("X-User-Id", "alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}

	var job Job
	json.Unmarshal(w.Body.Bytes(), &job)
	if job.ID == "" {
		t.Error("job ID should not be empty")
	}
	if job.Pipeline != "test-echo" {
		t.Errorf("pipeline = %s, want test-echo", job.Pipeline)
	}
}

func TestSubmitJobTooManyResources(t *testing.T) {
	engine, r := setupTestEngine()
	defer engine.Shutdown()

	resources := make([]string, 1001)
	for i := range resources {
		resources[i] = "file.txt"
	}

	body, _ := json.Marshal(SubmitRequest{Pipeline: "test-echo", Resources: resources})
	req := httptest.NewRequest("POST", "/api/v0/jobs", bytes.NewReader(body))
	req.Header.Set("X-User-Id", "user1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for too many resources", w.Code)
	}
}

func TestRateLimiting(t *testing.T) {
	cfg := config.Defaults()
	cfg.Pipelines["slow"] = config.Pipeline{
		Batch: true,
		Executor: config.ExecutorConfig{
			Type:    "exec",
			Command: "sleep",
			Args:    []string{"60"},
			Timeout: 120 * time.Second,
		},
	}
	engine := New(cfg)
	defer engine.Shutdown()

	r := chi.NewRouter()
	engine.RegisterRoutes(r)

	// Submit 10 jobs
	for i := 0; i < 10; i++ {
		body, _ := json.Marshal(SubmitRequest{Pipeline: "slow", Resources: []string{"file.txt"}})
		req := httptest.NewRequest("POST", "/api/v0/jobs", bytes.NewReader(body))
		req.Header.Set("X-User-Id", "spammer")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusAccepted {
			t.Fatalf("job %d: status = %d, want 202", i, w.Code)
		}
	}

	// 11th should be rejected
	body, _ := json.Marshal(SubmitRequest{Pipeline: "slow", Resources: []string{"file.txt"}})
	req := httptest.NewRequest("POST", "/api/v0/jobs", bytes.NewReader(body))
	req.Header.Set("X-User-Id", "spammer")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("11th job: status = %d, want 429", w.Code)
	}
}

func TestGetJobNotFound(t *testing.T) {
	engine, r := setupTestEngine()
	defer engine.Shutdown()

	req := httptest.NewRequest("GET", "/api/v0/jobs/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestCancelJobAPI(t *testing.T) {
	cfg := config.Defaults()
	cfg.Pipelines["slow"] = config.Pipeline{
		Batch: true,
		Executor: config.ExecutorConfig{
			Type:    "exec",
			Command: "sleep",
			Args:    []string{"60"},
			Timeout: 120 * time.Second,
		},
	}
	engine := New(cfg)
	defer engine.Shutdown()

	r := chi.NewRouter()
	engine.RegisterRoutes(r)

	// Submit
	body, _ := json.Marshal(SubmitRequest{Pipeline: "slow", Resources: []string{"file.txt"}})
	req := httptest.NewRequest("POST", "/api/v0/jobs", bytes.NewReader(body))
	req.Header.Set("X-User-Id", "user1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var job Job
	json.Unmarshal(w.Body.Bytes(), &job)

	// Cancel
	req = httptest.NewRequest("DELETE", "/api/v0/jobs/"+job.ID, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("cancel status = %d, want 204", w.Code)
	}
}
