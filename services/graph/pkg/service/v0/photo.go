package svc

import (
	"net/http"

	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	"github.com/go-chi/render"
	"github.com/opencloud-eu/opencloud/services/graph/pkg/errorcode"
	revactx "github.com/opencloud-eu/reva/v2/pkg/ctx"
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
	g.GetPhoto(w, r, u.GetId())
}

// GetPhoto implements the Service interface
func (g Graph) GetPhoto(w http.ResponseWriter, r *http.Request, u *userpb.UserId) {
	logger := g.logger.SubloggerWithRequestID(r.Context())
	logger.Debug().Msg("GetPhoto called")
	
	// TODO: use proper default return
	render.Status(r, http.StatusNotFound)
	render.JSON(w, r, nil)
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
	g.UpdatePhoto(w, r, u.GetId())
}

// UpdatePhoto implements the Service interface
func (g Graph) UpdatePhoto(w http.ResponseWriter, r *http.Request, u *userpb.UserId) {
	logger := g.logger.SubloggerWithRequestID(r.Context())
	logger.Debug().Msg("UpdatePhoto called")

	// TODO: use proper default return
	render.Status(r, http.StatusForbidden)
	render.JSON(w, r, nil)
}
