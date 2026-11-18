package keystore

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gorilla/mux"

	userkeystore "github.com/opencloud-eu/opencloud/pkg/keystore"
)

const testBearer = "Bearer token-of-alice"

// bearerOfAlice accepts exactly one token and maps it to the test user.
func bearerOfAlice(request *http.Request) (string, error) {
	if request.Header.Get("Authorization") != testBearer {
		return "", errors.New("invalid token")
	}
	return testUser, nil
}

func callRoute(t *testing.T, router http.Handler, method, target, authorization string, requestBody any) (int, []byte) {
	t.Helper()
	var encodedBody bytes.Buffer
	if requestBody != nil {
		if err := json.NewEncoder(&encodedBody).Encode(requestBody); err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(method, target, &encodedBody)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder.Code, recorder.Body.Bytes()
}

func errorCode(t *testing.T, responseBody []byte) string {
	t.Helper()
	var failure errorResponse
	if err := json.Unmarshal(responseBody, &failure); err != nil {
		t.Fatalf("no error body: %s", responseBody)
	}
	return failure.Error
}

func TestRoutesSessionFlow(t *testing.T) {
	servicePrivateKey, encodedPublicKey := newServiceKey(t)
	keystore, err := Open(filepath.Join(t.TempDir(), "keystore.boltdb"), "opendock="+encodedPublicKey, "1h")
	if err != nil {
		t.Fatal(err)
	}
	serviceDirectory, _ := ParseServiceKeys("opendock=" + encodedPublicKey)
	serviceKeyID := serviceDirectory.KeyIDs()[0]
	hook := NewHook(keystore, quietLogger())
	ctx := context.Background()

	router := mux.NewRouter()
	NewRoutes(keystore, bearerOfAlice, quietLogger()).AddRoutes(router)

	// without a valid bearer nothing is served
	for _, target := range []string{"/key", "/session/" + serviceKeyID} {
		if status, _ := callRoute(t, router, http.MethodGet, RoutesPathPrefix+target, "", nil); status != http.StatusUnauthorized {
			t.Fatalf("%s without bearer: %d", target, status)
		}
	}
	if status, _ := callRoute(t, router, http.MethodPost, RoutesPathPrefix+"/rewrap", "Bearer stolen", rewrapRequestBody{}); status != http.StatusUnauthorized {
		t.Fatalf("rewrap with foreign bearer: %d", status)
	}

	// before the first login there is no key store
	status, responseBody := callRoute(t, router, http.MethodGet, RoutesPathPrefix+"/key", testBearer, nil)
	if status != http.StatusNotFound || errorCode(t, responseBody) != "no_keystore" {
		t.Fatalf("key before login: %d %s", status, responseBody)
	}

	sessionRef := hook.LoggedOn(ctx, testUser, testPassword)
	if sessionRef == nil {
		t.Fatal("login did not open the key store")
	}

	// public key of the user
	status, responseBody = callRoute(t, router, http.MethodGet, RoutesPathPrefix+"/key", testBearer, nil)
	var userKey publicKeyResponse
	if status != http.StatusOK || json.Unmarshal(responseBody, &userKey) != nil || len(userKey.PublicKey) != 32 {
		t.Fatalf("key: %d %s", status, responseBody)
	}

	// the service fetches its sealed session secret and opens it
	status, responseBody = callRoute(t, router, http.MethodGet, RoutesPathPrefix+"/session/"+serviceKeyID, testBearer, nil)
	var sealedSecret userkeystore.Envelope
	if status != http.StatusOK || json.Unmarshal(responseBody, &sealedSecret) != nil {
		t.Fatalf("session: %d %s", status, responseBody)
	}
	sessionSecret, err := userkeystore.OpenSessionSecret(servicePrivateKey, sealedSecret, testUser)
	if err != nil {
		t.Fatal(err)
	}
	status, responseBody = callRoute(t, router, http.MethodGet, RoutesPathPrefix+"/session/unknown-service", testBearer, nil)
	if status != http.StatusForbidden || errorCode(t, responseBody) != "target_not_allowed" {
		t.Fatalf("session for unknown service: %d %s", status, responseBody)
	}

	// an entry's content key, sealed for the user, is re-encrypted to the service
	const entryContext = "entry-42"
	contentKey := make([]byte, 32)
	if _, err := rand.Read(contentKey); err != nil {
		t.Fatal(err)
	}
	userEnvelope, err := userkeystore.SealContentKey(userkeystore.DirectoryKey{
		KeyID: userKey.KeyID, PublicKey: userKey.PublicKey, Kind: userkeystore.KindUser, Owner: testUser,
	}, entryContext, contentKey)
	if err != nil {
		t.Fatal(err)
	}
	validRewrap := rewrapRequestBody{
		SessionSecret: sessionSecret, Envelope: userEnvelope, EntryContext: entryContext, TargetKeyID: serviceKeyID,
	}
	status, responseBody = callRoute(t, router, http.MethodPost, RoutesPathPrefix+"/rewrap", testBearer, validRewrap)
	var serviceEnvelope userkeystore.Envelope
	if status != http.StatusOK || json.Unmarshal(responseBody, &serviceEnvelope) != nil {
		t.Fatalf("rewrap: %d %s", status, responseBody)
	}
	if bytes.Contains(responseBody, contentKey) {
		t.Fatal("response carries the content key in the clear")
	}
	reopenedContentKey, err := userkeystore.OpenContentKey(servicePrivateKey, serviceEnvelope, entryContext)
	if err != nil || !bytes.Equal(reopenedContentKey, contentKey) {
		t.Fatalf("service cannot open the re-encrypted content key: %v", err)
	}

	// refusals
	wrongSecret := validRewrap
	wrongSecret.SessionSecret = make([]byte, 32)
	status, responseBody = callRoute(t, router, http.MethodPost, RoutesPathPrefix+"/rewrap", testBearer, wrongSecret)
	if status != http.StatusForbidden || errorCode(t, responseBody) != "session_secret_mismatch" {
		t.Fatalf("wrong session secret: %d %s", status, responseBody)
	}
	attackerKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	attackerKeyID, _ := userkeystore.KeyID(attackerKey.PublicKey().Bytes())
	foreignTarget := validRewrap
	foreignTarget.TargetKeyID = attackerKeyID
	status, responseBody = callRoute(t, router, http.MethodPost, RoutesPathPrefix+"/rewrap", testBearer, foreignTarget)
	if status != http.StatusForbidden || errorCode(t, responseBody) != "target_not_allowed" {
		t.Fatalf("target outside the directory: %d %s", status, responseBody)
	}
	otherContext := validRewrap
	otherContext.EntryContext = "entry-43"
	status, responseBody = callRoute(t, router, http.MethodPost, RoutesPathPrefix+"/rewrap", testBearer, otherContext)
	if status != http.StatusUnprocessableEntity || errorCode(t, responseBody) != "envelope_not_openable" {
		t.Fatalf("other entry context: %d %s", status, responseBody)
	}
	request := httptest.NewRequest(http.MethodPost, RoutesPathPrefix+"/rewrap", bytes.NewBufferString("{not json"))
	request.Header.Set("Authorization", testBearer)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("broken body: %d", recorder.Code)
	}

	// after logout the session is gone, the public key stays
	hook.SessionDestroyed(ctx, sessionRef)
	status, responseBody = callRoute(t, router, http.MethodGet, RoutesPathPrefix+"/session/"+serviceKeyID, testBearer, nil)
	if status != http.StatusConflict || errorCode(t, responseBody) != "keystore_locked" {
		t.Fatalf("session after logout: %d %s", status, responseBody)
	}
	status, responseBody = callRoute(t, router, http.MethodPost, RoutesPathPrefix+"/rewrap", testBearer, validRewrap)
	if status != http.StatusConflict || errorCode(t, responseBody) != "keystore_locked" {
		t.Fatalf("rewrap after logout: %d %s", status, responseBody)
	}
	if status, _ = callRoute(t, router, http.MethodGet, RoutesPathPrefix+"/key", testBearer, nil); status != http.StatusOK {
		t.Fatalf("key after logout: %d", status)
	}
}
