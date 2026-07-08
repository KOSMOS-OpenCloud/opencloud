package service

import (
	"encoding/json"
	"net/http"

	revactx "github.com/opencloud-eu/reva/v2/pkg/ctx"

	"codeberg.org/kosmos-openworks/openworks-pipeworx/pkg/engine"
)

// BundleUUIDRoleAdmin is the OpenCloud admin role UUID (from settings service defaults).
const BundleUUIDRoleAdmin = "71881883-1768-46bd-a24d-a356a2afdf7f"

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

		// Check if user has admin role (set by proxy role assigner).
		if entry, ok := user.GetOpaque().GetMap()["roles"]; ok {
			var roles []string
			if json.Unmarshal(entry.GetValue(), &roles) == nil {
				for _, r := range roles {
					if r == BundleUUIDRoleAdmin {
						isAdmin = true
						break
					}
				}
			}
		}
	}

	return &engine.UserInfo{
		ID:      id,
		IsAdmin: isAdmin,
	}, true
}
