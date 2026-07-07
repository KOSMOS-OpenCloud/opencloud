package svc

import (
	"encoding/json"
	"io"
	"net/http"

	rpc "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	searchsvc "github.com/opencloud-eu/opencloud/protogen/gen/opencloud/services/search/v0"
	"github.com/opencloud-eu/opencloud/services/graph/pkg/errorcode"
	revaCtx "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"github.com/opencloud-eu/reva/v2/pkg/storagespace"
	"go-micro.dev/v4/metadata"
)

// Rendition represents a derived file linked to its source document.
type Rendition struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	MimeType      string `json:"mimeType,omitempty"`
	Size          uint64 `json:"size,omitempty"`
	Type          string `json:"type,omitempty"`
	Profile       string `json:"profile,omitempty"`
	Valid         string `json:"valid,omitempty"`
	Pages         string `json:"pages,omitempty"`
	SourceVersion string `json:"sourceVersion,omitempty"`
	CreatedBy     string `json:"createdBy,omitempty"`
	CreatedAt     string `json:"createdAt,omitempty"`
}

// GetItemRenditions returns all renditions of a drive item.
// Renditions are files that have metadata key "rendition.source" pointing to this item's ID.
// The search is performed via the search service (Bleve), which indexes all ArbitraryMetadata
// keys dynamically.
//
// GET /drives/{driveID}/items/{itemID}/renditions
//
// Response: [{ "id": "...", "name": "brief.pdf", "type": "pdf/a", ... }]
func (g Graph) GetItemRenditions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	_, itemID, err := GetDriveAndItemIDParam(r, g.logger)
	if err != nil {
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, err.Error())
		return
	}

	// Build the search query: find all files where rendition.source matches this item's opaque ID
	sourceID := storagespace.FormatResourceID(itemID)
	query := `Metadata.rendition\.source:` + sourceID

	// Forward auth token for the search service
	th := r.Header.Get(revaCtx.TokenHeader)
	searchCtx := revaCtx.ContextSetToken(ctx, th)
	searchCtx = metadata.Set(searchCtx, revaCtx.TokenHeader, th)

	sr, err := g.searchService.Search(searchCtx, &searchsvc.SearchRequest{
		Query:    query,
		PageSize: 100,
	})
	if err != nil {
		g.logger.Error().Err(err).Str("query", query).Msg("renditions: search failed")
		errorcode.GeneralException.Render(w, r, http.StatusInternalServerError, "search failed")
		return
	}

	gatewayClient, err := g.gatewaySelector.Next()
	if err != nil {
		errorcode.ServiceNotAvailable.Render(w, r, http.StatusServiceUnavailable, "gateway not available")
		return
	}

	renditions := make([]Rendition, 0, len(sr.GetMatches()))
	for _, match := range sr.GetMatches() {
		entity := match.GetEntity()
		if entity == nil {
			continue
		}

		renditionID := entity.GetId()
		if renditionID == nil {
			continue
		}

		rid := &provider.ResourceId{
			StorageId: renditionID.GetStorageId(),
			SpaceId:   renditionID.GetSpaceId(),
			OpaqueId:  renditionID.GetOpaqueId(),
		}

		// Stat the rendition file to get its ArbitraryMetadata
		statRes, err := gatewayClient.Stat(ctx, &provider.StatRequest{
			Ref:                   &provider.Reference{ResourceId: rid},
			ArbitraryMetadataKeys: []string{"*"},
		})
		if err != nil || statRes.GetStatus().GetCode() != rpc.Code_CODE_OK {
			continue // skip files we can't stat
		}

		info := statRes.GetInfo()
		md := make(map[string]string)
		if am := info.GetArbitraryMetadata(); am != nil {
			md = am.GetMetadata()
		}

		renditions = append(renditions, Rendition{
			ID:            storagespace.FormatResourceID(info.GetId()),
			Name:          info.GetName(),
			MimeType:      info.GetMimeType(),
			Size:          info.GetSize(),
			Type:          md["rendition.type"],
			Profile:       md["rendition.profile"],
			Valid:         md["rendition.valid"],
			Pages:         md["rendition.pages"],
			SourceVersion: md["rendition.sourceVersion"],
			CreatedBy:     md["rendition.createdBy"],
			CreatedAt:     md["rendition.createdAt"],
		})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(renditions); err != nil {
		g.logger.Error().Err(err).Msg("renditions: could not encode response")
	}
}

// SetItemRendition marks an existing file as a rendition of the given source item.
// Sets rendition.* metadata keys on the rendition file via SetArbitraryMetadata.
//
// POST /drives/{driveID}/items/{itemID}/renditions
//
// Request body: { "renditionId": "...", "type": "pdf/a", "profile": "2b", ... }
func (g Graph) SetItemRendition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	_, itemID, err := GetDriveAndItemIDParam(r, g.logger)
	if err != nil {
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, err.Error())
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "could not read request body")
		return
	}
	defer r.Body.Close()

	var req struct {
		RenditionID   string `json:"renditionId"`
		Type          string `json:"type"`
		Profile       string `json:"profile"`
		Valid         string `json:"valid"`
		Pages         string `json:"pages"`
		SourceVersion string `json:"sourceVersion"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if req.RenditionID == "" {
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "renditionId is required")
		return
	}

	// Parse the rendition file's resource ID
	renditionRID, err := storagespace.ParseID(req.RenditionID)
	if err != nil {
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "invalid renditionId")
		return
	}

	gatewayClient, err := g.gatewaySelector.Next()
	if err != nil {
		errorcode.ServiceNotAvailable.Render(w, r, http.StatusServiceUnavailable, "gateway not available")
		return
	}

	// Build metadata map
	sourceID := storagespace.FormatResourceID(itemID)
	md := map[string]string{
		"rendition.source": sourceID,
	}
	if req.Type != "" {
		md["rendition.type"] = req.Type
	}
	if req.Profile != "" {
		md["rendition.profile"] = req.Profile
	}
	if req.Valid != "" {
		md["rendition.valid"] = req.Valid
	}
	if req.Pages != "" {
		md["rendition.pages"] = req.Pages
	}
	if req.SourceVersion != "" {
		md["rendition.sourceVersion"] = req.SourceVersion
	}

	// Set rendition metadata on the rendition file
	res, err := gatewayClient.SetArbitraryMetadata(ctx, &provider.SetArbitraryMetadataRequest{
		Ref: &provider.Reference{ResourceId: &renditionRID},
		ArbitraryMetadata: &provider.ArbitraryMetadata{
			Metadata: md,
		},
	})
	if err != nil {
		g.logger.Error().Err(err).Msg("renditions: SetArbitraryMetadata error")
		errorcode.GeneralException.Render(w, r, http.StatusInternalServerError, "could not set rendition metadata")
		return
	}
	switch res.GetStatus().GetCode() {
	case rpc.Code_CODE_OK:
		w.WriteHeader(http.StatusCreated)
	case rpc.Code_CODE_NOT_FOUND:
		errorcode.ItemNotFound.Render(w, r, http.StatusNotFound, "rendition file not found")
	case rpc.Code_CODE_PERMISSION_DENIED:
		errorcode.AccessDenied.Render(w, r, http.StatusForbidden, "permission denied")
	default:
		errorcode.GeneralException.Render(w, r, http.StatusInternalServerError, res.GetStatus().GetMessage())
	}
}
