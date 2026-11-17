package kosmoskeystore

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// cheap Argon2id parameters keep the tests fast; production uses the default.
var testArgonParameters = ArgonParameters{Time: 1, MemoryKiB: 64, Parallelism: 1}

type testDirectory map[string]DirectoryKey

func (d testDirectory) LookupKey(_ context.Context, keyID string) (DirectoryKey, error) {
	directoryKey, found := d[keyID]
	if !found {
		return DirectoryKey{}, ErrUnknownKey
	}
	return directoryKey, nil
}

type testKeyPair struct {
	privateKey []byte
	published  DirectoryKey
}

func newTestKeyPair(t *testing.T, kind KeyKind, owner string) testKeyPair {
	t.Helper()
	ecdhPrivateKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicKey := ecdhPrivateKey.PublicKey().Bytes()
	keyID, err := KeyID(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	return testKeyPair{
		privateKey: ecdhPrivateKey.Bytes(),
		published:  DirectoryKey{KeyID: keyID, PublicKey: publicKey, Kind: kind, Owner: owner},
	}
}

type testFixture struct {
	keystore     *Keystore
	store        Store
	directory    testDirectory
	dockService  testKeyPair
	otherService testKeyPair
	otherUser    testKeyPair
	clock        *time.Time
}

const (
	testUser     = "user-alice"
	testPassword = "correct horse"
	testEntry    = "entry-imap-1"
)

func newTestFixture(t *testing.T, store Store) *testFixture {
	t.Helper()
	fixture := &testFixture{
		store:        store,
		directory:    testDirectory{},
		dockService:  newTestKeyPair(t, KindService, "opendock"),
		otherService: newTestKeyPair(t, KindService, "unconfigured"),
		otherUser:    newTestKeyPair(t, KindUser, "user-bob"),
	}
	for _, keyPair := range []testKeyPair{fixture.dockService, fixture.otherService, fixture.otherUser} {
		fixture.directory[keyPair.published.KeyID] = keyPair.published
	}
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	fixture.clock = &now
	keystore, err := New(Options{
		Store:         store,
		Directory:     fixture.directory,
		ServiceKeyIDs: []string{fixture.dockService.published.KeyID},
		SessionTTL:    time.Hour,
		Argon:         testArgonParameters,
		Now:           func() time.Time { return *fixture.clock },
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.keystore = keystore
	return fixture
}

// createAndUnlock returns the user's published key and the session secret as
// the dock service sees it.
func (f *testFixture) createAndUnlock(t *testing.T) (DirectoryKey, []byte) {
	t.Helper()
	ctx := context.Background()
	userKey, err := f.keystore.Create(ctx, testUser, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.keystore.Unlock(ctx, testUser, testPassword); err != nil {
		t.Fatal(err)
	}
	return userKey, f.sessionSecret(t)
}

func (f *testFixture) sessionSecret(t *testing.T) []byte {
	t.Helper()
	sealedSecret, err := f.keystore.SessionSecretFor(context.Background(), testUser, f.dockService.published.KeyID)
	if err != nil {
		t.Fatal(err)
	}
	sessionSecret, err := OpenSessionSecret(f.dockService.privateKey, sealedSecret, testUser)
	if err != nil {
		t.Fatal(err)
	}
	return sessionSecret
}

func sealTestContentKey(t *testing.T, recipient DirectoryKey) ([]byte, Envelope) {
	t.Helper()
	contentKey := make([]byte, 32)
	if _, err := rand.Read(contentKey); err != nil {
		t.Fatal(err)
	}
	envelope, err := SealContentKey(recipient, testEntry, contentKey)
	if err != nil {
		t.Fatal(err)
	}
	return contentKey, envelope
}

func TestKeyIDIsJWKThumbprint(t *testing.T) {
	// RFC 8037 appendix A.3: thumbprint of the Ed25519 example key. Same
	// construction (OKP, crv, x), so it pins the canonical form.
	publicKey, err := base64.RawURLEncoding.DecodeString("11qYAYKxCrfVS_7TyWQHOg7hcvPapiMlrwIaaPcHURo")
	if err != nil {
		t.Fatal(err)
	}
	if thumbprint := jwkThumbprint("Ed25519", publicKey); thumbprint != "kPrK_qmxVWaYVA9wwBF6Iuo3vVzz7TxHCTwXBygrS4k" {
		t.Fatalf("unexpected thumbprint %s", thumbprint)
	}
	if _, err := KeyID([]byte("short")); err == nil {
		t.Fatal("short public key must be rejected")
	}
}

func TestRewrapToService(t *testing.T) {
	fixture := newTestFixture(t, NewMemoryStore())
	userKey, sessionSecret := fixture.createAndUnlock(t)
	contentKey, envelope := sealTestContentKey(t, userKey)

	rewrapped, err := fixture.keystore.Rewrap(context.Background(), RewrapRequest{
		UserID: testUser, SessionSecret: sessionSecret, Envelope: envelope,
		EntryContext: testEntry, TargetKeyID: fixture.dockService.published.KeyID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rewrapped.KeyID != fixture.dockService.published.KeyID {
		t.Fatalf("rewrapped for %s", rewrapped.KeyID)
	}
	opened, err := OpenContentKey(fixture.dockService.privateKey, rewrapped, testEntry)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(opened, contentKey) {
		t.Fatal("content key changed on the way")
	}
	if _, err := OpenContentKey(fixture.dockService.privateKey, rewrapped, "entry-other"); !errors.Is(err, ErrEnvelope) {
		t.Fatalf("other entry context must fail, got %v", err)
	}
	if _, err := OpenContentKey(fixture.otherService.privateKey, rewrapped, testEntry); !errors.Is(err, ErrEnvelope) {
		t.Fatalf("other recipient must fail, got %v", err)
	}
}

func TestRewrapRefusals(t *testing.T) {
	fixture := newTestFixture(t, NewMemoryStore())
	userKey, sessionSecret := fixture.createAndUnlock(t)
	_, envelope := sealTestContentKey(t, userKey)
	ctx := context.Background()
	validRequest := RewrapRequest{
		UserID: testUser, SessionSecret: sessionSecret, Envelope: envelope,
		EntryContext: testEntry, TargetKeyID: fixture.dockService.published.KeyID,
	}

	wrongSecret := validRequest
	wrongSecret.SessionSecret = bytes.Repeat([]byte{7}, 32)
	if _, err := fixture.keystore.Rewrap(ctx, wrongSecret); !errors.Is(err, ErrSessionSecret) {
		t.Fatalf("wrong session secret: %v", err)
	}

	userTarget := validRequest
	userTarget.TargetKeyID = fixture.otherUser.published.KeyID
	if _, err := fixture.keystore.Rewrap(ctx, userTarget); !errors.Is(err, ErrTargetNotAllowed) {
		t.Fatalf("user key as session target: %v", err)
	}

	unknownTarget := validRequest
	unknownTarget.TargetKeyID = "not-in-directory"
	if _, err := fixture.keystore.Rewrap(ctx, unknownTarget); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("unknown target: %v", err)
	}

	wrongContext := validRequest
	wrongContext.EntryContext = "entry-other"
	if _, err := fixture.keystore.Rewrap(ctx, wrongContext); !errors.Is(err, ErrEnvelope) {
		t.Fatalf("wrong entry context: %v", err)
	}

	tampered := validRequest
	tampered.Envelope.Sealed = append([]byte{}, envelope.Sealed...)
	tampered.Envelope.Sealed[len(tampered.Envelope.Sealed)-1] ^= 1
	if _, err := fixture.keystore.Rewrap(ctx, tampered); !errors.Is(err, ErrEnvelope) {
		t.Fatalf("tampered envelope: %v", err)
	}

	if _, err := fixture.keystore.SessionSecretFor(ctx, testUser, fixture.otherService.published.KeyID); !errors.Is(err, ErrTargetNotAllowed) {
		t.Fatalf("session secret for unconfigured service: %v", err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	fixture := newTestFixture(t, NewMemoryStore())
	userKey, firstSecret := fixture.createAndUnlock(t)
	_, envelope := sealTestContentKey(t, userKey)
	ctx := context.Background()
	request := RewrapRequest{
		UserID: testUser, SessionSecret: firstSecret, Envelope: envelope,
		EntryContext: testEntry, TargetKeyID: fixture.dockService.published.KeyID,
	}

	// second login: new session secret, the first one is stale
	if err := fixture.keystore.Unlock(ctx, testUser, testPassword); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.keystore.Rewrap(ctx, request); !errors.Is(err, ErrSessionSecret) {
		t.Fatalf("stale secret after second login: %v", err)
	}
	request.SessionSecret = fixture.sessionSecret(t)
	if _, err := fixture.keystore.Rewrap(ctx, request); err != nil {
		t.Fatal(err)
	}

	// first logout keeps the session, second closes it
	if err := fixture.keystore.Lock(ctx, testUser); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.keystore.Rewrap(ctx, request); err != nil {
		t.Fatalf("one of two sessions still open: %v", err)
	}
	if err := fixture.keystore.Lock(ctx, testUser); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.keystore.Rewrap(ctx, request); !errors.Is(err, ErrLocked) {
		t.Fatalf("after last logout: %v", err)
	}
	if _, err := fixture.keystore.SessionSecretFor(ctx, testUser, fixture.dockService.published.KeyID); !errors.Is(err, ErrLocked) {
		t.Fatalf("session secret after logout: %v", err)
	}

	// expiry closes a session that never logged out
	if err := fixture.keystore.Unlock(ctx, testUser, testPassword); err != nil {
		t.Fatal(err)
	}
	request.SessionSecret = fixture.sessionSecret(t)
	expired := fixture.clock.Add(2 * time.Hour)
	*fixture.clock = expired
	if _, err := fixture.keystore.Rewrap(ctx, request); !errors.Is(err, ErrLocked) {
		t.Fatalf("expired session: %v", err)
	}
}

func TestPasswordPaths(t *testing.T) {
	fixture := newTestFixture(t, NewMemoryStore())
	userKey, sessionSecret := fixture.createAndUnlock(t)
	contentKey, envelope := sealTestContentKey(t, userKey)
	ctx := context.Background()

	if _, err := fixture.keystore.Create(ctx, testUser, testPassword); !errors.Is(err, ErrExists) {
		t.Fatalf("second create: %v", err)
	}
	if err := fixture.keystore.Unlock(ctx, testUser, "wrong"); !errors.Is(err, ErrUnlock) {
		t.Fatalf("wrong password: %v", err)
	}
	if err := fixture.keystore.ChangePassword(ctx, testUser, "wrong", "new password"); !errors.Is(err, ErrUnlock) {
		t.Fatalf("change with wrong current password: %v", err)
	}
	if err := fixture.keystore.ChangePassword(ctx, testUser, testPassword, "new password"); err != nil {
		t.Fatal(err)
	}
	// the open session survives the change
	if _, err := fixture.keystore.Rewrap(ctx, RewrapRequest{
		UserID: testUser, SessionSecret: sessionSecret, Envelope: envelope,
		EntryContext: testEntry, TargetKeyID: fixture.dockService.published.KeyID,
	}); err != nil {
		t.Fatalf("session after password change: %v", err)
	}
	if err := fixture.keystore.Unlock(ctx, testUser, testPassword); !errors.Is(err, ErrUnlock) {
		t.Fatalf("old password after change: %v", err)
	}
	if err := fixture.keystore.Unlock(ctx, testUser, "new password"); err != nil {
		t.Fatal(err)
	}

	// re-encryption for another user needs the password, not the session
	forOtherUser, err := fixture.keystore.RewrapWithPassword(ctx, testUser, "new password", envelope, testEntry, fixture.otherUser.published.KeyID)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := OpenContentKey(fixture.otherUser.privateKey, forOtherUser, testEntry)
	if err != nil || !bytes.Equal(opened, contentKey) {
		t.Fatalf("other user cannot open: %v", err)
	}
	if _, err := fixture.keystore.RewrapWithPassword(ctx, testUser, "wrong", envelope, testEntry, fixture.otherUser.published.KeyID); !errors.Is(err, ErrUnlock) {
		t.Fatalf("rewrap with wrong password: %v", err)
	}

	// reset: record gone, old key id reported, new pair differs
	retiredKeyID, err := fixture.keystore.Reset(ctx, testUser)
	if err != nil || retiredKeyID != userKey.KeyID {
		t.Fatalf("reset: %v %s", err, retiredKeyID)
	}
	if err := fixture.keystore.Unlock(ctx, testUser, "new password"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unlock after reset: %v", err)
	}
	newUserKey, err := fixture.keystore.Create(ctx, testUser, "after reset")
	if err != nil || newUserKey.KeyID == userKey.KeyID {
		t.Fatalf("create after reset: %v", err)
	}
}

func TestRecordHoldsNoPlaintext(t *testing.T) {
	store := NewMemoryStore()
	fixture := newTestFixture(t, store)
	_, sessionSecret := fixture.createAndUnlock(t)

	// recover the private key the way only a party with the session secret can
	userRecord, err := store.View(context.Background(), testUser)
	if err != nil {
		t.Fatal(err)
	}
	privateKey, err := decryptAESGCM(sessionSecret, userRecord.Session.Nonce, userRecord.Session.Ciphertext,
		associatedData(aadSessionWrap, testUser, userRecord.KeyID))
	if err != nil {
		t.Fatal(err)
	}
	encodedRecord, err := json.Marshal(userRecord)
	if err != nil {
		t.Fatal(err)
	}
	for name, secret := range map[string][]byte{"private key": privateKey, "session secret": sessionSecret} {
		if bytes.Contains(encodedRecord, secret) ||
			bytes.Contains(encodedRecord, []byte(base64.StdEncoding.EncodeToString(secret))) {
			t.Fatalf("%s found in the stored record", name)
		}
	}
}

func TestBoltStoreSurvivesReopen(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "keystore.boltdb")
	boltStore, err := OpenBoltStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	fixture := newTestFixture(t, boltStore)
	userKey, sessionSecret := fixture.createAndUnlock(t)
	contentKey, envelope := sealTestContentKey(t, userKey)
	if err := boltStore.Close(); err != nil {
		t.Fatal(err)
	}

	// restart: a new process opens the file; the session is still usable
	reopenedStore, err := OpenBoltStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopenedStore.Close()
	restartedKeystore, err := New(Options{
		Store: reopenedStore, Directory: fixture.directory,
		ServiceKeyIDs: []string{fixture.dockService.published.KeyID},
		SessionTTL:    time.Hour, Argon: testArgonParameters,
		Now: func() time.Time { return *fixture.clock },
	})
	if err != nil {
		t.Fatal(err)
	}
	rewrapped, err := restartedKeystore.Rewrap(context.Background(), RewrapRequest{
		UserID: testUser, SessionSecret: sessionSecret, Envelope: envelope,
		EntryContext: testEntry, TargetKeyID: fixture.dockService.published.KeyID,
	})
	if err != nil {
		t.Fatal(err)
	}
	opened, err := OpenContentKey(fixture.dockService.privateKey, rewrapped, testEntry)
	if err != nil || !bytes.Equal(opened, contentKey) {
		t.Fatalf("after restart: %v", err)
	}
	if _, err := reopenedStore.View(context.Background(), "nobody"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown user: %v", err)
	}
}
