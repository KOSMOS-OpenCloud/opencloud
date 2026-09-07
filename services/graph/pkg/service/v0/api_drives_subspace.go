package svc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	gateway "github.com/cs3org/go-cs3apis/cs3/gateway/v1beta1"
	rpc "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	types "github.com/cs3org/go-cs3apis/cs3/types/v1beta1"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	libregraph "github.com/opencloud-eu/libre-graph-api-go"
	revactx "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"github.com/opencloud-eu/reva/v2/pkg/storagespace"
	"github.com/opencloud-eu/reva/v2/pkg/utils"

	"github.com/opencloud-eu/opencloud/services/graph/pkg/errorcode"
	"github.com/opencloud-eu/opencloud/services/graph/pkg/validate"
)

// SubspaceEntry matches the reva node.SubspaceEntry struct.
type SubspaceEntry struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

// ListSubspaces returns the subspace list for a drive.
// GET /drives/{driveID}/subspaces
func (g Graph) ListSubspaces(w http.ResponseWriter, r *http.Request) {
	driveID, err := parseIDParam(r, "driveID")
	if err != nil {
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "invalid driveID")
		return
	}

	gatewayClient, err := g.gatewaySelector.Next()
	if err != nil {
		errorcode.ServiceNotAvailable.Render(w, r, http.StatusServiceUnavailable, "gateway not available")
		return
	}

	space, err := utils.GetSpace(r.Context(), storagespace.FormatResourceID(&driveID), gatewayClient)
	if err != nil || space == nil {
		errorcode.ItemNotFound.Render(w, r, http.StatusNotFound, "space not found")
		return
	}

	entries := subspacesFromOpaque(space.Opaque)
	render.Status(r, http.StatusOK)
	render.JSON(w, r, map[string]any{"value": entries})
}

// SetSubspace marks a folder as a subspace within a drive.
// POST /drives/{driveID}/items/{itemID}/subspace
func (g Graph) SetSubspace(w http.ResponseWriter, r *http.Request) {
	driveID, err := parseIDParam(r, "driveID")
	if err != nil {
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "invalid driveID")
		return
	}
	itemID, err := parseIDParam(r, "itemID")
	if err != nil {
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "invalid itemID")
		return
	}

	ctx := r.Context()
	gatewayClient, err := g.gatewaySelector.Next()
	if err != nil {
		errorcode.ServiceNotAvailable.Render(w, r, http.StatusServiceUnavailable, "gateway not available")
		return
	}

	// Stat the item to get its path
	statRes, err := gatewayClient.Stat(ctx, &provider.StatRequest{
		Ref: &provider.Reference{ResourceId: &itemID},
	})
	if err != nil {
		errorcode.GeneralException.Render(w, r, http.StatusInternalServerError, "could not stat item: "+err.Error())
		return
	}
	if statRes.GetStatus().GetCode() != rpc.Code_CODE_OK {
		errorcode.ItemNotFound.Render(w, r, http.StatusNotFound, "item not found")
		return
	}
	if statRes.GetInfo().GetType() != provider.ResourceType_RESOURCE_TYPE_CONTAINER {
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "only folders can be subspaces")
		return
	}

	itemPath := statRes.GetInfo().GetPath()

	// Call UpdateStorageSpace with subspace.add opaque
	updateReq := &provider.UpdateStorageSpaceRequest{
		StorageSpace: &provider.StorageSpace{
			Id: &provider.StorageSpaceId{
				OpaqueId: storagespace.FormatResourceID(&driveID),
			},
			Root: &driveID,
			Opaque: utils.AppendPlainToOpaque(nil, "subspace.add", itemID.OpaqueId+":"+itemPath),
		},
	}

	resp, err := gatewayClient.UpdateStorageSpace(ctx, updateReq)
	if err != nil {
		errorcode.GeneralException.Render(w, r, http.StatusInternalServerError, "could not update space: "+err.Error())
		return
	}

	switch resp.GetStatus().GetCode() {
	case rpc.Code_CODE_OK:
		render.Status(r, http.StatusOK)
		render.JSON(w, r, SubspaceEntry{ID: itemID.OpaqueId, Path: itemPath})
	case rpc.Code_CODE_PERMISSION_DENIED:
		errorcode.AccessDenied.Render(w, r, http.StatusForbidden, "only managers can manage subspaces")
	case rpc.Code_CODE_INVALID_ARGUMENT:
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, resp.GetStatus().GetMessage())
	default:
		errorcode.GeneralException.Render(w, r, http.StatusInternalServerError, resp.GetStatus().GetMessage())
	}
}

