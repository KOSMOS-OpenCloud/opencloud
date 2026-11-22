// Package service connects the Craft Signal relay (the hub from github.com/flash7777/craft-signal-relay) to OpenCloud: who asks
// comes from the reva token the proxy puts on the request, what they may do with a document from a stat
// through the gateway with that same token.
package service

import (
	"errors"
	"net/http"
	"slices"

	gateway "github.com/cs3org/go-cs3apis/cs3/gateway/v1beta1"
	rpc "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	revactx "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/todo/pool"
	"github.com/opencloud-eu/reva/v2/pkg/storagespace"
	"google.golang.org/grpc/metadata"

	relay "github.com/flash7777/craft-signal-relay/signal"

	"github.com/opencloud-eu/opencloud/pkg/log"
)

// Stat is the one gateway call the relay needs: the rights of the asking user on a document.
type Stat func(r *http.Request, id *provider.ResourceId) (*provider.ResourceInfo, error)

// Hooks are the relay's questions to OpenCloud.
type Hooks struct {
	Logger log.Logger
	Stat   Stat
	// Operators and HiddenUsers are user ids (see the config of the service).
	Operators   []string
	HiddenUsers []string
}

// GatewayStat stats with the token of the request.
func GatewayStat(selector *pool.Selector[gateway.GatewayAPIClient]) Stat {
	return func(r *http.Request, id *provider.ResourceId) (*provider.ResourceInfo, error) {
		client, err := selector.Next()
		if err != nil {
			return nil, err
		}
		ctx := metadata.AppendToOutgoingContext(r.Context(), revactx.TokenHeader, r.Header.Get(revactx.TokenHeader))
		res, err := client.Stat(ctx, &provider.StatRequest{Ref: &provider.Reference{ResourceId: id}})
		if err != nil {
			return nil, err
		}
		if res.GetStatus().GetCode() != rpc.Code_CODE_OK {
			return nil, errors.New(res.GetStatus().GetMessage())
		}
		return res.GetInfo(), nil
	}
}

// Options are the relay options with every hook set: nothing is open without a signed-in user and nothing
// about a document without the right to read it.
func (h Hooks) Options(blobBudget int) relay.Options {
	options := relay.Options{
		BlobBudget:  blobBudget,
		Authorize:   h.authorize,
		DisplayName: h.displayName,
		Rights:      h.rights,
		Operator:    func(user string) bool { return slices.Contains(h.Operators, user) },
	}
	// Not set means switched off on this instance: the relay then refuses every hidden ticket.
	if len(h.HiddenUsers) > 0 {
		options.Hidden = func(user string) bool { return slices.Contains(h.HiddenUsers, user) }
	}
	return options
}

// authorize names the user of the request by its id; the account middleware put it there from the token.
func (h Hooks) authorize(r *http.Request) (string, error) {
	user, ok := revactx.ContextGetUser(r.Context())
	if !ok || user.GetId().GetOpaqueId() == "" {
		return "", errors.New("no user")
	}
	return user.GetId().GetOpaqueId(), nil
}

func (h Hooks) displayName(r *http.Request) string {
	user, ok := revactx.ContextGetUser(r.Context())
	if !ok {
		return ""
	}
	if name := user.GetDisplayName(); name != "" {
		return name
	}
	return user.GetUsername()
}

// rights reads what the user may do with the document `doc` (a resource id as the clients have it,
// `storage$space!item`): edit when they may upload a new version, read when the stat answers at all.
func (h Hooks) rights(r *http.Request, user, doc string) (uint8, bool) {
	id, err := storagespace.ParseID(doc)
	if err != nil || id.GetOpaqueId() == "" {
		h.Logger.Debug().Str("doc", doc).Msg("signal: not a resource id")
		return relay.ModeReadOnly, false
	}
	info, err := h.Stat(r, &id)
	if err != nil || info == nil {
		h.Logger.Debug().Err(err).Str("doc", doc).Str("user", user).Msg("signal: no access to the document")
		return relay.ModeReadOnly, false
	}
	if info.GetPermissionSet().GetInitiateFileUpload() {
		return relay.ModeEdit, true
	}
	return relay.ModeReadOnly, true
}
