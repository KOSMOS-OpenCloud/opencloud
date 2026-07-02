package service

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/opencloud-eu/opencloud/services/jobengine/pkg/config"
	revactx "github.com/opencloud-eu/reva/v2/pkg/ctx"
)

var validIDRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// RegisterRoutes sets up the HTTP API routes
func (e *JobEngine) RegisterRoutes(r chi.Router) {
	r.Route("/api/v0/jobs", func(r chi.Router) {
		// User-facing API
		r.Get("/pipelines", e.handleGetPipelines)
		r.Post("/", e.handleSubmitJob)
		r.Get("/{jobId}", e.handleGetJob)
		r.Delete("/{jobId}", e.handleCancelJob)
		r.Get("/", e.handleListJobs)

		// Worker-facing API (OpenWorks protocol)
		r.Post("/workers/poll", e.handleWorkerPoll)
	})

	// Admin API for Pipe-Matrix + Workers
	e.RegisterMatrixRoutes(r)

	r.Route("/api/v0/jobs/workers", func(r chi.Router) {
		r.Get("/", e.handleListWorkers)
	})
}

// PipelineInfo is the public representation of a pipeline
type PipelineInfo struct {
	ID             string             `json:"id"`
	Label          string             `json:"label"`
	Icon           string             `json:"icon"`
	SourceTypes    []string           `json:"sourceTypes"`
	TargetLocation string             `json:"targetLocation"`
	Menu           string             `json:"menu,omitempty"`
	Dialog         *config.DialogSpec `json:"dialog,omitempty"`
	JobType        string             `json:"jobType"`
	Notification   string             `json:"notification,omitempty"`
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
			ID:             id,
			Label:          p.Label,
			Icon:           p.Icon,
			SourceTypes:    p.SourceTypes,
			TargetLocation: p.Target.Location,
			Menu:           p.Menu,
			Dialog:         p.Dialog,
			JobType:        p.Job.Type,
			Notification:   p.Notification,
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

	// User ID from context (set by ExtractAccountUUID middleware)
	user, ok := revactx.ContextGetUser(r.Context())
	if !ok || user.GetId() == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}
	userID := user.GetId().GetOpaqueId()

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
	userID := ""
	if user, ok := revactx.ContextGetUser(r.Context()); ok && user.GetId() != nil {
		userID = user.GetId().GetOpaqueId()
	}
	status := JobStatus(r.URL.Query().Get("status"))

	jobs := e.GetUserJobs(userID, status)
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
