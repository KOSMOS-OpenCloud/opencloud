// Package keystore holds one X25519 key pair per user on behalf of the
// IdP and re-encrypts content keys for other recipients. The private key
// never leaves the package: callers hand in an envelope and get back an
// envelope for another recipient key.
//
// At rest there is only ciphertext:
//   - the private key encrypted with a key derived from the login password
//     (available only while the password is, i.e. inside Logon), and
//   - between login and logout the private key encrypted with a random
//     session secret, which is stored only sealed for the service keys.
//
// A service (OpenDock) fetches its sealed session secret, opens it with its
// own private key and passes it along with every Rewrap call. Opening
// therefore needs both the keystore record and the service's private key.
//
// The package has no dependency on lico, graph or any other service.
package keystore

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/argon2"
)

var (
	// ErrExists is returned by Create when the user already has a keystore.
	ErrExists = errors.New("keystore: keystore already exists")
	// ErrUnlock is returned when the password does not open the password
	// wrap: wrong password, or the password was changed without the keystore
	// being told (admin reset, change in an external directory).
	ErrUnlock = errors.New("keystore: password does not open the keystore")
	// ErrLocked is returned when there is no open session or it has expired.
	ErrLocked = errors.New("keystore: keystore is locked")
	// ErrSessionSecret is returned when the session secret is wrong or stale.
	ErrSessionSecret = errors.New("keystore: session secret does not match")
	// ErrUnknownKey is returned by a Directory for a key id it does not hold.
	ErrUnknownKey = errors.New("keystore: key id not in directory")
	// ErrTargetNotAllowed is returned when the target key may not be used
	// for this kind of re-encryption.
	ErrTargetNotAllowed = errors.New("keystore: target key not allowed")
	// ErrConflict is returned when the record changed concurrently.
	ErrConflict = errors.New("keystore: record changed concurrently")
)

// KeyKind tells who owns a published key.
type KeyKind string

const (
	// KindService marks keys of services registered by an administrator.
	KindService KeyKind = "service"
	// KindUser marks keys of users (any of a user's key stores).
	KindUser KeyKind = "user"
)

// DirectoryKey is a public key as published in the directory.
type DirectoryKey struct {
	KeyID     string
	PublicKey []byte // X25519, 32 bytes
	Kind      KeyKind
	Owner     string // user id or service name
}

// Directory resolves key ids to published keys. Re-encryption targets are
// always resolved here and never accepted as raw keys from a request;
// otherwise a stolen bearer token could name its own key as target.
type Directory interface {
	LookupKey(ctx context.Context, keyID string) (DirectoryKey, error)
}

// ArgonParameters are the Argon2id cost parameters of new password wraps.
type ArgonParameters struct {
	Time        uint32
	MemoryKiB   uint32
	Parallelism uint8
}

// DefaultArgonParameters is the second recommended option of RFC 9106
// (3 passes, 64 MiB, 4 lanes).
var DefaultArgonParameters = ArgonParameters{Time: 3, MemoryKiB: 64 * 1024, Parallelism: 4}

