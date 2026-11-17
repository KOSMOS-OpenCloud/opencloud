package keystore

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"io"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/opencloud-eu/opencloud/pkg/kosmoskeystore"
)

const (
	testUser     = "user-alice"
	testPassword = "correct horse"
)

func quietLogger() logrus.FieldLogger {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	return logger
}

func newServiceKey(t *testing.T) (privateKey []byte, encodedPublicKey string) {
	t.Helper()
	ecdhPrivateKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return ecdhPrivateKey.Bytes(), base64.RawURLEncoding.EncodeToString(ecdhPrivateKey.PublicKey().Bytes())
}

func TestParseServiceKeys(t *testing.T) {
	_, encodedPublicKey := newServiceKey(t)
	serviceDirectory, err := ParseServiceKeys(" opendock=" + encodedPublicKey + " , ")
	if err != nil || len(serviceDirectory) != 1 {
		t.Fatalf("valid list: %v %d", err, len(serviceDirectory))
	}
	serviceKey, err := serviceDirectory.LookupKey(context.Background(), serviceDirectory.KeyIDs()[0])
	if err != nil || serviceKey.Owner != "opendock" || serviceKey.Kind != kosmoskeystore.KindService {
		t.Fatalf("lookup: %v %+v", err, serviceKey)
	}
	for _, invalidList := range []string{"opendock", "=" + encodedPublicKey, "opendock=!!", "opendock=c2hvcnQ"} {
		if _, err := ParseServiceKeys(invalidList); err == nil {
			t.Fatalf("%q must be rejected", invalidList)
		}
	}
	if empty, err := ParseServiceKeys(""); err != nil || len(empty) != 0 {
		t.Fatalf("empty list: %v", err)
	}
}

func TestOpenDisabledAndInvalid(t *testing.T) {
	if disabled, err := Open("", "", ""); err != nil || disabled != nil {
		t.Fatalf("empty path must disable: %v", err)
	}
	if _, err := Open(filepath.Join(t.TempDir(), "k.boltdb"), "", "soon"); err == nil {
		t.Fatal("invalid ttl must be rejected")
	}
}

func TestHookLifecycle(t *testing.T) {
	servicePrivateKey, encodedPublicKey := newServiceKey(t)
	keystore, err := Open(filepath.Join(t.TempDir(), "keystore.boltdb"), "opendock="+encodedPublicKey, "1h")
	if err != nil {
		t.Fatal(err)
	}
	serviceDirectory, _ := ParseServiceKeys("opendock=" + encodedPublicKey)
	serviceKeyID := serviceDirectory.KeyIDs()[0]
	hook := NewHook(keystore, quietLogger())
	ctx := context.Background()

	// first login creates and opens
	sessionRef := hook.LoggedOn(ctx, testUser, testPassword)
	if sessionRef == nil || *sessionRef != sessionRefPrefix+testUser {
		t.Fatalf("session ref: %v", sessionRef)
	}
	sealedSecret, err := keystore.SessionSecretFor(ctx, testUser, serviceKeyID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := kosmoskeystore.OpenSessionSecret(servicePrivateKey, sealedSecret, testUser); err != nil {
		t.Fatalf("service cannot open its session secret: %v", err)
	}
	createdKey, err := keystore.PublicKey(ctx, testUser)
	if err != nil {
		t.Fatal(err)
	}

	// a password that does not fit keeps the key store as it is, without session ref
	if staleRef := hook.LoggedOn(ctx, testUser, "changed elsewhere"); staleRef != nil {
		t.Fatalf("stale password must not open: %v", *staleRef)
	}
	unchangedKey, err := keystore.PublicKey(ctx, testUser)
	if err != nil || unchangedKey.KeyID != createdKey.KeyID {
		t.Fatalf("key store was replaced: %v", err)
	}

	// logout closes; foreign references are not ours
	if !hook.SessionDestroyed(ctx, sessionRef) {
		t.Fatal("own session ref not recognised")
	}
	if _, err := keystore.SessionSecretFor(ctx, testUser, serviceKeyID); err != kosmoskeystore.ErrLocked {
		t.Fatalf("after logout: %v", err)
	}
	foreignRef := "other:-:" + testUser
	if hook.SessionDestroyed(ctx, &foreignRef) || hook.SessionDestroyed(ctx, nil) {
		t.Fatal("foreign session ref handled")
	}
}

func TestNilHook(t *testing.T) {
	var disabledHook *Hook
	if disabledHook.LoggedOn(context.Background(), testUser, testPassword) != nil {
		t.Fatal("disabled hook returned a session ref")
	}
	sessionRef := sessionRefPrefix + testUser
	if disabledHook.SessionDestroyed(context.Background(), &sessionRef) {
		t.Fatal("disabled hook handled a session")
	}
}
