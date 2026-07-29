package svc

import (
	"context"
	"net/http"
	"strings"

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

	spaceID := itemID.GetStorageId() + "$" + itemID.GetSpaceId()

	g.logger.Info().Str("spaceID", spaceID).Msg("reindex requested")

	// Fire-and-forget — micro-client timeout ist erwartbar, kein Fehler
	go func() {
		_, err := g.searchService.IndexSpace(context.Background(), &searchsvc.IndexSpaceRequest{
			SpaceId:      spaceID,
			ForceReindex: true,
		})
		if err != nil && !strings.Contains(err.Error(), "deadline exceeded") {
			g.logger.Error().Err(err).Str("spaceID", spaceID).Msg("reindex failed")
		}
	}()

	w.WriteHeader(http.StatusAccepted)
}