// Options configure a Keystore.
type Options struct {
	Store     Store
	Directory Directory
	// ServiceKeyIDs are the service keys that receive the sealed session
	// secret at unlock time.
	ServiceKeyIDs []string
	// SessionTTL bounds a session that ends without logout.
	SessionTTL time.Duration
	Argon      ArgonParameters
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// Keystore implements the operations. It is safe for concurrent use as far
// as its Store is.
type Keystore struct {
	options Options
}

const (
	symmetricKeySizeBytes = 32
	saltSizeBytes         = 16
	aadPasswordWrap       = "opencore/keystore/v1/passwordwrap"
	aadSessionWrap        = "opencore/keystore/v1/sessionwrap"
)

// New validates the options and returns a Keystore.
func New(options Options) (*Keystore, error) {
	if options.Store == nil || options.Directory == nil {
		return nil, errors.New("keystore: store and directory are required")
	}
	if options.SessionTTL <= 0 {
		return nil, errors.New("keystore: session ttl must be positive")
	}
	if options.Argon == (ArgonParameters{}) {
		options.Argon = DefaultArgonParameters
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Keystore{options: options}, nil
}

// Create generates the user's key pair and stores it wrapped with the
// password. The returned key is what the caller publishes in the directory.
func (k *Keystore) Create(ctx context.Context, userID, password string) (DirectoryKey, error) {
	ecdhPrivateKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return DirectoryKey{}, err
	}
	privateKey := ecdhPrivateKey.Bytes()
	defer clear(privateKey)
	publicKey := ecdhPrivateKey.PublicKey().Bytes()
	keyID := jwkThumbprint(jwkCurveX25519, publicKey)

	passwordWrap, err := k.wrapWithPassword(userID, keyID, password, privateKey)
	if err != nil {
		return DirectoryKey{}, err
	}
	err = k.options.Store.Update(ctx, userID, func(currentRecord *UserRecord) (*UserRecord, error) {
		if currentRecord != nil {
			return nil, ErrExists
		}
		return &UserRecord{UserID: userID, KeyID: keyID, PublicKey: publicKey, PasswordWrap: passwordWrap}, nil
	})
	if err != nil {
		return DirectoryKey{}, err
	}
	return DirectoryKey{KeyID: keyID, PublicKey: publicKey, Kind: KindUser, Owner: userID}, nil
}

// PublicKey returns the user's published key.
func (k *Keystore) PublicKey(ctx context.Context, userID string) (DirectoryKey, error) {
	userRecord, err := k.options.Store.View(ctx, userID)
	if err != nil {
		return DirectoryKey{}, err
	}
	return DirectoryKey{KeyID: userRecord.KeyID, PublicKey: userRecord.PublicKey, Kind: KindUser, Owner: userID}, nil
}

// Unlock opens a session. It is called where the password is at hand (IdP
// Logon). The private key is re-encrypted with a fresh session secret, the
// session secret is sealed for every configured service key, and both
// plaintexts are discarded. Each call counts as one open session.
func (k *Keystore) Unlock(ctx context.Context, userID, password string) error {
	userRecord, err := k.options.Store.View(ctx, userID)
	if err != nil {
		return err
	}
	privateKey, err := unwrapWithPassword(userRecord, password)
	if err != nil {
		return err
	}
	defer clear(privateKey)

	sessionSecret := make([]byte, symmetricKeySizeBytes)
	if _, err := rand.Read(sessionSecret); err != nil {
		return err
	}
	defer clear(sessionSecret)

	sessionNonce, sessionCiphertext, err := encryptAESGCM(sessionSecret, privateKey, associatedData(aadSessionWrap, userID, userRecord.KeyID))
	if err != nil {
		return err
	}
	serviceSecrets := make(map[string]Envelope, len(k.options.ServiceKeyIDs))
	for _, serviceKeyID := range k.options.ServiceKeyIDs {
		serviceKey, err := k.options.Directory.LookupKey(ctx, serviceKeyID)
		if err != nil {
			return fmt.Errorf("keystore: service key %q: %w", serviceKeyID, err)
		}
		if serviceKey.Kind != KindService {
			return fmt.Errorf("keystore: key %q is not a service key: %w", serviceKeyID, ErrTargetNotAllowed)
		}
		sealedSecret, err := seal(serviceKey, infoPrefixSession, userID, sessionSecret)
		if err != nil {
			return err
		}
		serviceSecrets[serviceKey.KeyID] = sealedSecret
	}

	now := k.options.Now()
	return k.options.Store.Update(ctx, userID, func(currentRecord *UserRecord) (*UserRecord, error) {
		if currentRecord == nil || currentRecord.KeyID != userRecord.KeyID {
			return nil, ErrConflict
		}
		openSessions := 1
		if currentRecord.Session != nil && now.Before(currentRecord.Session.ExpiresAt) {
			openSessions = currentRecord.Session.OpenSessions + 1
		}
		currentRecord.Session = &SessionState{
			Nonce:          sessionNonce,
			Ciphertext:     sessionCiphertext,
			ServiceSecrets: serviceSecrets,
			OpenSessions:   openSessions,
			ExpiresAt:      now.Add(k.options.SessionTTL),
		}
		return currentRecord, nil
	})
}

// Lock closes one session (IdP DestroySession). When the last one closes,
// the session ciphertexts are deleted.
func (k *Keystore) Lock(ctx context.Context, userID string) error {
	return k.options.Store.Update(ctx, userID, func(currentRecord *UserRecord) (*UserRecord, error) {
		if currentRecord == nil {
			return nil, ErrNotFound
		}
		if currentRecord.Session == nil {
			return currentRecord, nil
		}
		currentRecord.Session.OpenSessions--
		if currentRecord.Session.OpenSessions <= 0 {
			currentRecord.Session = nil
		}
		return currentRecord, nil
	})
}

// SessionSecretFor returns the session secret sealed for one service key.
// The caller must have authenticated the request as coming for this user;
// the envelope is useless without the service's private key.
func (k *Keystore) SessionSecretFor(ctx context.Context, userID, serviceKeyID string) (Envelope, error) {
	userRecord, err := k.options.Store.View(ctx, userID)
	if err != nil {
		return Envelope{}, err
	}
	if !k.sessionOpen(userRecord) {
		return Envelope{}, ErrLocked
	}
	sealedSecret, found := userRecord.Session.ServiceSecrets[serviceKeyID]
	if !found {
		return Envelope{}, ErrTargetNotAllowed
	}
	return sealedSecret, nil
}

// RewrapRequest asks to re-encrypt a content key within an open session.
type RewrapRequest struct {
	UserID        string
	SessionSecret []byte   // opened by the calling service from its sealed copy
	Envelope      Envelope // content key sealed for the user's keystore key
	EntryContext  string   // binding of the content key, e.g. the entry id
	TargetKeyID   string   // must resolve to a service key in the directory
}

// Rewrap opens the envelope with the user's private key and seals the
// content key for a service key. The content key is not returned in the
// clear. Targets other than service keys need RewrapWithPassword.
func (k *Keystore) Rewrap(ctx context.Context, request RewrapRequest) (Envelope, error) {
	userRecord, err := k.options.Store.View(ctx, request.UserID)
	if err != nil {
		return Envelope{}, err
	}
	if !k.sessionOpen(userRecord) {
		return Envelope{}, ErrLocked
	}
	if len(request.SessionSecret) != symmetricKeySizeBytes {
		return Envelope{}, ErrSessionSecret
	}
	targetKey, err := k.options.Directory.LookupKey(ctx, request.TargetKeyID)
	if err != nil {
		return Envelope{}, err
	}
	if targetKey.Kind != KindService {
		return Envelope{}, ErrTargetNotAllowed
	}
	privateKey, err := decryptAESGCM(request.SessionSecret, userRecord.Session.Nonce, userRecord.Session.Ciphertext,
		associatedData(aadSessionWrap, request.UserID, userRecord.KeyID))
	if err != nil {
		return Envelope{}, ErrSessionSecret
	}
	defer clear(privateKey)
	return reseal(privateKey, request.Envelope, request.EntryContext, targetKey)
}

// RewrapWithPassword re-encrypts a content key for any key in the directory
// (another key store of the user, another user). It needs the password again
// and does not depend on an open session.
func (k *Keystore) RewrapWithPassword(ctx context.Context, userID, password string, envelope Envelope, entryContext, targetKeyID string) (Envelope, error) {
	userRecord, err := k.options.Store.View(ctx, userID)
	if err != nil {
		return Envelope{}, err
	}
	targetKey, err := k.options.Directory.LookupKey(ctx, targetKeyID)
	if err != nil {
		return Envelope{}, err
	}
	privateKey, err := unwrapWithPassword(userRecord, password)
	if err != nil {
		return Envelope{}, err
	}
	defer clear(privateKey)
	return reseal(privateKey, envelope, entryContext, targetKey)
}

// ChangePassword re-encrypts the password wrap. Both passwords are needed;
// an open session stays open.
func (k *Keystore) ChangePassword(ctx context.Context, userID, currentPassword, newPassword string) error {
	userRecord, err := k.options.Store.View(ctx, userID)
	if err != nil {
		return err
	}
	privateKey, err := unwrapWithPassword(userRecord, currentPassword)
	if err != nil {
		return err
	}
	defer clear(privateKey)
	newPasswordWrap, err := k.wrapWithPassword(userID, userRecord.KeyID, newPassword, privateKey)
	if err != nil {
		return err
	}
	return k.options.Store.Update(ctx, userID, func(currentRecord *UserRecord) (*UserRecord, error) {
		if currentRecord == nil || currentRecord.KeyID != userRecord.KeyID ||
			!bytes.Equal(currentRecord.PasswordWrap.Nonce, userRecord.PasswordWrap.Nonce) {
			return nil, ErrConflict
		}
		currentRecord.PasswordWrap = newPasswordWrap
		return currentRecord, nil
	})
}

// Reset deletes the user's keystore record, for a password reset without
// the old password. The private key is lost; the returned key id must be
// withdrawn from the directory. A new key pair comes from Create.
func (k *Keystore) Reset(ctx context.Context, userID string) (string, error) {
	retiredKeyID := ""
	err := k.options.Store.Update(ctx, userID, func(currentRecord *UserRecord) (*UserRecord, error) {
		if currentRecord == nil {
			return nil, ErrNotFound
		}
		retiredKeyID = currentRecord.KeyID
		return nil, nil
	})
	return retiredKeyID, err
}

func (k *Keystore) sessionOpen(userRecord *UserRecord) bool {
	return userRecord.Session != nil && k.options.Now().Before(userRecord.Session.ExpiresAt)
}

func reseal(privateKey []byte, envelope Envelope, entryContext string, targetKey DirectoryKey) (Envelope, error) {
	contentKey, err := open(privateKey, infoPrefixContent, entryContext, envelope)
	if err != nil {
		return Envelope{}, err
	}
	defer clear(contentKey)
	return seal(targetKey, infoPrefixContent, entryContext, contentKey)
}

func (k *Keystore) wrapWithPassword(userID, keyID, password string, privateKey []byte) (PasswordWrap, error) {
	salt := make([]byte, saltSizeBytes)
	if _, err := rand.Read(salt); err != nil {
		return PasswordWrap{}, err
	}
	argonParameters := k.options.Argon
	passwordKey := argon2.IDKey([]byte(password), salt, argonParameters.Time, argonParameters.MemoryKiB, argonParameters.Parallelism, symmetricKeySizeBytes)
	defer clear(passwordKey)
	nonce, ciphertext, err := encryptAESGCM(passwordKey, privateKey, associatedData(aadPasswordWrap, userID, keyID))
	if err != nil {
		return PasswordWrap{}, err
	}
	return PasswordWrap{
		Salt:             salt,
		ArgonTime:        argonParameters.Time,
		ArgonMemoryKiB:   argonParameters.MemoryKiB,
		ArgonParallelism: argonParameters.Parallelism,
		Nonce:            nonce,
		Ciphertext:       ciphertext,
	}, nil
}

func unwrapWithPassword(userRecord *UserRecord, password string) ([]byte, error) {
	passwordWrap := userRecord.PasswordWrap
	passwordKey := argon2.IDKey([]byte(password), passwordWrap.Salt, passwordWrap.ArgonTime, passwordWrap.ArgonMemoryKiB, passwordWrap.ArgonParallelism, symmetricKeySizeBytes)
	defer clear(passwordKey)
	privateKey, err := decryptAESGCM(passwordKey, passwordWrap.Nonce, passwordWrap.Ciphertext,
		associatedData(aadPasswordWrap, userRecord.UserID, userRecord.KeyID))
	if err != nil {
		return nil, ErrUnlock
	}
	return privateKey, nil
}

func associatedData(purpose, userID, keyID string) []byte {
	return envelopeInfo(purpose, keyID, userID)
}

func encryptAESGCM(key, plaintext, additionalData []byte) (nonce, ciphertext []byte, err error) {
	aead, err := newAESGCM(key)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	return nonce, aead.Seal(nil, nonce, plaintext, additionalData), nil
}

func decryptAESGCM(key, nonce, ciphertext, additionalData []byte) ([]byte, error) {
	aead, err := newAESGCM(key)
	if err != nil {
		return nil, err
	}
	if len(nonce) != aead.NonceSize() {
		return nil, errors.New("keystore: bad nonce size")
	}
	return aead.Open(nil, nonce, ciphertext, additionalData)
}

func newAESGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
