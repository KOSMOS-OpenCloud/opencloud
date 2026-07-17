package svc

import (
	"net/http"

	searchsvc "github.com/opencloud-eu/opencloud/protogen/gen/opencloud/services/search/v0"
	"github.com/opencloud-eu/opencloud/services/graph/pkg/errorcode"
)

// ReindexItem triggers re-indexing and re-enrichment of a drive item's space.
// Uses IndexSpace with ForceReindex to re-extract all files in the space
// (existing metadata is protected — only missing keys are written).
//
// POST /drives/{driveID}/items/{itemID}/reindex
func (g Graph) ReindexItem(w http.ResponseWriter, r *http.Request) {
	itemID, err := parseIDParam(r, "itemID")
	if err != nil {
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "invalid itemID")
		return
	}

	spaceID := itemID.GetStorageId() + "$" + itemID.GetSpaceId()

	_, err = g.searchService.IndexSpace(r.Context(), &searchsvc.IndexSpaceRequest{
		SpaceId:      spaceID,
		ForceReindex: true,
	})
	if err != nil {
		g.logger.Error().Err(err).Str("spaceID", spaceID).Msg("reindex failed")
		errorcode.GeneralException.Render(w, r, http.StatusInternalServerError, "reindex failed")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
