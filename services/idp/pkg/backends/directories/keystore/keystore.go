// Package keystore connects the 'directories' identifier backend with
// pkg/kosmoskeystore: the user's key store is opened where the password is at
// hand (Logon) and closed when lico destroys the session. A failing key store
// never blocks a login. See opendock/OPENCORE_KEYSTORE.md §10.
package keystore

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/opencloud-eu/opencloud/pkg/kosmoskeystore"
)

// sessionRefPrefix marks session references issued by this package. lico
// keeps the reference in its logon cookie and hands it back on logout.
const sessionRefPrefix = "kosmos-keystore:"

// DefaultSessionTTL bounds a key store session that ends without logout.
const DefaultSessionTTL = 12 * time.Hour

// Operations is the part of kosmoskeystore.Keystore the hook needs.
type Operations interface {
	Create(ctx context.Context, userID, password string) (kosmoskeystore.DirectoryKey, error)
	Unlock(ctx context.Context, userID, password string) error
	Lock(ctx context.Context, userID string) error
}

// Hook is called by the identifier backend. A nil *Hook is valid and does
// nothing, so the backend needs no checks when the key store is disabled.
type Hook struct {
	operations Operations
	logger     logrus.FieldLogger
}

// NewHook wraps key store operations.
func NewHook(operations Operations, logger logrus.FieldLogger) *Hook {
	return &Hook{operations: operations, logger: logger}
}

// LoggedOn opens the user's key store after a successful password check and
// returns the session reference for lico, or nil if no session was opened.
// A user without a key store gets one with this password.
//
// A password that does not open an existing key store (admin reset, change in
// an external directory) leaves it untouched and locked: the wrapped private
// key is still recoverable with the old password, so nothing is replaced here.
func (h *Hook) LoggedOn(ctx context.Context, userID, password string) *string {
	if h == nil {
		return nil
	}
	unlockErr := h.operations.Unlock(ctx, userID, password)
	if errors.Is(unlockErr, kosmoskeystore.ErrNotFound) {
		createdKey, createErr := h.operations.Create(ctx, userID, password)
		if createErr != nil && !errors.Is(createErr, kosmoskeystore.ErrExists) {
			h.logger.WithError(createErr).WithField("id", userID).Warnln("keystore: create failed, login continues without key store")
			return nil
		}
		if createErr == nil {
			h.logger.WithFields(logrus.Fields{"id": userID, "kid": createdKey.KeyID}).Infoln("keystore: created")
		}
		unlockErr = h.operations.Unlock(ctx, userID, password)
	}
	switch {
	case unlockErr == nil:
		sessionRef := sessionRefPrefix + userID
		return &sessionRef
	case errors.Is(unlockErr, kosmoskeystore.ErrUnlock):
		h.logger.WithField("id", userID).Warnln("keystore: login password does not open the key store, it stays locked")
	default:
		h.logger.WithError(unlockErr).WithField("id", userID).Warnln("keystore: unlock failed, login continues without key store")
	}
	return nil
}

// SessionDestroyed closes one key store session. It reports whether the
// session reference was issued by this package.
func (h *Hook) SessionDestroyed(ctx context.Context, sessionRef *string) bool {
	if h == nil || sessionRef == nil || !strings.HasPrefix(*sessionRef, sessionRefPrefix) {
		return false
	}
	userID := strings.TrimPrefix(*sessionRef, sessionRefPrefix)
	if err := h.operations.Lock(ctx, userID); err != nil {
		h.logger.WithError(err).WithField("id", userID).Warnln("keystore: lock failed")
	}
	return true
}

// ServiceDirectory is the kosmoskeystore.Directory of the configured service
// keys. Only these keys can receive session secrets and re-encrypted content
// keys within a session.
type ServiceDirectory map[string]kosmoskeystore.DirectoryKey

// LookupKey implements kosmoskeystore.Directory.
func (d ServiceDirectory) LookupKey(_ context.Context, keyID string) (kosmoskeystore.DirectoryKey, error) {
	serviceKey, found := d[keyID]
	if !found {
		return kosmoskeystore.DirectoryKey{}, kosmoskeystore.ErrUnknownKey
	}
	return serviceKey, nil
}

// KeyIDs returns the ids of all service keys.
func (d ServiceDirectory) KeyIDs() []string {
	keyIDs := make([]string, 0, len(d))
	for keyID := range d {
		keyIDs = append(keyIDs, keyID)
	}
	return keyIDs
}

// ParseServiceKeys reads entries of the form "<service name>=<X25519 public
// key, base64url without padding>", separated by commas.
func ParseServiceKeys(serviceKeysList string) (ServiceDirectory, error) {
	serviceDirectory := ServiceDirectory{}
	for _, serviceKeyEntry := range strings.Split(serviceKeysList, ",") {
		serviceKeyEntry = strings.TrimSpace(serviceKeyEntry)
		if serviceKeyEntry == "" {
			continue
		}
		serviceName, encodedPublicKey, hasSeparator := strings.Cut(serviceKeyEntry, "=")
		serviceName = strings.TrimSpace(serviceName)
		if !hasSeparator || serviceName == "" {
			return nil, fmt.Errorf("keystore: service key %q is not of the form name=publickey", serviceKeyEntry)
		}
		publicKey, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(encodedPublicKey))
		if err != nil {
			return nil, fmt.Errorf("keystore: service key of %q is not base64url: %w", serviceName, err)
		}
		keyID, err := kosmoskeystore.KeyID(publicKey)
		if err != nil {
			return nil, fmt.Errorf("keystore: service key of %q: %w", serviceName, err)
		}
		serviceDirectory[keyID] = kosmoskeystore.DirectoryKey{
			KeyID: keyID, PublicKey: publicKey, Kind: kosmoskeystore.KindService, Owner: serviceName,
		}
	}
	return serviceDirectory, nil
}

// Open builds the key store from its settings. An empty path means the key
// store is disabled; the result is then nil without error.
func Open(storePath, serviceKeysList, sessionTTL string) (*kosmoskeystore.Keystore, error) {
	if storePath == "" {
		return nil, nil
	}
	serviceDirectory, err := ParseServiceKeys(serviceKeysList)
	if err != nil {
		return nil, err
	}
	sessionDuration := DefaultSessionTTL
	if sessionTTL != "" {
		sessionDuration, err = time.ParseDuration(sessionTTL)
		if err != nil {
			return nil, fmt.Errorf("keystore: session ttl %q: %w", sessionTTL, err)
		}
	}
	boltStore, err := kosmoskeystore.OpenBoltStore(storePath)
	if err != nil {
		return nil, fmt.Errorf("keystore: open %s: %w", storePath, err)
	}
	return kosmoskeystore.New(kosmoskeystore.Options{
		Store:         boltStore,
		Directory:     serviceDirectory,
		ServiceKeyIDs: serviceDirectory.KeyIDs(),
		SessionTTL:    sessionDuration,
	})
}
