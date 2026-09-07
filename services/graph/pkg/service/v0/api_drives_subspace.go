package svc

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	gateway "github.com/cs3org/go-cs3apis/cs3/gateway/v1beta1"
	rpc "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	types "github.com/cs3org/go-cs3apis/cs3/types/v1beta1"
	"github.com/go-chi/render"
	revactx "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"github.com/opencloud-eu/reva/v2/pkg/storagespace"
	"github.com/opencloud-eu/reva/v2/pkg/utils"
	settingsmsg "github.com/opencloud-eu/opencloud/protogen/gen/opencloud/messages/settings/v0"
	settingssvc "github.com/opencloud-eu/opencloud/protogen/gen/opencloud/services/settings/v0"
	"github.com/opencloud-eu/opencloud/services/graph/pkg/errorcode"
	"github.com/opencloud-eu/opencloud/services/settings/pkg/store/defaults"
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

	// The space owner can always manage grants on their own space,
	// including creating subspaces by adding the first grant to a folder.
	userID := revactx.ContextMustGetUser(ctx).GetId().GetOpaqueId()
	if ownerID := space.GetOwner().GetId().GetOpaqueId(); ownerID == userID {
		return nil
	}

	// This grant may create a subspace (reva autoAddSubspace will register it
	// if it is the first grant on this folder). The caller must hold the
	// "Manage space properties" permission (Admin / SpaceAdmin).
	return s.ensureSpaceManagerRole(ctx)
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

// ensureSpaceManagerRole checks that the current user holds the OpenCloud
// "Manage space properties" permission (ManageSpaceProperties with constraint
// All). This is the permission held by the Admin and SpaceAdmin app-roles and
// is the correct basis for subspace management: it is independent of reva CS3
// grants, so an Admin who is not an explicit member/owner of the space still
// passes, while a plain Space-Manager (CS3 RemoveGrant) does not.
// Returns an access-denied error otherwise.
func (s DriveItemPermissionsService) ensureSpaceManagerRole(ctx context.Context) error {
	if s.roleService == nil {
		return errorcode.New(errorcode.AccessDenied, "only a space manager can create a subspace")
	}
	userID := revactx.ContextMustGetUser(ctx).GetId().GetOpaqueId()

	assignments, err := s.roleService.ListRoleAssignments(ctx, &settingssvc.ListRoleAssignmentsRequest{
		AccountUuid: userID,
	})
	if err != nil {
		return err
	}

	roleIDs := make([]string, 0, len(assignments.GetAssignments()))
	for _, a := range assignments.GetAssignments() {
		roleIDs = append(roleIDs, a.GetRoleId())
	}
	if len(roleIDs) == 0 {
		return errorcode.New(errorcode.AccessDenied, "only a space manager can create a subspace")
	}

	bundles, err := s.roleService.ListRoles(ctx, &settingssvc.ListBundlesRequest{
		BundleIds: roleIDs,
	})
	if err != nil {
		return err
	}

	targetSetting := defaults.ManageSpacePropertiesPermission(defaults.All)
	for _, bundle := range bundles.GetBundles() {
		for _, setting := range bundle.GetSettings() {
			if setting.GetId() != targetSetting.GetId() {
				continue
			}
			if pv := setting.GetPermissionValue(); pv != nil && pv.GetConstraint() == settingsmsg.Permission_CONSTRAINT_ALL {
				return nil
			}
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
