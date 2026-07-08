package service

import (
	"encoding/json"
	"fmt"
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

	// Debug: log opaque keys and roles
	if user.GetOpaque() != nil {
		keys := make([]string, 0)
		for k := range user.GetOpaque().GetMap() {
			keys = append(keys, k)
		}
		fmt.Printf("[reva_auth] user=%s opaque_keys=%v\n", id, keys)
		if entry, ok := user.GetOpaque().GetMap()["roles"]; ok {
			fmt.Printf("[reva_auth] roles raw=%q decoder=%s\n", string(entry.GetValue()), entry.GetDecoder())
		}
	} else {
		fmt.Printf("[reva_auth] user=%s opaque=nil\n", id)
	}

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
