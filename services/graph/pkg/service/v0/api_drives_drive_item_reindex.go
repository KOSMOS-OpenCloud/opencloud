package svc

import (
	"context"
	"net/http"

	searchsvc "github.com/opencloud-eu/opencloud/protogen/gen/opencloud/services/search/v0"
	"github.com/opencloud-eu/opencloud/services/graph/pkg/errorcode"
)

// ReindexItem triggers re-indexing and re-enrichment of a drive item's space.
// Runs asynchronously — returns 202 Accepted immediately.
//
// POST /drives/{driveID}/items/{itemID}/reindex
func (g Graph) ReindexItem(w http.ResponseWriter, r *http.Request) {
	itemID, err := parseIDParam(r, "itemID")
	if err != nil {
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "invalid itemID")
		return
	}

	spaceID := itemID.GetStorageId() + "$" + itemID.GetSpaceId()

	// Run async — IndexSpace can take a long time for large spaces
	go func() {
		_, err := g.searchService.IndexSpace(context.Background(), &searchsvc.IndexSpaceRequest{
			SpaceId:      spaceID,
			ForceReindex: true,
		})
		if err != nil {
			g.logger.Error().Err(err).Str("spaceID", spaceID).Msg("async reindex failed")
		} else {
			g.logger.Info().Str("spaceID", spaceID).Msg("async reindex complete")
		}
	}()

	w.WriteHeader(http.StatusAccepted)
}
