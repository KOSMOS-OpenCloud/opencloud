package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/nats-io/nats.go"
	"github.com/vmihailenco/msgpack/v5"
)

const activitylogVersionKey = "activilog.version"
const currentMigrationVersion = "1"

// RunMigrations checks the activilog data version and runs migrations if necessary.
// It should be called during service startup, after the NATS KeyValue store is initialized.
func runMigrations(ctx context.Context, kv nats.KeyValue) error {
	entry, err := kv.Get(activitylogVersionKey)
	if err == nats.ErrKeyNotFound {
		// Key doesn't exist, version is implicitly pre-V1. Run migration to V1.
		log.Println("Activilog version key not found. Running migration to V1...")
		return migrateToV1(ctx, kv)
	} else if err != nil {
		return fmt.Errorf("failed to get activilog version from NATS KV store: %w", err)
	}

	version := string(entry.Value())
	if version == currentMigrationVersion {
		log.Printf("Activilog data version is '%s'. No migration needed.", version)
		return nil
	}

	// If version is something else, it might indicate a future version or an unexpected state.
	// Add logic here if more complex version handling is needed.
	log.Printf("Activilog data version is '%s', expected '%s'. Migration logic for this scenario is not implemented.", version, currentMigrationVersion)
	return fmt.Errorf("unexpected activilog version: %s, expected %s", version, currentMigrationVersion)
}

// migrateToV1 performs the data migration to version 1.
// It iterates over all keys, expecting their values to be JSON arrays of strings.
// For each such key, it creates a new key in the format "originalKey:count:timestamp"
// and stores the original list of strings (re-marshalled to JSON) as its value.
// Finally, it sets the activilog.version key to "1".
func migrateToV1(_ context.Context, kv nats.KeyValue) error {
	lister, err := kv.ListKeys()
	if err != nil {
		return fmt.Errorf("migrateToV1: failed to list keys from NATS KV store: %w", err)
	}

	migratedCount := 0
	skippedCount := 0

	keyChan := lister.Keys()
	defer lister.Stop()

	// keyValueEnvelope is the data structure used by the go micro plugin which was used previously.
	type keyValueEnvelope struct {
		Key      string                 `json:"key"`
		Data     []byte                 `json:"data"`
		Metadata map[string]interface{} `json:"metadata"`
	}

	for key := range keyChan {
		if key == activitylogVersionKey {
			skippedCount++
			continue // Skip the version key itself
		}

		// Get the original value
		entry, err := kv.Get(key)
		if err == nats.ErrKeyNotFound {
			log.Printf("migrateToV1: Key '%s' disappeared during migration, skipping.", key)
			skippedCount++
			continue
		} else if err != nil {
			log.Printf("migrateToV1: Failed to get value for key '%s': %v. Skipping.", key, err)
			skippedCount++
			continue
		}
		valBytes := entry.Value()

		val := keyValueEnvelope{}
		// Unmarshal the value into the keyValueEnvelope structure
		if err := json.Unmarshal(valBytes, &val); err != nil {
			log.Printf("migrateToV1: Value for key '%s' is not a keyValueEnvelope: %v. Skipping.", key, err)
			skippedCount++
		}

		// Unmarshal value into a list of strings
		var activities []RawActivity
		if err := msgpack.Unmarshal(val.Data, &activities); err != nil {
			if err := json.Unmarshal(val.Data, &activities); err != nil {
				// This key's value is not a JSON array of strings. Skip it.
				log.Printf("migrateToV1: Value for key '%s' is not a msgback or JSON array of strings: %v. Skipping.", key, err)
				skippedCount++
				continue
			}
		}

		// Construct the new key
		newKey := natsKey(val.Key, len(activities))
		newValue, err := msgpack.Marshal(activities)
		if err != nil {
			log.Printf("migrateToV1: Failed to marshal activities for key '%s': %v. Skipping.", key, err)
			skippedCount++
			continue
		}

		// Write the value (the list of strings, marshalled as JSON) under the new key
		if _, err := kv.Put(newKey, newValue); err != nil {
			log.Printf("migrateToV1: Failed to put new key '%s' (original key '%s') in NATS KV store: %v. Skipping.", newKey, key, err)
			skippedCount++
			continue
		}

		// delete old key, it's no longer needed
		if err := kv.Delete(key); err != nil {
			log.Printf("migrateToV1: Failed to delete old key '%s' after migration: %v. Skipping deletion.", key, err)
			skippedCount++
			continue
		}

		log.Printf("migrateToV1: Migrated key '%s' to '%s' with %d elements.", key, newKey, len(activities))
		migratedCount++
	}

	// Set the activilog version to "1" after migration
	if _, err := kv.PutString(activitylogVersionKey, currentMigrationVersion); err != nil {
		return fmt.Errorf("migrateToV1: failed to set activilog version key to '%s' in NATS KV store: %w", currentMigrationVersion, err)
	}

	log.Printf("Migration to V1 complete. Migrated %d keys, skipped %d keys. Activilog version set to '%s'.", migratedCount, skippedCount, currentMigrationVersion)
	return nil
}