// DeleteSubspace removes the subspace marking from a folder.
// DELETE /drives/{driveID}/items/{itemID}/subspace
func (g Graph) DeleteSubspace(w http.ResponseWriter, r *http.Request) {
	driveID, err := parseIDParam(r, "driveID")
	if err != nil {
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "invalid driveID")
		return
	}
	itemID, err := parseIDParam(r, "itemID")
	if err != nil {
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "invalid itemID")
		return
	}

	ctx := r.Context()
	gatewayClient, err := g.gatewaySelector.Next()
	if err != nil {
		errorcode.ServiceNotAvailable.Render(w, r, http.StatusServiceUnavailable, "gateway not available")
		return
	}

	updateReq := &provider.UpdateStorageSpaceRequest{
		StorageSpace: &provider.StorageSpace{
			Id: &provider.StorageSpaceId{
				OpaqueId: storagespace.FormatResourceID(&driveID),
			},
			Root: &driveID,
			Opaque: utils.AppendPlainToOpaque(nil, "subspace.remove", itemID.OpaqueId),
		},
	}

	resp, err := gatewayClient.UpdateStorageSpace(ctx, updateReq)
	if err != nil {
		errorcode.GeneralException.Render(w, r, http.StatusInternalServerError, "could not update space: "+err.Error())
		return
	}

	switch resp.GetStatus().GetCode() {
	case rpc.Code_CODE_OK:
		w.WriteHeader(http.StatusNoContent)
	case rpc.Code_CODE_PERMISSION_DENIED:
		errorcode.AccessDenied.Render(w, r, http.StatusForbidden, "only managers can manage subspaces")
	default:
		errorcode.GeneralException.Render(w, r, http.StatusInternalServerError, resp.GetStatus().GetMessage())
	}
}

// spaceTypeProject is the value of the space-type xattr for project spaces.
// It mirrors reva's decomposedfs constant so the graph API can detect the
// subspace-creating case without depending on the storage driver.
const spaceTypeProject = "project"

// InviteSubspaceMember adds a user or group as a member of a subspace folder.
// Unlike a normal invite this does not rely on the CS3 grant walk: the caller
// must hold the global ManageSpaceProperties permission (space manager /
// admin), which is checked explicitly here.
// POST /drives/{driveID}/items/{itemID}/subspace/permissions
func (g Graph) InviteSubspaceMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	driveID, err := parseIDParam(r, "driveID")
	if err != nil {
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "invalid driveID")
		return
	}
	itemID, err := parseIDParam(r, "itemID")
	if err != nil {
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "invalid itemID")
		return
	}

	invite := &libregraph.DriveItemInvite{}
	if err = StrictJSONUnmarshal(r.Body, invite); err != nil {
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	if err = validate.StructCtx(ctx, invite); err != nil {
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, err.Error())
		return
	}

	gatewayClient, err := g.gatewaySelector.Next()
	if err != nil {
		errorcode.ServiceNotAvailable.Render(w, r, http.StatusServiceUnavailable, "gateway not available")
		return
	}

	space, err := utils.GetSpace(ctx, storagespace.FormatResourceID(&driveID), gatewayClient)
	if err != nil || space == nil {
		errorcode.ItemNotFound.Render(w, r, http.StatusNotFound, "space not found")
		return
	}
	if space.GetSpaceType() != spaceTypeProject {
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "subspace members are only supported in project spaces")
		return
	}
	// Only a user with the global ManageSpaceProperties permission may manage
	// subspace members. This is the check the normal invite path is missing.
	if err := g.ensureSpaceManagerPermission(ctx, gatewayClient, space); err != nil {
		errorcode.RenderError(w, r, err)
		return
	}

	permission, err := g.subspaceInvite(ctx, &itemID, *invite)
	if err != nil {
		errorcode.RenderError(w, r, err)
		return
	}

	render.Status(r, http.StatusOK)
	render.JSON(w, r, &ListResponse{Value: []any{permission}})
}

// RemoveSubspaceMember removes a member (share) from a subspace folder.
// Analogous to InviteSubspaceMember but for deletion. Removing the last grant
// on a folder deregisters it as a subspace (reva autoRemoveSubspace).
// DELETE /drives/{driveID}/items/{itemID}/subspace/permissions/{permissionID}
func (g Graph) RemoveSubspaceMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	driveID, err := parseIDParam(r, "driveID")
	if err != nil {
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "invalid driveID")
		return
	}
	itemID, err := parseIDParam(r, "itemID")
	if err != nil {
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "invalid itemID")
		return
	}
	permissionID, err := url.PathUnescape(chi.URLParam(r, "permissionID"))
	if err != nil {
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "invalid permissionID")
		return
	}

	gatewayClient, err := g.gatewaySelector.Next()
	if err != nil {
		errorcode.ServiceNotAvailable.Render(w, r, http.StatusServiceUnavailable, "gateway not available")
		return
	}

	space, err := utils.GetSpace(ctx, storagespace.FormatResourceID(&driveID), gatewayClient)
	if err != nil || space == nil {
		errorcode.ItemNotFound.Render(w, r, http.StatusNotFound, "space not found")
		return
	}
	if space.GetSpaceType() != spaceTypeProject {
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "subspace members are only supported in project spaces")
		return
	}
	if err := g.ensureSpaceManagerPermission(ctx, gatewayClient, space); err != nil {
		errorcode.RenderError(w, r, err)
		return
	}

	if err := g.subspaceDeletePermission(ctx, &itemID, permissionID); err != nil {
		errorcode.RenderError(w, r, err)
		return
	}

	render.Status(r, http.StatusNoContent)
	render.NoContent(w, r)
}

