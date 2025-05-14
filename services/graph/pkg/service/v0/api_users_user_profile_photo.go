package svc

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/render"
	revactx "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"github.com/opencloud-eu/reva/v2/pkg/storage/utils/metadata"

	"github.com/opencloud-eu/opencloud/pkg/log"
	"github.com/opencloud-eu/opencloud/services/graph/pkg/errorcode"
)

type (
	// UsersUserProfilePhotoProvider is the interface that defines the methods for the user profile photo service
	UsersUserProfilePhotoProvider interface {
		// GetPhoto retrieves the requested photo
		GetPhoto(ctx context.Context, id string) ([]byte, error)

		// UpdatePhoto retrieves the requested photo
		UpdatePhoto(ctx context.Context, id string, rc io.Reader) error

		// DeletePhoto deletes the requested photo
		DeletePhoto(ctx context.Context, id string) error
	}
)

var (
	// profilePhotoSpaceID is the space ID for the profile photo
	profilePhotoSpaceID = "f2bdd61a-da7c-49fc-8203-0558109d1b4f"

	// ErrNoBytes is returned when no bytes are found
	ErrNoBytes = errors.New("no bytes")

	// ErrNoUser is returned when no user is found
	ErrNoUser = errors.New("no user found")
)

// UsersUserProfilePhotoService is the implementation of the UsersUserProfilePhotoProvider interface
type UsersUserProfilePhotoService struct {
	storage metadata.Storage
}

// NewUsersUserProfilePhotoService creates a new UsersUserProfilePhotoService
func NewUsersUserProfilePhotoService(storage metadata.Storage) (UsersUserProfilePhotoService, error) {
	if err := storage.Init(context.Background(), profilePhotoSpaceID); err != nil {
		return UsersUserProfilePhotoService{}, err
	}

	return UsersUserProfilePhotoService{
		storage: storage,
	}, nil
}

// GetPhoto retrieves the requested photo
func (s UsersUserProfilePhotoService) GetPhoto(ctx context.Context, id string) ([]byte, error) {
	photo, err := s.storage.SimpleDownload(ctx, id)
	if err != nil {
		return nil, err
	}

	return photo, nil
}

// DeletePhoto deletes the requested photo
func (s UsersUserProfilePhotoService) DeletePhoto(ctx context.Context, id string) error {
	return s.storage.Delete(ctx, id)
}

// UpdatePhoto updates the requested photo
func (s UsersUserProfilePhotoService) UpdatePhoto(ctx context.Context, id string, rc io.Reader) error {
	photo, err := io.ReadAll(rc)
	if err != nil {
		return err
	}

	if len(photo) == 0 {
		return ErrNoBytes
	}

	return s.storage.SimpleUpload(ctx, id, photo)
}

// UsersUserProfilePhotoApi contains all photo related api endpoints
type UsersUserProfilePhotoApi struct {
	logger                       log.Logger
	usersUserProfilePhotoService UsersUserProfilePhotoProvider
}

// NewUsersUserProfilePhotoApi creates a new UsersUserProfilePhotoApi
func NewUsersUserProfilePhotoApi(usersUserProfilePhotoService UsersUserProfilePhotoProvider, logger log.Logger) (UsersUserProfilePhotoApi, error) {
	return UsersUserProfilePhotoApi{
		logger:                       log.Logger{Logger: logger.With().Str("graph api", "UsersUserProfilePhotoApi").Logger()},
		usersUserProfilePhotoService: usersUserProfilePhotoService,
	}, nil
}

// GetProfilePhoto provides the requested photo
func (api UsersUserProfilePhotoApi) GetProfilePhoto(w http.ResponseWriter, r *http.Request) {
	id, ok := api.getUserID(w, r)
	if !ok {
		return
	}

	photo, err := api.usersUserProfilePhotoService.GetPhoto(r.Context(), id)
	if err != nil {
		api.logger.Debug().Err(err)
		errorcode.GeneralException.Render(w, r, http.StatusNotFound, "failed to get photo")
		return
	}

	render.Status(r, http.StatusOK)
	_, _ = w.Write(photo)
}

// UpsertProfilePhoto updates or inserts (initial create) the requested photo
func (api UsersUserProfilePhotoApi) UpsertProfilePhoto(w http.ResponseWriter, r *http.Request) {
	id, ok := api.getUserID(w, r)
	if !ok {
		return
	}

	if err := api.usersUserProfilePhotoService.UpdatePhoto(r.Context(), id, r.Body); err != nil {
		api.logger.Debug().Err(err)
		errorcode.GeneralException.Render(w, r, http.StatusNotFound, "failed to update photo")
		return
	}
	defer func() {
		_ = r.Body.Close()
	}()

	render.Status(r, http.StatusOK)
}

// DeleteProfilePhoto deletes the requested photo
func (api UsersUserProfilePhotoApi) DeleteProfilePhoto(w http.ResponseWriter, r *http.Request) {
	id, ok := api.getUserID(w, r)
	if !ok {
		return
	}

	if err := api.usersUserProfilePhotoService.DeletePhoto(r.Context(), id); err != nil {
		api.logger.Debug().Err(err)
		errorcode.GeneralException.Render(w, r, http.StatusNotFound, "failed to delete photo")
		return
	}

	render.Status(r, http.StatusOK)
}

func (api UsersUserProfilePhotoApi) getUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	u, ok := revactx.ContextGetUser(r.Context())
	if !ok {
		api.logger.Debug().Msg(ErrNoUser.Error())
		errorcode.GeneralException.Render(w, r, http.StatusMethodNotAllowed, ErrNoUser.Error())
		return "", false
	}

	return u.GetId().GetOpaqueId(), true
}
