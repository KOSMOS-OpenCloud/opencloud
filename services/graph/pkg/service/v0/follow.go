package svc

import (
	"net/http"

	rpc "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/opencloud/services/graph/pkg/errorcode"
	revactx "github.com/opencloud-eu/reva/v2/pkg/ctx"
)

// FollowDriveItem marks a drive item as favorite.
func (g Graph) FollowDriveItem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	itemID, err := parseIDParam(r, "itemID")
	if err != nil {
		g.logger.Debug().Err(err).Msg("could not parse itemID")
		return
	}

	gatewayClient, err := g.gatewaySelector.Next()
	if err != nil {
		g.logger.Error().Err(err).Msg("could not select next gateway client")
		errorcode.ServiceNotAvailable.Render(w, r, http.StatusServiceUnavailable, "could not select next gateway client")
		return
	}

	ref := &provider.Reference{
		ResourceId: &itemID,
	}

	u, ok := revactx.ContextGetUser(ctx)
	if !ok {
		errorcode.GeneralException.Render(w, r, http.StatusUnauthorized, "User not found in context")
		return
	}

	statReq := &provider.StatRequest{
		Ref: ref,
	}
	statRes, err := gatewayClient.Stat(ctx, statReq)
	if err != nil {
		g.logger.Error().Err(err).Msg("could not stat resource")
		errorcode.GeneralException.Render(w, r, http.StatusInternalServerError, "could not stat resource")
		return
	}
	switch statRes.GetStatus().GetCode() {
	case rpc.Code_CODE_OK:
		// continue
	case rpc.Code_CODE_NOT_FOUND:
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "resource not found")
		return
	default:
		errorcode.GeneralException.Render(w, r, http.StatusInternalServerError, "could not stat resource")
		return
	}

	req := &provider.AddFavoriteRequest{
		Ref:    ref,
		UserId: u.Id,
	}

	res, err := gatewayClient.AddFavorite(ctx, req)
	if err != nil {
		g.logger.Error().Err(err).Msg("could not add favorite")
		errorcode.GeneralException.Render(w, r, http.StatusInternalServerError, "could not add favorite")
		return
	}

	if res.Status.Code != rpc.Code_CODE_OK {
		errorcode.GeneralException.Render(w, r, http.StatusInternalServerError, "could not add favorite")
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// UnfollowDriveItem unmarks a drive item as favorite.
func (g Graph) UnfollowDriveItem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	itemID, err := parseIDParam(r, "itemID")
	if err != nil {
		g.logger.Debug().Err(err).Msg("could not parse itemID")
		return
	}

	gatewayClient, err := g.gatewaySelector.Next()
	if err != nil {
		g.logger.Error().Err(err).Msg("could not select next gateway client")
		errorcode.ServiceNotAvailable.Render(w, r, http.StatusServiceUnavailable, "could not select next gateway client")
		return
	}

	ref := &provider.Reference{
		ResourceId: &itemID,
	}

	u, ok := revactx.ContextGetUser(ctx)
	if !ok {
		errorcode.GeneralException.Render(w, r, http.StatusUnauthorized, "User not found in context")
		return
	}

	req := &provider.RemoveFavoriteRequest{
		Ref:    ref,
		UserId: u.Id,
	}

	res, err := gatewayClient.RemoveFavorite(ctx, req)
	if err != nil {
		g.logger.Error().Err(err).Msg("could not remove favorite")
		errorcode.GeneralException.Render(w, r, http.StatusInternalServerError, "could not remove favorite")
		return
	}

	switch res.Status.Code {
	case rpc.Code_CODE_OK:
		// continue
	case rpc.Code_CODE_NOT_FOUND:
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "favorite not found")
		return
	default:
		errorcode.GeneralException.Render(w, r, http.StatusInternalServerError, "could not remove favorite")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
