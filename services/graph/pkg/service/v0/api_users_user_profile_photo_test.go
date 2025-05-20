package svc_test

import (
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

func TestUsersUserProfilePhotoApi(t *testing.T) {
	var (
		usersUserProfilePhotoProvider = mocks.NewUsersUserProfilePhotoProvider(t)
		dummyDataProvider             = func(w http.ResponseWriter, r *http.Request) (string, bool) {
			return "123", true
		}
	)

	api, err := svc.NewUsersUserProfilePhotoApi(usersUserProfilePhotoProvider, log.NopLogger())
	assert.NoError(t, err)

	t.Run("GetProfilePhoto", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		ep := api.GetProfilePhoto(dummyDataProvider)

		t.Run("fails if photo provider errors", func(t *testing.T) {
			w := httptest.NewRecorder()

			usersUserProfilePhotoProvider.EXPECT().GetPhoto(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, s string) ([]byte, error) {
				return nil, errors.New("any")
			}).Once()

			ep.ServeHTTP(w, r)

			assert.Equal(t, http.StatusNotFound, w.Code)
		})

		t.Run("successfully returns the requested photo", func(t *testing.T) {
			w := httptest.NewRecorder()

			usersUserProfilePhotoProvider.EXPECT().GetPhoto(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, s string) ([]byte, error) {
				return []byte("photo"), nil
			}).Once()

			ep.ServeHTTP(w, r)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, "photo", w.Body.String())
		})
	})

	t.Run("DeleteProfilePhoto", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodDelete, "/", nil)
		ep := api.DeleteProfilePhoto(dummyDataProvider)

		t.Run("fails if photo provider errors", func(t *testing.T) {
			w := httptest.NewRecorder()

			usersUserProfilePhotoProvider.EXPECT().DeletePhoto(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, s string) error {
				return errors.New("any")
			}).Once()

			ep.ServeHTTP(w, r)

			assert.Equal(t, http.StatusInternalServerError, w.Code)
		})

		t.Run("successfully deletes the requested photo", func(t *testing.T) {
			w := httptest.NewRecorder()

			usersUserProfilePhotoProvider.EXPECT().DeletePhoto(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, s string) error {
				return nil
			}).Once()

			ep.ServeHTTP(w, r)

			assert.Equal(t, http.StatusOK, w.Code)
		})
	})

	t.Run("UpsertProfilePhoto", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPut, "/", strings.NewReader("body"))
		ep := api.UpsertProfilePhoto(dummyDataProvider)

		t.Run("fails if photo provider errors", func(t *testing.T) {
			w := httptest.NewRecorder()

			usersUserProfilePhotoProvider.EXPECT().UpdatePhoto(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, s string, r io.Reader) error {
				return errors.New("any")
			}).Once()

			ep.ServeHTTP(w, r)

			assert.Equal(t, http.StatusInternalServerError, w.Code)
		})

		t.Run("successfully upserts the photo", func(t *testing.T) {
			w := httptest.NewRecorder()

			usersUserProfilePhotoProvider.EXPECT().UpdatePhoto(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, s string, r io.Reader) error {
				return nil
			}).Once()

			ep.ServeHTTP(w, r)

			assert.Equal(t, http.StatusOK, w.Code)
		})
	})
}
