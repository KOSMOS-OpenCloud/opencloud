package svc

import (
	"context"
	"net/http"

	searchsvc "github.com/opencloud-eu/opencloud/protogen/gen/opencloud/services/search/v0"
	"github.com/opencloud-eu/opencloud/services/graph/pkg/errorcode"
)

// ReindexItem triggers re-indexing of a drive item's space.
// Fire-and-forget — returns 202 Accepted immediately.
//
// POST /drives/{driveID}/items/{itemID}/reindex
func (g Graph) ReindexItem(w http.ResponseWriter, r *http.Request) {
	itemID, err := parseIDParam(r, "itemID")
	if err != nil {
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "invalid itemID")
		return
	}

	resourceID := itemID.GetStorageId() + "$" + itemID.GetSpaceId() + "!" + itemID.GetOpaqueId()
	forceOverwrite := r.URL.Query().Get("overwrite") == "true"

	g.logger.Info().Str("itemID", resourceID).Bool("overwrite", forceOverwrite).Msg("reindex item requested")

	// Trigger: Search-Service verarbeitet async, Client wartet nicht
	g.searchService.IndexItem(context.Background(), &searchsvc.IndexItemRequest{
		ResourceId:     resourceID,
		ForceOverwrite: forceOverwrite,
	})

	w.WriteHeader(http.StatusAccepted)
}