// ensureSpaceManagerPermission checks that the acting user holds the global
// ManageSpaceProperties permission (space manager / admin). It checks the
// ManagerRole membership on the space, which is populated from the role that
// grants Drives.ReadWrite (the same check autoAddSubspace uses on the reva side).
func (g Graph) ensureSpaceManagerPermission(ctx context.Context, gwc gateway.GatewayAPIClient, space *provider.StorageSpace) error {
	members, err := utils.GetSpaceMembers(ctx, space.GetId().GetOpaqueId(), gwc, utils.ManagerRole)
	if err != nil {
		return errorcode.New(errorcode.GeneralException, "could not check space manager permission")
	}
	userID := revactx.ContextMustGetUser(ctx).GetId().GetOpaqueId()
	for _, member := range members {
		if member == userID {
			return nil
		}
	}
	return errorcode.New(errorcode.AccessDenied, "only a space manager can manage subspace members")
}

// subspaceInvite performs the grant creation for a subspace member. It reuses
// the DriveItemPermissionsService.InviteWithoutSubspaceCheck logic (which
// creates the share via the gateway) but skips the ensureSubspaceManager check
// because the ManagerRole check was already done in the handler. The Reva-side
// AddGrant bypass (ManageSpaceProperties) is what actually lets the share be
// created without a CS3 grant walk.
func (g Graph) subspaceInvite(ctx context.Context, itemID *provider.ResourceId, invite libregraph.DriveItemInvite) (libregraph.Permission, error) {
	return g.driveItemPermissionsService.InviteWithoutSubspaceCheck(ctx, itemID, invite)
}

// subspaceDeletePermission removes a share by its permissionID on a subspace
// folder. Mirrors DeletePermission but without the role check (already done).
func (g Graph) subspaceDeletePermission(ctx context.Context, _ *provider.ResourceId, permissionID string) error {
	return g.removeUserShare(ctx, permissionID)
}

// ensureSubspaceManager enforces the space-manager role before a grant is
// created when that grant would turn a folder into a subspace. Subspace
// registration only happens for a non-root container in a project space that
// is not already in the subspace registry. For every other invite (space-root
// membership, non-project spaces, files, already-a-subspace folders, or a
// folder that already has grants) this is a no-op, so simple invites are
// unaffected.
func (s DriveItemPermissionsService) ensureSubspaceManager(ctx context.Context, gwc gateway.GatewayAPIClient, itemID *provider.ResourceId, info *provider.ResourceInfo) error {
	// Space-root membership never creates a subspace.
	if IsSpaceRoot(itemID) {
		return nil
	}
	// Only folders can become subspaces.
	if info == nil || info.GetType() != provider.ResourceType_RESOURCE_TYPE_CONTAINER {
		return nil
	}

	spaceID := info.GetSpace().GetId().GetOpaqueId()
	if spaceID == "" {
		return nil
	}
	space, err := utils.GetSpace(ctx, spaceID, gwc)
	if err != nil || space == nil {
		return nil
	}
	// Only project spaces support subspaces.
	if space.GetSpaceType() != spaceTypeProject {
		return nil
	}
	// A folder that is already a subspace: further grants are simple members.
	if containsSubspaceID(subspacesFromOpaque(space.GetOpaque()), info.GetId().GetOpaqueId()) {
		return nil
	}

	// This grant may create a subspace (reva autoAddSubspace will register it
	// if it is the first grant on this folder). The caller must be a space
	// manager; if the folder already has grants but is not yet a subspace the
	// reva-side check will catch the half-baked case.
	return s.ensureSpaceManagerRole(ctx, gwc, space)
}

// containsSubspaceID reports whether the subspace list contains the given id.
func containsSubspaceID(entries []SubspaceEntry, id string) bool {
	for i := range entries {
		if entries[i].ID == id {
			return true
		}
	}
	return false
}

