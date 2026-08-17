package svc

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	searchsvc "github.com/opencloud-eu/opencloud/protogen/gen/opencloud/services/search/v0"
	"github.com/opencloud-eu/opencloud/services/graph/pkg/errorcode"
)

// ResolveResourceID resolves a resource ID to its current location via the
// search index. Handles both current IDs and old IDs (cross-space moves).
//
// Uses the existing Search RPC with proper protobuf types — no custom
// gRPC messages needed.
//
// GET /v1.0/resources/{resourceID}/resolve
func (g Graph) ResolveResourceID(w http.ResponseWriter, r *http.Request) {
	resourceID := chi.URLParam(r, "resourceID")
	if resourceID == "" {
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "missing resourceID")
		return
	}

	// Search by ID field, then by OldIDs field (cross-space move fallback)
	for _, query := range []string{"ID:" + resourceID, "OldIDs:" + resourceID} {
		resp, err := g.searchService.Search(r.Context(), &searchsvc.SearchRequest{
			Query:    query,
			PageSize: 1,
		})
		if err != nil || resp.TotalMatches == 0 || len(resp.Matches) == 0 {
			continue
		}
		m := resp.Matches[0].Entity
		rid := ""
		rootID := ""
		if m.Id != nil {
			rid = m.Id.StorageId + "$" + m.Id.SpaceId + "!" + m.Id.OpaqueId
			rootID = m.Id.StorageId + "$" + m.Id.SpaceId
		}
		path := ""
		if m.Ref != nil {
			path = m.Ref.Path
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"resource_id": rid,
			"path":        path,
			"root_id":     rootID,
			"name":        m.Name,
		})
		return
	}

	errorcode.ItemNotFound.Render(w, r, http.StatusNotFound, "resource not found")
}
