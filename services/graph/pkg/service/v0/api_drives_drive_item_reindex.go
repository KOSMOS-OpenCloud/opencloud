package svc

import (
	"net/http"

	searchsvc "github.com/opencloud-eu/opencloud/protogen/gen/opencloud/services/search/v0"
	"github.com/opencloud-eu/opencloud/services/graph/pkg/errorcode"
)

// ReindexItem triggers re-indexing and re-enrichment of a single drive item.
//
// POST /drives/{driveID}/items/{itemID}/reindex
func (g Graph) ReindexItem(w http.ResponseWriter, r *http.Request) {
	itemID, err := parseIDParam(r, "itemID")
	if err != nil {
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "invalid itemID")
		return
	}

	resourceID := itemID.GetStorageId() + "$" + itemID.GetSpaceId() + "!" + itemID.GetOpaqueId()

	_, err = g.searchService.IndexItem(r.Context(), &searchsvc.IndexItemRequest{
		ResourceId: resourceID,
	})
	if err != nil {
		g.logger.Error().Err(err).Str("resourceID", resourceID).Msg("reindex failed")
		errorcode.GeneralException.Render(w, r, http.StatusInternalServerError, "reindex failed")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
