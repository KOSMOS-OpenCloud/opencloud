package svc

import (
	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/opencloud-eu/opencloud/pkg/systemstorageclient"
	"github.com/opencloud-eu/opencloud/services/graph/pkg/errorcode"
	revactx "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"io"
	"net/http"
	"net/url"
)

var (
	namespace  = "profilephoto"
	scope      = "user"
	identifier = "profilephoto"
)

// GetMePhoto implements the Service interface
func (g Graph) GetMePhoto(w http.ResponseWriter, r *http.Request) {
	logger := g.logger.SubloggerWithRequestID(r.Context())
	logger.Debug().Msg("GetMePhoto called")
	u, ok := revactx.ContextGetUser(r.Context())
	if !ok {
		logger.Debug().Msg("could not get user: user not in context")
		errorcode.GeneralException.Render(w, r, http.StatusInternalServerError, "user not in context")
		return
	}
	g.getPhoto(w, r, u.GetId())
}

// GetPhoto implements the Service interface
func (g Graph) GetPhoto(w http.ResponseWriter, r *http.Request) {
	logger := g.logger.SubloggerWithRequestID(r.Context())
	logger.Debug().Msg("GetPhoto called")
	userID, err := url.PathUnescape(chi.URLParam(r, "userID"))
	if err != nil {
		logger.Debug().Err(err).Str("userID", chi.URLParam(r, "userID")).Msg("could not get drive: unescaping drive id failed")
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "unescaping user id failed")
		return
	}
	g.getPhoto(w, r, &userpb.UserId{
		OpaqueId: userID,
	})
}

func (g Graph) getPhoto(w http.ResponseWriter, r *http.Request, u *userpb.UserId) {
	logger := g.logger.SubloggerWithRequestID(r.Context())
	logger.Debug().Msg("GetPhoto called")
	g.getSystemStorageClient()
	photo, err := g.sdsc.SimpleDownload(r.Context(), u.GetOpaqueId(), identifier)
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, nil)
	}
	render.Status(r, http.StatusOK)
	render.JSON(w, r, photo)

}

// UpdateMePhoto implements the Service interface
func (g Graph) UpdateMePhoto(w http.ResponseWriter, r *http.Request) {
	logger := g.logger.SubloggerWithRequestID(r.Context())
	logger.Debug().Msg("UpdateMePhoto called")
	u, ok := revactx.ContextGetUser(r.Context())
	if !ok {
		logger.Debug().Msg("could not get user: user not in context")
		errorcode.GeneralException.Render(w, r, http.StatusInternalServerError, "user not in context")
		return
	}
	g.updatePhoto(w, r, u.GetId())
}

// UpdatePhoto implements the Service interface
func (g Graph) UpdatePhoto(w http.ResponseWriter, r *http.Request) {
	logger := g.logger.SubloggerWithRequestID(r.Context())
	logger.Debug().Msg("UpdatePhoto called")
	userID, err := url.PathUnescape(chi.URLParam(r, "userID"))
	if err != nil {
		logger.Debug().Err(err).Str("userID", chi.URLParam(r, "userID")).Msg("could not get drive: unescaping drive id failed")
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "unescaping user id failed")
		return
	}
	g.updatePhoto(w, r, &userpb.UserId{
		OpaqueId: userID,
	})
}

func (g Graph) updatePhoto(w http.ResponseWriter, r *http.Request, u *userpb.UserId) {
	logger := g.logger.SubloggerWithRequestID(r.Context())
	logger.Debug().Msg("UpdatePhoto called")
	client := g.getSystemStorageClient()
	content, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Debug().Err(err).Msg("could not read body")
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "could not read body")
		return
	}
	if len(content) == 0 {
		logger.Debug().Msg("could not read body: empty body")
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "empty body")
		return
	}
	err = client.SimpleUpload(r.Context(), u.GetOpaqueId(), identifier, content)
	if err != nil {
		logger.Debug().Err(err).Msg("could not upload photo")
		errorcode.GeneralException.Render(w, r, http.StatusInternalServerError, "could not upload photo")
		return
	}
	render.Status(r, http.StatusOK)
	render.JSON(w, r, nil)
}

func (g Graph) getSystemStorageClient() systemstorageclient.SystemDataStorageClient {
	// TODO: this needs a check if the client is already initialized and if not, initialize it
	g.sdsc = *systemstorageclient.NewSystemStorageClient(
		scope,
		namespace,
		g.logger,
		g.config.SystemStorageClient.GatewayAddress,
		g.config.SystemStorageClient.StorageAddress,
		g.config.SystemStorageClient.SystemUserID,
		g.config.SystemStorageClient.SystemUserIDP,
		g.config.SystemStorageClient.SystemUserAPIKey,
	)
	return g.sdsc
}
