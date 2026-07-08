package service

import (
	"net/http"

	revactx "github.com/opencloud-eu/reva/v2/pkg/ctx"

	"codeberg.org/kosmos-openworks/openworks-pipeworx/pkg/engine"
)

// RevaAuthExtractor extracts user identity from reva context (set by ExtractAccountUUID middleware).
type RevaAuthExtractor struct{}

func (a *RevaAuthExtractor) ExtractUser(r *http.Request) (*engine.UserInfo, bool) {
	user, ok := revactx.ContextGetUser(r.Context())
	if !ok || user.GetId() == nil {
		return nil, false
	}

	// Use app-token-label as worker ID if available (set by reva appauth manager).
	// Falls back to user UUID if no token label is present.
	id := user.GetId().GetOpaqueId()
	if user.GetOpaque() != nil {
		if entry, ok := user.GetOpaque().GetMap()["app-token-label"]; ok {
			if label := string(entry.GetValue()); label != "" {
				id = label
			}
		}
	}

	// Admin check: all authenticated users are treated as admin for now.
	// The Web UI restricts access via can('read-all', 'Setting').
	// Server-side role checking requires a gRPC call to the settings service
	// because OIDC JWTs don't carry role assignments in the opaque.
	return &engine.UserInfo{
		ID:      id,
		IsAdmin: true,
	}, true
}
