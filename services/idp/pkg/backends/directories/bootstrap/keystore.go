package bootstrap

import (
	"context"
	"errors"
	"net/http"

	"github.com/gorilla/mux"
	konnect "github.com/libregraph/lico"
	"github.com/libregraph/lico/identity/managers"
	licomanagers "github.com/libregraph/lico/managers"
	"github.com/libregraph/lico/oidc/provider"
	"github.com/sirupsen/logrus"

	userkeystore "github.com/opencloud-eu/opencloud/pkg/keystore"
	"github.com/opencloud-eu/opencloud/services/idp/pkg/backends/directories/keystore"
)

// keystoreIdentityManager is the identifier identity manager plus the key
// store routes. It is only used when the key store is enabled.
type keystoreIdentityManager struct {
	*managers.IdentifierIdentityManager

	userKeystore *userkeystore.Keystore
	oidcProvider *provider.Provider
	logger       logrus.FieldLogger
}

// RegisterManagers keeps the OIDC provider, which validates access tokens.
func (im *keystoreIdentityManager) RegisterManagers(mgrs *licomanagers.Managers) error {
	im.oidcProvider = mgrs.Must("oidc").(*provider.Provider)

	return im.IdentifierIdentityManager.RegisterManagers(mgrs)
}

// AddRoutes adds the key store routes to the routes of the identifier.
func (im *keystoreIdentityManager) AddRoutes(ctx context.Context, router *mux.Router) {
	im.IdentifierIdentityManager.AddRoutes(ctx, router)
	keystore.NewRoutes(im.userKeystore, im.userIDFromAccessToken, im.logger).AddRoutes(router)
}

// userIDFromAccessToken validates the bearer token (signature, expiry, token
// type) and returns the user id Logon used for the key store. Tokens of other
// identity managers (guests) are refused.
func (im *keystoreIdentityManager) userIDFromAccessToken(request *http.Request) (string, error) {
	if im.oidcProvider == nil {
		return "", errors.New("oidc provider not registered")
	}
	accessTokenClaims, err := im.oidcProvider.GetAccessTokenClaimsFromRequest(request)
	if err != nil {
		return "", err
	}
	if accessTokenClaims.IdentityProvider != im.Name() {
		return "", errors.New("access token of another identity manager")
	}
	userID, _ := accessTokenClaims.IdentityClaims[konnect.IdentifiedUserIDClaim].(string)
	if userID == "" {
		return "", errors.New("access token without user id")
	}
	return userID, nil
}
