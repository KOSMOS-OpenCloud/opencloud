package keystore

// Das Umschlagformat liegt in einem eigenen Modul (github.com/KOSMOS-OpenCloud/opencore-envelope, Entscheidung E-K8
// in opendock/OPENCORE_KEYSTORE.md): eine Quelle für opencloud, opendock und Dienste. Diese Datei reicht nur durch,
// damit die Namen im Paket keystore gleich bleiben. Format und HPKE-Info sind unverändert.

import (
	envelope "github.com/KOSMOS-OpenCloud/opencore-envelope"
)

// SuiteX25519AES256GCM names the only envelope suite of version 1:
// HPKE (RFC 9180) base mode, DHKEM(X25519, HKDF-SHA256), HKDF-SHA256, AES-256-GCM.
const SuiteX25519AES256GCM = envelope.SuiteX25519AES256GCM

const (
	infoPrefixContent  = envelope.PurposeContentKey
	infoPrefixSession  = envelope.PurposeSessionSecret
	jwkCurveX25519     = "X25519"
	x25519KeySizeBytes = envelope.X25519KeySize
)

// ErrEnvelope is returned when an envelope cannot be opened: wrong recipient,
// wrong context, tampered data or an unsupported format.
var ErrEnvelope = envelope.ErrEnvelope

// Envelope carries a secret sealed for exactly one recipient key (see opencore-envelope).
type Envelope = envelope.Envelope

// KeyID returns the RFC 7638 JWK thumbprint (SHA-256, base64url) of an X25519
// public key. It is the stable identifier under which a key is published in
// the directory.
func KeyID(publicKey []byte) (string, error) {
	return envelope.KeyID(publicKey)
}

func jwkThumbprint(curve string, publicKey []byte) string {
	return envelope.JWKThumbprint(curve, publicKey)
}

// SealContentKey seals a content key for a recipient published in the
// directory. Senders (third parties depositing an entry) and the keystore
// itself (re-encryption) use the same function, so the format has one source.
func SealContentKey(recipient DirectoryKey, entryContext string, contentKey []byte) (Envelope, error) {
	return envelope.SealContentKey(asRecipient(recipient), entryContext, contentKey)
}

// OpenContentKey opens an envelope with the recipient's private key. Services
// use it to read what the keystore re-encrypted for them.
func OpenContentKey(recipientPrivateKey []byte, sealedEnvelope Envelope, entryContext string) ([]byte, error) {
	return envelope.OpenContentKey(recipientPrivateKey, sealedEnvelope, entryContext)
}

// OpenSessionSecret opens the session secret the keystore sealed for a
// service at unlock time. The context is the user id.
func OpenSessionSecret(servicePrivateKey []byte, sealedEnvelope Envelope, userID string) ([]byte, error) {
	return envelope.OpenSessionSecret(servicePrivateKey, sealedEnvelope, userID)
}

func seal(recipient DirectoryKey, infoPrefix, bindingContext string, plaintext []byte) (Envelope, error) {
	return envelope.Seal(asRecipient(recipient), infoPrefix, bindingContext, plaintext)
}

func open(recipientPrivateKey []byte, infoPrefix, bindingContext string, sealedEnvelope Envelope) ([]byte, error) {
	return envelope.Open(recipientPrivateKey, infoPrefix, bindingContext, sealedEnvelope)
}

// envelopeInfo builds the HPKE info (length-prefixed parts), also used as
// associated data in this package.
func envelopeInfo(infoPrefix, recipientKeyID, bindingContext string) []byte {
	return envelope.Info(infoPrefix, recipientKeyID, bindingContext)
}

func asRecipient(directoryKey DirectoryKey) envelope.Recipient {
	return envelope.Recipient{KeyID: directoryKey.KeyID, PublicKey: directoryKey.PublicKey}
}
