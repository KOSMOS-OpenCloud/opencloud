package service

import (
	"encoding/json"
	"net/http"

	revactx "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"go-micro.dev/v4/metadata"

	"codeberg.org/kosmos-openworks/openworks-pipeworx/pkg/engine"
)

// BundleUUIDRoleAdmin is the OpenCloud admin role UUID (from settings service defaults).
const BundleUUIDRoleAdmin = "71881883-1768-46bd-a24d-a356a2afdf7f"

// RoleIDs is the metadata key for roles (set by ExtractAccountUUID middleware).
const RoleIDs = "Role-Ids"

// RevaAuthExtractor extracts user identity from reva context (set by ExtractAccountUUID middleware).
type RevaAuthExtractor struct{}

func (a *RevaAuthExtractor) ExtractUser(r *http.Request) (*engine.UserInfo, bool) {
	user, ok := revactx.ContextGetUser(r.Context())
	if !ok || user.GetId() == nil {
		return nil, false
	}

	id := user.GetId().GetOpaqueId()
	isAdmin := false

	if user.GetOpaque() != nil {
		// Use app-token-label as worker ID if available (set by reva appauth manager).
		if entry, ok := user.GetOpaque().GetMap()["app-token-label"]; ok {
			if label := string(entry.GetValue()); label != "" {
				id = label
			}
		}
	}

	// Check admin role from context metadata (set by ExtractAccountUUID middleware
	// which reads roles from JWT opaque OR from proxy role assigner).
	// This works for both OIDC and app-token auth flows.
	if rolesJSON, ok := metadata.Get(r.Context(), RoleIDs); ok {
		var roles []string
		if json.Unmarshal([]byte(rolesJSON), &roles) == nil {
			for _, role := range roles {
				if role == BundleUUIDRoleAdmin {
					isAdmin = true
					break
				}
			}
		}
	}

	return &engine.UserInfo{
		ID:      id,
		IsAdmin: isAdmin,
	}, true
}
