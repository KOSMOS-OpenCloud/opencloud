package service

import (
	"fmt"
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
		opaqueKeys := make([]string, 0, len(user.GetOpaque().GetMap()))
		for k := range user.GetOpaque().GetMap() {
			opaqueKeys = append(opaqueKeys, k)
		}
		fmt.Printf("[reva_auth] user=%s opaque_keys=%v\n", id, opaqueKeys)
		if entry, ok := user.GetOpaque().GetMap()["app-token-label"]; ok {
			label := string(entry.GetValue())
			fmt.Printf("[reva_auth] app-token-label found: %q decoder=%s\n", label, entry.GetDecoder())
			if label != "" {
				id = label
			}
		} else {
			fmt.Printf("[reva_auth] app-token-label NOT in opaque\n")
		}
	} else {
		fmt.Printf("[reva_auth] user=%s opaque=nil\n", id)
	}

	return &engine.UserInfo{
		ID:      id,
		IsAdmin: true, // TODO: integrate with OpenCloud role system
	}, true
}
