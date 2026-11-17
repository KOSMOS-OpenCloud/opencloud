package keystore

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"

	"github.com/opencloud-eu/opencloud/pkg/kosmoskeystore"
)

// RoutesPathPrefix is where the key store routes are mounted. It lies below
// /signin/, which the proxy already forwards to the idp without own checks;
// every route here validates the bearer token itself.
const RoutesPathPrefix = "/signin/v1/keystore"

// maxRequestBodyBytes bounds a rewrap request (one envelope plus ids).
const maxRequestBodyBytes = 16 * 1024

// SessionOperations is the part of kosmoskeystore.Keystore the routes need.
type SessionOperations interface {
	PublicKey(ctx context.Context, userID string) (kosmoskeystore.DirectoryKey, error)
	SessionSecretFor(ctx context.Context, userID, serviceKeyID string) (kosmoskeystore.Envelope, error)
	Rewrap(ctx context.Context, request kosmoskeystore.RewrapRequest) (kosmoskeystore.Envelope, error)
}

// Authenticator validates the bearer token of a request and returns the user
// id it was issued for (the same id Logon uses for the key store).
type Authenticator func(request *http.Request) (userID string, err error)

// Routes serves the key store over HTTP for services that act with the
// user's bearer token (opendock/OPENCORE_KEYSTORE.md §10.3). No route returns
// a private key, a session secret in the clear or a content key in the clear.
type Routes struct {
	operations   SessionOperations
	authenticate Authenticator
	logger       logrus.FieldLogger
}

// NewRoutes builds the routes.
func NewRoutes(operations SessionOperations, authenticate Authenticator, logger logrus.FieldLogger) *Routes {
	return &Routes{operations: operations, authenticate: authenticate, logger: logger}
}

// AddRoutes registers the routes on the idp router.
func (routes *Routes) AddRoutes(router *mux.Router) {
	router.Handle(RoutesPathPrefix+"/key", routes.authenticated(routes.publicKey)).Methods(http.MethodGet)
	router.Handle(RoutesPathPrefix+"/session/{serviceKeyID}", routes.authenticated(routes.sessionSecret)).Methods(http.MethodGet)
	router.Handle(RoutesPathPrefix+"/rewrap", routes.authenticated(routes.rewrap)).Methods(http.MethodPost)
}

type publicKeyResponse struct {
	KeyID     string `json:"kid"`
	PublicKey []byte `json:"publicKey"`
}

type rewrapRequestBody struct {
	SessionSecret []byte                  `json:"sessionSecret"`
	Envelope      kosmoskeystore.Envelope `json:"envelope"`
	EntryContext  string                  `json:"entryContext"`
	TargetKeyID   string                  `json:"targetKid"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func (routes *Routes) authenticated(handle func(responseWriter http.ResponseWriter, request *http.Request, userID string)) http.Handler {
	return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set("Cache-Control", "no-store")
		userID, authenticateErr := routes.authenticate(request)
		if authenticateErr != nil || userID == "" {
			responseWriter.Header().Set("WWW-Authenticate", "Bearer")
			writeJSON(responseWriter, http.StatusUnauthorized, errorResponse{Error: "invalid_token"})
			return
		}
		handle(responseWriter, request, userID)
	})
}

// publicKey returns the caller's own public key store key.
func (routes *Routes) publicKey(responseWriter http.ResponseWriter, request *http.Request, userID string) {
	userKey, err := routes.operations.PublicKey(request.Context(), userID)
	if err != nil {
		routes.writeError(responseWriter, userID, err)
		return
	}
	writeJSON(responseWriter, http.StatusOK, publicKeyResponse{KeyID: userKey.KeyID, PublicKey: userKey.PublicKey})
}

// sessionSecret returns the session secret sealed for one service key. Only
// the holder of that service's private key can open it.
func (routes *Routes) sessionSecret(responseWriter http.ResponseWriter, request *http.Request, userID string) {
	sealedSecret, err := routes.operations.SessionSecretFor(request.Context(), userID, mux.Vars(request)["serviceKeyID"])
	if err != nil {
		routes.writeError(responseWriter, userID, err)
		return
	}
	writeJSON(responseWriter, http.StatusOK, sealedSecret)
}

// rewrap re-encrypts a content key from the user's key to a service key.
func (routes *Routes) rewrap(responseWriter http.ResponseWriter, request *http.Request, userID string) {
	var requestBody rewrapRequestBody
	bodyDecoder := json.NewDecoder(http.MaxBytesReader(responseWriter, request.Body, maxRequestBodyBytes))
	if err := bodyDecoder.Decode(&requestBody); err != nil {
		writeJSON(responseWriter, http.StatusBadRequest, errorResponse{Error: "invalid_request"})
		return
	}
	defer clear(requestBody.SessionSecret)
	rewrappedEnvelope, err := routes.operations.Rewrap(request.Context(), kosmoskeystore.RewrapRequest{
		UserID:        userID,
		SessionSecret: requestBody.SessionSecret,
		Envelope:      requestBody.Envelope,
		EntryContext:  requestBody.EntryContext,
		TargetKeyID:   requestBody.TargetKeyID,
	})
	if err != nil {
		routes.writeError(responseWriter, userID, err)
		return
	}
	writeJSON(responseWriter, http.StatusOK, rewrappedEnvelope)
}

// writeError maps key store errors to a status and a stable error code.
func (routes *Routes) writeError(responseWriter http.ResponseWriter, userID string, err error) {
	switch {
	case errors.Is(err, kosmoskeystore.ErrNotFound):
		writeJSON(responseWriter, http.StatusNotFound, errorResponse{Error: "no_keystore"})
	case errors.Is(err, kosmoskeystore.ErrLocked):
		writeJSON(responseWriter, http.StatusConflict, errorResponse{Error: "keystore_locked"})
	case errors.Is(err, kosmoskeystore.ErrSessionSecret):
		writeJSON(responseWriter, http.StatusForbidden, errorResponse{Error: "session_secret_mismatch"})
	case errors.Is(err, kosmoskeystore.ErrTargetNotAllowed), errors.Is(err, kosmoskeystore.ErrUnknownKey):
		writeJSON(responseWriter, http.StatusForbidden, errorResponse{Error: "target_not_allowed"})
	case errors.Is(err, kosmoskeystore.ErrEnvelope):
		writeJSON(responseWriter, http.StatusUnprocessableEntity, errorResponse{Error: "envelope_not_openable"})
	default:
		routes.logger.WithError(err).WithField("id", userID).Warnln("keystore: request failed")
		writeJSON(responseWriter, http.StatusInternalServerError, errorResponse{Error: "server_error"})
	}
}

func writeJSON(responseWriter http.ResponseWriter, status int, body any) {
	responseWriter.Header().Set("Content-Type", "application/json")
	responseWriter.WriteHeader(status)
	_ = json.NewEncoder(responseWriter).Encode(body)
}
