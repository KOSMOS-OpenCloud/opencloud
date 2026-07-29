package svc

import (
	"context"
	"net/http"
	"time"

	"go-micro.dev/v4/client"

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

	g.logger.Info().Str("itemID", resourceID).Msg("reindex item requested")

	// Einzelnes Item reindexen — 10min Timeout (LLM-OCR kann dauern)
	go func() {
		_, err := g.searchService.IndexItem(context.Background(), &searchsvc.IndexItemRequest{
			ResourceId: resourceID,
		}, client.WithRequestTimeout(10*time.Minute))
		if err != nil {
			g.logger.Error().Err(err).Str("itemID", resourceID).Msg("reindex item failed")
		} else {
			g.logger.Info().Str("itemID", resourceID).Msg("reindex item complete")
		}
	}()

	w.WriteHeader(http.StatusAccepted)
}
