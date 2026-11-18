package keystore

import (
	"crypto/ecdh"
	"crypto/hpke"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
)

// SuiteX25519AES256GCM names the only envelope suite of version 1:
// HPKE (RFC 9180) base mode, DHKEM(X25519, HKDF-SHA256), HKDF-SHA256, AES-256-GCM.
const SuiteX25519AES256GCM = "HPKE-X25519-SHA256-A256GCM"

const (
	envelopeVersion    = 1
	infoPrefixContent  = "opencore/keystore/v1/cek"
	infoPrefixSession  = "opencore/keystore/v1/session"
	jwkCurveX25519     = "X25519"
	x25519KeySizeBytes = 32
)

// ErrEnvelope is returned when an envelope cannot be opened: wrong recipient,
// wrong context, tampered data or an unsupported format.
var ErrEnvelope = errors.New("keystore: envelope cannot be opened")

// Envelope carries a secret sealed for exactly one recipient key. The secret
// is bound to the recipient key id and to a caller-chosen context (for a
// content key: the id of the keystore entry), so an envelope cannot be
// replayed for another entry or another recipient.
type Envelope struct {
	Version int    `json:"v"`
	Suite   string `json:"suite"`
	KeyID   string `json:"kid"`
	Sealed  []byte `json:"sealed"` // HPKE encapsulated key followed by the ciphertext
}

// KeyID returns the RFC 7638 JWK thumbprint (SHA-256, base64url) of an X25519
// public key. It is the stable identifier under which a key is published in
// the directory.
func KeyID(publicKey []byte) (string, error) {
	if len(publicKey) != x25519KeySizeBytes {
		return "", fmt.Errorf("keystore: public key must be %d bytes, got %d", x25519KeySizeBytes, len(publicKey))
	}
	return jwkThumbprint(jwkCurveX25519, publicKey), nil
}

func jwkThumbprint(curve string, publicKey []byte) string {
	// RFC 7638: required members in lexicographic order, no whitespace.
	canonicalJWK := fmt.Sprintf(`{"crv":"%s","kty":"OKP","x":"%s"}`, curve, base64.RawURLEncoding.EncodeToString(publicKey))
	digest := sha256.Sum256([]byte(canonicalJWK))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

// SealContentKey seals a content key for a recipient published in the
// directory. Senders (third parties depositing an entry) and the keystore
// itself (re-encryption) use the same function, so the format has one source.
func SealContentKey(recipient DirectoryKey, entryContext string, contentKey []byte) (Envelope, error) {
	return seal(recipient, infoPrefixContent, entryContext, contentKey)
}

// OpenContentKey opens an envelope with the recipient's private key. Services
// use it to read what the keystore re-encrypted for them.
func OpenContentKey(recipientPrivateKey []byte, envelope Envelope, entryContext string) ([]byte, error) {
	return open(recipientPrivateKey, infoPrefixContent, entryContext, envelope)
}

// OpenSessionSecret opens the session secret the keystore sealed for a
// service at unlock time. The context is the user id.
func OpenSessionSecret(servicePrivateKey []byte, envelope Envelope, userID string) ([]byte, error) {
	return open(servicePrivateKey, infoPrefixSession, userID, envelope)
}

func seal(recipient DirectoryKey, infoPrefix, bindingContext string, plaintext []byte) (Envelope, error) {
	recipientKeyID, err := KeyID(recipient.PublicKey)
	if err != nil {
		return Envelope{}, err
	}
	if recipient.KeyID != recipientKeyID {
		return Envelope{}, fmt.Errorf("keystore: directory key id %q does not match its public key", recipient.KeyID)
	}
	ecdhPublicKey, err := ecdh.X25519().NewPublicKey(recipient.PublicKey)
	if err != nil {
		return Envelope{}, fmt.Errorf("keystore: invalid recipient public key: %w", err)
	}
	hpkePublicKey, err := hpke.NewDHKEMPublicKey(ecdhPublicKey)
	if err != nil {
		return Envelope{}, err
	}
	sealed, err := hpke.Seal(hpkePublicKey, hpke.HKDFSHA256(), hpke.AES256GCM(), envelopeInfo(infoPrefix, recipientKeyID, bindingContext), plaintext)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{Version: envelopeVersion, Suite: SuiteX25519AES256GCM, KeyID: recipientKeyID, Sealed: sealed}, nil
}

func open(recipientPrivateKey []byte, infoPrefix, bindingContext string, envelope Envelope) ([]byte, error) {
	if envelope.Version != envelopeVersion || envelope.Suite != SuiteX25519AES256GCM {
		return nil, ErrEnvelope
	}
	ecdhPrivateKey, err := ecdh.X25519().NewPrivateKey(recipientPrivateKey)
	if err != nil {
		return nil, ErrEnvelope
	}
	// The key id inside the info is derived from the private key, not taken
	// from the envelope: an envelope relabelled for another recipient fails.
	recipientKeyID := jwkThumbprint(jwkCurveX25519, ecdhPrivateKey.PublicKey().Bytes())
	if envelope.KeyID != recipientKeyID {
		return nil, ErrEnvelope
	}
	hpkePrivateKey, err := hpke.NewDHKEMPrivateKey(ecdhPrivateKey)
	if err != nil {
		return nil, ErrEnvelope
	}
	plaintext, err := hpke.Open(hpkePrivateKey, hpke.HKDFSHA256(), hpke.AES256GCM(), envelopeInfo(infoPrefix, recipientKeyID, bindingContext), envelope.Sealed)
	if err != nil {
		return nil, ErrEnvelope
	}
	return plaintext, nil
}

// envelopeInfo builds the HPKE info. Each part is length-prefixed so that no
// two different (prefix, key id, context) triples yield the same bytes.
func envelopeInfo(infoPrefix, recipientKeyID, bindingContext string) []byte {
	info := make([]byte, 0, len(infoPrefix)+len(recipientKeyID)+len(bindingContext)+12)
	for _, part := range []string{infoPrefix, recipientKeyID, bindingContext} {
		partLength := uint32(len(part))
		info = append(info, byte(partLength>>24), byte(partLength>>16), byte(partLength>>8), byte(partLength))
		info = append(info, part...)
	}
	return info
}
