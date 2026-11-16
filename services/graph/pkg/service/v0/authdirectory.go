package svc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"

	"github.com/opencloud-eu/opencloud/pkg/shared"
	"github.com/opencloud-eu/opencloud/services/graph/pkg/errorcode"
)

// internalAuthDirectory is the reserved value meaning "authenticate against the
// built-in idm". It is stored as the empty/absent attribute value.
const internalAuthDirectory = "internal"

// AuthDirectoryBackend is the optional capability of an identity backend to read
// and set the per-user auth directory (OpenCore extension). Only the LDAP
// backend implements it; see opendock/OPENCORE_DIRECTORY.md.
type AuthDirectoryBackend interface {
	GetUserAuthDirectory(ctx context.Context, nameOrID string) (string, error)
	SetUserAuthDirectory(ctx context.Context, nameOrID, directory string) error
}

type authDirectoryBody struct {
	AuthDirectory string `json:"authDirectory"`
}

// authDirectoryItem is one selectable auth directory for the user editor.
type authDirectoryItem struct {
	Name  string `json:"name"`
	Label string `json:"label"`
}

// authDirectoryList is the response for GET /extensions/opencore/authDirectories.
type authDirectoryList struct {
	Value []authDirectoryItem `json:"value"`
}

// ListAuthDirectories returns the auth directories an admin can assign to a user:
// the built-in "internal" pseudo-entry first, then every configured external
// directory. OpenCore extension; the user editor uses this to populate the auth
// source dropdown. Connection details are never exposed, only name and label.
func (g Graph) ListAuthDirectories(w http.ResponseWriter, r *http.Request) {
	var directories []shared.Directory
	if g.config.Commons != nil {
		directories = g.config.Commons.Directories
	}
	items := make([]authDirectoryItem, 0, len(directories)+1)
	items = append(items, authDirectoryItem{Name: internalAuthDirectory, Label: internalAuthDirectory})
	for _, dir := range directories {
		label := dir.Label
		if label == "" {
			label = dir.Name
		}
		items = append(items, authDirectoryItem{Name: dir.Name, Label: label})
	}
	render.Status(r, http.StatusOK)
	render.JSON(w, r, authDirectoryList{Value: items})
}

func authDirectoryUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	userID, err := url.PathUnescape(chi.URLParam(r, "userID"))
	if err != nil || userID == "" {
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "invalid user id")
		return "", false
	}
	return userID, true
}

// GetUserAuthDirectory returns the auth directory assigned to a user. "internal"
// means the built-in idm. OpenCore extension.
func (g Graph) GetUserAuthDirectory(w http.ResponseWriter, r *http.Request) {
	logger := g.logger.SubloggerWithRequestID(r.Context())
	backend, ok := g.identityBackend.(AuthDirectoryBackend)
	if !ok {
		errorcode.NotSupported.Render(w, r, http.StatusNotImplemented, "auth directory not supported by this identity backend")
		return
	}
	userID, ok := authDirectoryUserID(w, r)
	if !ok {
		return
	}
	dir, err := backend.GetUserAuthDirectory(r.Context(), userID)
	if err != nil {
		logger.Debug().Err(err).Str("userID", userID).Msg("could not get auth directory")
		errorcode.RenderError(w, r, err)
		return
	}
	if dir == "" {
		dir = internalAuthDirectory
	}
	render.Status(r, http.StatusOK)
	render.JSON(w, r, authDirectoryBody{AuthDirectory: dir})
}

// SetUserAuthDirectory assigns the auth directory of a user. "internal" (or an
// empty value) stores the user as internal (idm). Any other value is the name of
// a configured external directory; validity is enforced by the IdP at login.
func (g Graph) SetUserAuthDirectory(w http.ResponseWriter, r *http.Request) {
	logger := g.logger.SubloggerWithRequestID(r.Context())
	backend, ok := g.identityBackend.(AuthDirectoryBackend)
	if !ok {
		errorcode.NotSupported.Render(w, r, http.StatusNotImplemented, "auth directory not supported by this identity backend")
		return
	}
	userID, ok := authDirectoryUserID(w, r)
	if !ok {
		return
	}
	var body authDirectoryBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "invalid request body")
		return
	}

	dir := strings.TrimSpace(body.AuthDirectory)
	if strings.EqualFold(dir, internalAuthDirectory) {
		dir = "" // internal is stored as the empty/absent value
	}
	if err := backend.SetUserAuthDirectory(r.Context(), userID, dir); err != nil {
		logger.Debug().Err(err).Str("userID", userID).Msg("could not set auth directory")
		errorcode.RenderError(w, r, err)
		return
	}

	result := dir
	if result == "" {
		result = internalAuthDirectory
	}
	render.Status(r, http.StatusOK)
	render.JSON(w, r, authDirectoryBody{AuthDirectory: result})
}
