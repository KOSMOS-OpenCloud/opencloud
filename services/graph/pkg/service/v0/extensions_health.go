package svc

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/render"
)

// HealthResponse is the top-level response for GET /graph/v1.0/extensions/health.
type HealthResponse struct {
	Services []ServiceHealth `json:"services"`
}

// ServiceHealth describes the health of a single internal service.
type ServiceHealth struct {
	Name    string      `json:"name"`
	Status  string      `json:"status"` // "ok" or "error"
	Message string      `json:"message,omitempty"`
	Details interface{} `json:"details,omitempty"`
}

// healthCheck defines one service to probe.
type healthCheck struct {
	name string
	url  string
}

// GetExtensionsHealth collects health status from internal services.
// GET /graph/v1.0/extensions/health
func (g Graph) GetExtensionsHealth(w http.ResponseWriter, r *http.Request) {
	client := &http.Client{Timeout: 2 * time.Second}

	checks := []healthCheck{
		{"search", g.config.Health.SearchDebugURL + "/index-status"},
		{"taki", g.config.Health.TakiURL + "/test"},
		{"qdrant", g.config.Health.QdrantURL + "/collections/opencloud"},
	}

	if u := g.config.Health.MicrollmURL; u != "" {
		checks = append(checks, healthCheck{"microllm", u + "/v1/models"})
		checks = append(checks, healthCheck{"microllm-stats", u + "/stats"})
	}
	if u := g.config.Health.CollaboraURL; u != "" {
		checks = append(checks, healthCheck{"collabora", u + "/hosting/discovery"})
	}
	if u := g.config.Health.CollaborationURL; u != "" {
		checks = append(checks, healthCheck{"collaboration", u + "/health"})
	}

	ch := make(chan ServiceHealth, len(checks))
	for _, c := range checks {
		go func(c healthCheck) {
			ch <- fetchHealth(client, c.name, c.url)
		}(c)
	}

	services := make([]ServiceHealth, 0, len(checks))
	for range checks {
		services = append(services, <-ch)
	}

	render.Status(r, http.StatusOK)
	render.JSON(w, r, HealthResponse{Services: services})
}

// fetchHealth makes a GET request and returns a ServiceHealth with parsed JSON details.
func fetchHealth(client *http.Client, name, url string) ServiceHealth {
	resp, err := client.Get(url)
	if err != nil {
		return ServiceHealth{Name: name, Status: "error", Message: err.Error()}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ServiceHealth{Name: name, Status: "error", Message: "read error: " + err.Error()}
	}

	if resp.StatusCode != http.StatusOK {
		return ServiceHealth{Name: name, Status: "error", Message: "HTTP " + resp.Status}
	}

	var details interface{}
	if err := json.Unmarshal(body, &details); err != nil {
		return ServiceHealth{Name: name, Status: "ok", Message: string(body)}
	}

	return ServiceHealth{Name: name, Status: "ok", Details: details}
}
