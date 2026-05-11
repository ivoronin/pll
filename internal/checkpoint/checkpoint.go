// Package checkpoint provides persistent job result storage using BoltDB.
package checkpoint

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ivoronin/pll/internal/job"
	bolt "go.etcd.io/bbolt"
)

var (
	bucketName      = []byte("results")
	errNoCheckpoint = errors.New("no checkpoint data found")
)

// Store wraps a BoltDB database for persisting job results.
type Store struct {
	database *bolt.DB
}

// Open creates or opens a checkpoint database at the given path.
func Open(path string) (*Store, error) {
	database, openErr := bolt.Open(path, 0o600, nil)
	if openErr != nil {
		return nil, fmt.Errorf("opening checkpoint db: %w", openErr)
	}

	bucketErr := database.Update(func(tx *bolt.Tx) error {
		_, createErr := tx.CreateBucketIfNotExists(bucketName)

		return createErr
	})
	if bucketErr != nil {
		closeErr := database.Close()

		return nil, errors.Join(fmt.Errorf("creating bucket: %w", bucketErr), closeErr)
	}

	return &Store{database: database}, nil
}

// ShouldRun checks whether a directory needs to be executed based on checkpoint data.
// Returns true if the directory has not been recorded or previously failed.
func (s *Store) ShouldRun(dir string) (bool, error) {
	var shouldRun bool

	viewErr := s.database.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bucketName)
		value := bucket.Get([]byte(dir))

		if value == nil {
			shouldRun = true

			return nil
		}

		var result job.Result

		unmarshalErr := json.Unmarshal(value, &result)
		if unmarshalErr == nil {
			shouldRun = result.Status != job.StatusSuccess
		} else {
			shouldRun = true
		}

		return nil
	})

	return shouldRun, viewErr
}

// Record saves the result of a job execution to the checkpoint database.
func (s *Store) Record(dir string, result job.Result) error {
	return s.database.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bucketName)

		value, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			return marshalErr
		}

		return bucket.Put([]byte(dir), value)
	})
}

// Close closes the underlying BoltDB database.
func (s *Store) Close() error {
	return s.database.Close()
}

// OpenReadOnly opens a checkpoint database in read-only mode.
// Fails fast (1s timeout) if another process holds an exclusive lock on the file.
func OpenReadOnly(path string) (*Store, error) {
	database, openErr := bolt.Open(path, 0o600, &bolt.Options{
		ReadOnly: true,
		Timeout:  1 * time.Second,
	})
	if openErr != nil {
		return nil, fmt.Errorf("opening checkpoint db: %w", openErr)
	}

	return &Store{database: database}, nil
}

// ForEach iterates over all recorded entries in lexicographic key order,
// invoking visit for each (directory, result) pair. Iteration stops on the first error.
func (s *Store) ForEach(visit func(dir string, result job.Result) error) error {
	return s.database.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bucketName)
		if bucket == nil {
			return errNoCheckpoint
		}

		return bucket.ForEach(func(key, value []byte) error {
			var result job.Result

			unmarshalErr := json.Unmarshal(value, &result)
			if unmarshalErr != nil {
				return fmt.Errorf("entry %q: %w", key, unmarshalErr)
			}

			return visit(string(key), result)
		})
	})
}
