package svc_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/opencloud-eu/opencloud/pkg/log"
	"github.com/opencloud-eu/opencloud/services/graph/mocks"
	svc "github.com/opencloud-eu/opencloud/services/graph/pkg/service/v0"
)

func TestNewUsersUserProfilePhotoService(t *testing.T) {
	storage := mocks.NewStorage(t)
	storage.EXPECT().Init(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, id string) error { return nil })

	service, err := svc.NewUsersUserProfilePhotoService(storage)
	assert.NoError(t, err)

	t.Run("UpdatePhoto", func(t *testing.T) {
		t.Run("reports an error if id is empty", func(t *testing.T) {
			err := service.UpdatePhoto(context.Background(), "", bytes.NewReader([]byte{}))
			assert.ErrorIs(t, err, svc.ErrMissingArgument)
		})

		t.Run("reports an error if the reader does not contain any bytes", func(t *testing.T) {
			err := service.UpdatePhoto(context.Background(), "123", bytes.NewReader([]byte{}))
			assert.ErrorIs(t, err, svc.ErrNoBytes)
		})

		t.Run("reports an error if data is not an image", func(t *testing.T) {
			err := service.UpdatePhoto(context.Background(), "234", bytes.NewReader([]byte("not an image")))
			assert.ErrorIs(t, err, svc.ErrInvalidContentType)
		})
	})
}

func TestUsersUserProfilePhotoApi(t *testing.T) {
	var (
		serviceProvider = mocks.NewUsersUserProfilePhotoProvider(t)
		dataProvider    = func(w http.ResponseWriter, r *http.Request) (string, bool) {
			return "123", true
		}
	)

	api, err := svc.NewUsersUserProfilePhotoApi(serviceProvider, log.NopLogger())
	assert.NoError(t, err)

	t.Run("GetProfilePhoto", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		ep := api.GetProfilePhoto(dataProvider)

		t.Run("fails if photo provider errors", func(t *testing.T) {
			w := httptest.NewRecorder()

			serviceProvider.EXPECT().GetPhoto(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, s string) ([]byte, error) {
				return nil, errors.New("any")
			}).Once()

			ep.ServeHTTP(w, r)

			assert.Equal(t, http.StatusNotFound, w.Code)
		})

		t.Run("successfully returns the requested photo", func(t *testing.T) {
			w := httptest.NewRecorder()

			serviceProvider.EXPECT().GetPhoto(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, s string) ([]byte, error) {
				return []byte("photo"), nil
			}).Once()

			ep.ServeHTTP(w, r)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, "photo", w.Body.String())
		})
	})

	t.Run("DeleteProfilePhoto", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodDelete, "/", nil)
		ep := api.DeleteProfilePhoto(dataProvider)

		t.Run("fails if photo provider errors", func(t *testing.T) {
			w := httptest.NewRecorder()

			serviceProvider.EXPECT().DeletePhoto(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, s string) error {
				return errors.New("any")
			}).Once()

			ep.ServeHTTP(w, r)

			assert.Equal(t, http.StatusInternalServerError, w.Code)
		})

		t.Run("successfully deletes the requested photo", func(t *testing.T) {
			w := httptest.NewRecorder()

			serviceProvider.EXPECT().DeletePhoto(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, s string) error {
				return nil
			}).Once()

			ep.ServeHTTP(w, r)

			assert.Equal(t, http.StatusOK, w.Code)
		})
	})

	t.Run("UpsertProfilePhoto", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPut, "/", strings.NewReader("body"))
		ep := api.UpsertProfilePhoto(dataProvider)

		t.Run("fails if photo provider errors", func(t *testing.T) {
			w := httptest.NewRecorder()

			serviceProvider.EXPECT().UpdatePhoto(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, s string, r io.Reader) error {
				return errors.New("any")
			}).Once()

			ep.ServeHTTP(w, r)

			assert.Equal(t, http.StatusInternalServerError, w.Code)
		})

		t.Run("successfully upserts the photo", func(t *testing.T) {
			w := httptest.NewRecorder()

			serviceProvider.EXPECT().UpdatePhoto(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, s string, r io.Reader) error {
				return nil
			}).Once()

			ep.ServeHTTP(w, r)

			assert.Equal(t, http.StatusOK, w.Code)
		})
	})
}
