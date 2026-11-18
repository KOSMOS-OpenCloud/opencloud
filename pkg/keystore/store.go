package keystore

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
)

// ErrNotFound is returned by a Store when a user has no record.
var ErrNotFound = errors.New("keystore: no keystore for this user")

// UserRecord is everything the keystore persists for one user. It holds
// ciphertext and public data only; neither the private key nor a session
// secret is ever stored in the clear.
type UserRecord struct {
	UserID       string        `json:"user_id"`
	KeyID        string        `json:"kid"`
	PublicKey    []byte        `json:"public_key"`
	PasswordWrap PasswordWrap  `json:"password_wrap"`
	Session      *SessionState `json:"session,omitempty"`
}

// PasswordWrap is the private key encrypted with a key derived from the
// user's login password (Argon2id, AES-256-GCM).
type PasswordWrap struct {
	Salt             []byte `json:"salt"`
	ArgonTime        uint32 `json:"argon_time"`
	ArgonMemoryKiB   uint32 `json:"argon_memory_kib"`
	ArgonParallelism uint8  `json:"argon_parallelism"`
	Nonce            []byte `json:"nonce"`
	Ciphertext       []byte `json:"ciphertext"`
}

// SessionState exists between login and logout. The private key is encrypted
// with a random session secret; the session secret itself is stored only
// sealed for the configured services. The keystore cannot open either alone.
type SessionState struct {
	Nonce          []byte              `json:"nonce"`
	Ciphertext     []byte              `json:"ciphertext"`
	ServiceSecrets map[string]Envelope `json:"service_secrets"` // by service key id
	OpenSessions   int                 `json:"open_sessions"`
	ExpiresAt      time.Time           `json:"expires_at"`
}

// Store persists user records. Update must run the callback atomically with
// respect to other calls for the same user. The callback receives nil when
// the user has no record and may return nil to delete the record.
type Store interface {
	View(ctx context.Context, userID string) (*UserRecord, error)
	Update(ctx context.Context, userID string, change func(current *UserRecord) (*UserRecord, error)) error
}

// MemoryStore is a Store for tests.
type MemoryStore struct {
	mutex   sync.Mutex
	records map[string][]byte
}

// NewMemoryStore returns an empty in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{records: map[string][]byte{}}
}

// View implements Store.
func (s *MemoryStore) View(_ context.Context, userID string) (*UserRecord, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return decodeRecord(s.records[userID])
}

// Update implements Store.
func (s *MemoryStore) Update(_ context.Context, userID string, change func(*UserRecord) (*UserRecord, error)) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	currentRecord, err := decodeRecord(s.records[userID])
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	changedRecord, err := change(currentRecord)
	if err != nil {
		return err
	}
	if changedRecord == nil {
		delete(s.records, userID)
		return nil
	}
	encodedRecord, err := json.Marshal(changedRecord)
	if err != nil {
		return err
	}
	s.records[userID] = encodedRecord
	return nil
}

var boltBucketUsers = []byte("users")

// BoltStore is the file-backed Store (one bbolt file, bucket "users").
type BoltStore struct {
	database *bolt.DB
}

// OpenBoltStore opens or creates the store file with mode 0600. bbolt locks
// the file exclusively: only one process (and one handle) may have it open.
func OpenBoltStore(path string) (*BoltStore, error) {
	database, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return nil, err
	}
	err = database.Update(func(transaction *bolt.Tx) error {
		_, bucketErr := transaction.CreateBucketIfNotExists(boltBucketUsers)
		return bucketErr
	})
	if err != nil {
		database.Close()
		return nil, err
	}
	return &BoltStore{database: database}, nil
}

// Close releases the file.
func (s *BoltStore) Close() error { return s.database.Close() }

// View implements Store.
func (s *BoltStore) View(_ context.Context, userID string) (*UserRecord, error) {
	var userRecord *UserRecord
	err := s.database.View(func(transaction *bolt.Tx) error {
		var decodeErr error
		userRecord, decodeErr = decodeRecord(transaction.Bucket(boltBucketUsers).Get([]byte(userID)))
		return decodeErr
	})
	return userRecord, err
}

// Update implements Store.
func (s *BoltStore) Update(_ context.Context, userID string, change func(*UserRecord) (*UserRecord, error)) error {
	return s.database.Update(func(transaction *bolt.Tx) error {
		usersBucket := transaction.Bucket(boltBucketUsers)
		currentRecord, err := decodeRecord(usersBucket.Get([]byte(userID)))
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		changedRecord, err := change(currentRecord)
		if err != nil {
			return err
		}
		if changedRecord == nil {
			return usersBucket.Delete([]byte(userID))
		}
		encodedRecord, err := json.Marshal(changedRecord)
		if err != nil {
			return err
		}
		return usersBucket.Put([]byte(userID), encodedRecord)
	})
}

func decodeRecord(encodedRecord []byte) (*UserRecord, error) {
	if encodedRecord == nil {
		return nil, ErrNotFound
	}
	userRecord := &UserRecord{}
	if err := json.Unmarshal(encodedRecord, userRecord); err != nil {
		return nil, err
	}
	return userRecord, nil
}
