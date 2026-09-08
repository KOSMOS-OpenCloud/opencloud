package svc

import (
	"fmt"
	"net/http"

	searchsvc "github.com/opencloud-eu/opencloud/protogen/gen/opencloud/services/search/v0"
	"github.com/opencloud-eu/opencloud/services/graph/pkg/errorcode"
)

// ReindexItem triggers re-indexing of a single drive item.
// Synchron: wartet bis die Taki-Extraktion abgeschlossen ist.
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

	g.logger.Info().Str("itemID", resourceID).Bool("overwrite", forceOverwrite).Msg("reindex item requested (sync)")

	// Synchron: wartet bis Completion (gRPC-Service blockiert bis Enrichment fertig)
	_, err = g.searchService.IndexItem(r.Context(), &searchsvc.IndexItemRequest{
		ResourceId:     resourceID,
		ForceOverwrite: forceOverwrite,
	})
	if err != nil {
		g.logger.Error().Err(err).Str("itemID", resourceID).Msg("reindex item failed")
		errorcode.GeneralException.Render(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"status":"completed"}`)
}
