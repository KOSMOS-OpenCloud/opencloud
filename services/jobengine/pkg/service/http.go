package service

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

var validIDRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// RegisterRoutes sets up the HTTP API routes
func (e *JobEngine) RegisterRoutes(r chi.Router) {
	r.Route("/api/v0/jobs", func(r chi.Router) {
		r.Get("/pipelines", e.handleGetPipelines)
		r.Post("/", e.handleSubmitJob)
		r.Get("/{jobId}", e.handleGetJob)
		r.Delete("/{jobId}", e.handleCancelJob)
		r.Get("/", e.handleListJobs)
	})
}

// PipelineInfo is the public representation of a pipeline
type PipelineInfo struct {
	ID                  string   `json:"id"`
	Label               string   `json:"label"`
	Icon                string   `json:"icon"`
	SourceTypes         []string `json:"sourceTypes"`
	TargetLocation      string   `json:"targetLocation"`
	UserChoosableTarget bool     `json:"userChoosableTarget"`
	Batch               bool     `json:"batch"`
}

type PipelinesResponse struct {
	Pipelines []PipelineInfo `json:"pipelines"`
}

type SubmitRequest struct {
	Pipeline     string   `json:"pipeline"`
	Resources    []string `json:"resources"`
	TargetPath   string   `json:"targetPath"`
	CreateTarget bool     `json:"createTarget"`
}

func (e *JobEngine) handleGetPipelines(w http.ResponseWriter, r *http.Request) {
	pipelines := e.Pipelines()
	resp := PipelinesResponse{
		Pipelines: make([]PipelineInfo, 0, len(pipelines)),
	}

	for id, p := range pipelines {
		resp.Pipelines = append(resp.Pipelines, PipelineInfo{
			ID:                  id,
			Label:               p.Label,
			Icon:                p.Icon,
			SourceTypes:         p.SourceTypes,
			TargetLocation:      p.Target.Location,
			UserChoosableTarget: p.UserChoosableTarget,
			Batch:               p.Batch,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

func (e *JobEngine) handleSubmitJob(w http.ResponseWriter, r *http.Request) {
	var req SubmitRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	// Validate pipeline ID — only alphanumeric, dash, underscore
	if req.Pipeline == "" || !validIDRe.MatchString(req.Pipeline) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid pipeline id"})
		return
	}

	if len(req.Resources) == 0 || len(req.Resources) > 1000 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "1-1000 resources required"})
		return
	}

	// Validate target path — prevent path traversal
	if req.TargetPath != "" {
		cleaned := filepath.Clean(req.TargetPath)
		if strings.Contains(cleaned, "..") {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid target path"})
			return
		}
		req.TargetPath = cleaned
	}

	// User ID from OpenCloud proxy (x-access-token is validated by proxy)
	userID := r.Header.Get("X-User-Id")
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}

	// Rate limit: max 10 active jobs per user
	activeJobs := e.GetUserJobs(userID, "")
	activeCount := 0
	for _, j := range activeJobs {
		if j.Status == StatusQueued || j.Status == StatusRunning {
			activeCount++
		}
	}
	if activeCount >= 10 {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "max 10 active jobs per user"})
		return
	}

	job, err := e.Submit(req.Pipeline, req.Resources, userID, req.TargetPath, req.CreateTarget)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusAccepted, job)
}

func (e *JobEngine) handleGetJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobId")
	job, ok := e.GetJob(jobID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}

	writeJSON(w, http.StatusOK, job)
}

func (e *JobEngine) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobId")
	if err := e.CancelJob(jobID); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (e *JobEngine) handleListJobs(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-Id")
	status := JobStatus(r.URL.Query().Get("status"))

	jobs := e.GetUserJobs(userID, status)
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