// ensureSpaceManagerRole checks that the current user holds the space-manager
// role on the given space, mirroring the explicit subspace.add path in reva
// (permissions.IsManager). Returns an access-denied error otherwise.
func (s DriveItemPermissionsService) ensureSpaceManagerRole(ctx context.Context, gwc gateway.GatewayAPIClient, space *provider.StorageSpace) error {
	members, err := utils.GetSpaceMembers(ctx, space.GetId().GetOpaqueId(), gwc, utils.ManagerRole)
	if err != nil {
		return err
	}
	userID := revactx.ContextMustGetUser(ctx).GetId().GetOpaqueId()
	for _, member := range members {
		if member == userID {
			return nil
		}
	}
	return errorcode.New(errorcode.AccessDenied, "only a space manager can create a subspace")
}

// subspacesFromOpaque extracts the subspace list from a StorageSpace's opaque data.
func subspacesFromOpaque(opaque *types.Opaque) []SubspaceEntry {
	if opaque == nil {
		return []SubspaceEntry{}
	}
	entry, ok := opaque.Map["subspaces"]
	if !ok {
		return []SubspaceEntry{}
	}
	var entries []SubspaceEntry
	if err := json.Unmarshal(entry.Value, &entries); err != nil {
		return []SubspaceEntry{}
	}
	return entries
}

// SpaceContext describes the effective space/subspace context for a resource.
type SpaceContext struct {
	Type string `json:"type"` // "space" or "subspace"
	ID   string `json:"id"`
	Path string `json:"path"`
}

// GetItemSpaceContext returns the effective space/subspace context for an item.
// GET /drives/{driveID}/items/{itemID}/space
func (g Graph) GetItemSpaceContext(w http.ResponseWriter, r *http.Request) {
	driveID, err := parseIDParam(r, "driveID")
	if err != nil {
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "invalid driveID")
		return
	}
	itemID, err := parseIDParam(r, "itemID")
	if err != nil {
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "invalid itemID")
		return
	}

	ctx := r.Context()
	gatewayClient, err := g.gatewaySelector.Next()
	if err != nil {
		errorcode.ServiceNotAvailable.Render(w, r, http.StatusServiceUnavailable, "gateway not available")
		return
	}

	// Stat the item to get its path
	statRes, err := gatewayClient.Stat(ctx, &provider.StatRequest{
		Ref: &provider.Reference{ResourceId: &itemID},
	})
	if err != nil || statRes.GetStatus().GetCode() != rpc.Code_CODE_OK {
		errorcode.ItemNotFound.Render(w, r, http.StatusNotFound, "item not found")
		return
	}

	itemPath := statRes.GetInfo().GetPath()

	// Get the subspace list for this space
	space, err := utils.GetSpace(ctx, storagespace.FormatResourceID(&driveID), gatewayClient)
	if err != nil || space == nil {
		errorcode.ItemNotFound.Render(w, r, http.StatusNotFound, "space not found")
		return
	}

	subspaces := subspacesFromOpaque(space.GetOpaque())

	// Find the deepest subspace that contains this path
	var best *SubspaceEntry
	for i := range subspaces {
		ss := &subspaces[i]
		if itemPath == ss.Path || strings.HasPrefix(itemPath, ss.Path+"/") {
			if best == nil || len(ss.Path) > len(best.Path) {
				best = ss
			}
		}
	}

	if best != nil {
		render.Status(r, http.StatusOK)
		render.JSON(w, r, SpaceContext{Type: "subspace", ID: best.ID, Path: best.Path})
	} else {
		render.Status(r, http.StatusOK)
		render.JSON(w, r, SpaceContext{Type: "space", ID: driveID.GetOpaqueId(), Path: "/"})
	}
}

// collectSubspaceRootIDs loads subspace root node IDs for all spaces the user
// has access to. These IDs are passed to the share manager so it can filter
// subspace shares at the source (like space root shares).
func (g Graph) collectSubspaceRootIDs(ctx context.Context, gwc gateway.GatewayAPIClient) []string {
	// List all spaces the user can see
	listRes, err := gwc.ListStorageSpaces(ctx, &provider.ListStorageSpacesRequest{})
	if err != nil {
		g.logger.Warn().Err(err).Msg("collectSubspaceRootIDs: failed to list storage spaces, subspace filter will be skipped")
		return nil
	}
	if listRes.GetStatus().GetCode() != rpc.Code_CODE_OK {
		g.logger.Warn().Str("status", listRes.GetStatus().GetCode().String()).Msg("collectSubspaceRootIDs: unexpected status listing storage spaces, subspace filter will be skipped")
		return nil
	}

	var ids []string
	for _, space := range listRes.GetStorageSpaces() {
		for _, ss := range subspacesFromOpaque(space.GetOpaque()) {
			ids = append(ids, ss.ID)
		}
	}
	return ids
}
