package svc

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"

	"github.com/opencloud-eu/opencloud/pkg/shared"
	"github.com/opencloud-eu/opencloud/services/graph/pkg/directorylookup"
	"github.com/opencloud-eu/opencloud/services/graph/pkg/errorcode"
)

// authDirectoryLookupResult is the response for the username lookup. It reports
// only whether the login exists in the external directory, never any attribute
// value (E9, security: the external directory is queried anonymously and no
// attributes are requested).
type authDirectoryLookupResult struct {
	Directory string `json:"directory"`
	Login     string `json:"login"`
	Exists    bool   `json:"exists"`
}

// LookupAuthDirectoryUser checks whether a login exists in a configured external
// directory. The user editor uses this before assigning a user to that directory
// (E9): the OpenCore login must equal the directory's uid, so an admin needs to
// confirm the account is actually there. OpenCore extension.
//
// GET /graph/v1.0/extensions/opencore/authDirectories/{directory}/users/{login}
func (g Graph) LookupAuthDirectoryUser(w http.ResponseWriter, r *http.Request) {
	logger := g.logger.SubloggerWithRequestID(r.Context())

	name, err := url.PathUnescape(chi.URLParam(r, "directory"))
	if err != nil || name == "" {
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "invalid directory")
		return
	}
	login, err := url.PathUnescape(chi.URLParam(r, "login"))
	if err != nil || login == "" {
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "invalid login")
		return
	}

	// "internal" means the built-in idm, which is not looked up here: this endpoint
	// is only for confirming a login in an external directory before delegation.
	if strings.EqualFold(name, internalAuthDirectory) {
		errorcode.InvalidRequest.Render(w, r, http.StatusBadRequest, "lookup is only supported for external directories")
		return
	}

	dir, ok := g.findAuthDirectory(name)
	if !ok {
		errorcode.ItemNotFound.Render(w, r, http.StatusNotFound, "unknown directory")
		return
	}

	exists, lookupErr := directorylookup.Lookup(dir, login)
	if lookupErr != nil {
		// A transport/other error is not a negative answer: surface it so the editor
		// does not let an admin assign a user based on a failed lookup.
		logger.Debug().Err(lookupErr).Str("directory", name).Msg("auth directory lookup failed")
		errorcode.GeneralException.Render(w, r, http.StatusBadGateway, "could not reach the external directory")
		return
	}

	render.Status(r, http.StatusOK)
	render.JSON(w, r, authDirectoryLookupResult{Directory: dir.Name, Login: login, Exists: exists})
}

// findAuthDirectory resolves a configured external directory by name
// (case-insensitive). "internal" is never a configured directory.
func (g Graph) findAuthDirectory(name string) (shared.Directory, bool) {
	if g.config.Commons == nil {
		return shared.Directory{}, false
	}
	want := strings.ToLower(strings.TrimSpace(name))
	for _, dir := range g.config.Commons.Directories {
		if strings.ToLower(strings.TrimSpace(dir.Name)) == want {
			return dir, true
		}
	}
	return shared.Directory{}, false
}
