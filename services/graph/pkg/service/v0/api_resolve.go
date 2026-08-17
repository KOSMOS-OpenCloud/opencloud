package svc

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	searchsvc "github.com/opencloud-eu/opencloud/protogen/gen/opencloud/services/search/v0"
	"github.com/opencloud-eu/opencloud/services/graph/pkg/errorcode"
)

// ResolveResourceID resolves a (possibly outdated) resource ID to its current
// location via the search index. Used by apps that track resources across
// cross-space moves (e.g. tudu).
//
// GET /v1.0/resources/{resourceID}/resolve
func (g Graph) ResolveResourceID(w http.ResponseWriter, r *http.Request) {
	resourceID := chi.URLParam(r, "resourceID")
	if resourceID == "" {
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "missing resourceID")
		return
	}

	resp, err := g.searchService.Resolve(r.Context(), &searchsvc.ResolveRequest{
		ResourceId: resourceID,
	})
	if err != nil {
		g.logger.Error().Err(err).Str("resourceID", resourceID).Msg("resolve failed")
		errorcode.GeneralException.Render(w, r, http.StatusInternalServerError, "resolve failed")
		return
	}

	if resp.Status != 0 {
		errorcode.ItemNotFound.Render(w, r, http.StatusNotFound, "resource not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"resource_id": resp.ResourceId,
		"path":        resp.Path,
		"root_id":     resp.RootId,
		"name":        resp.Name,
	})
}
