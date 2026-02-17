package checkpoint

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/ivoronin/pll/internal/job"
	bolt "go.etcd.io/bbolt"
)

var bucketName = []byte("results")

type Store struct {
	db *bolt.DB
}

func Open(path string) (*Store, error) {
	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		return nil, fmt.Errorf("opening checkpoint db: %w", err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(bucketName)
		return err
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("creating bucket: %w", err)
	}

	return &Store{db: db}, nil
}

func (s *Store) ShouldRun(j *job.Job) (bool, error) {
	var shouldRun bool
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)
		v := b.Get(s.key(j))
		if v == nil {
			shouldRun = true
			return nil
		}
		var result job.Result
		if err := json.Unmarshal(v, &result); err != nil {
			shouldRun = true
			return nil
		}
		shouldRun = result.Status != job.StatusSuccess
		return nil
	})
	return shouldRun, err
}

func (s *Store) Record(j *job.Job, result job.Result) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)
		v, err := json.Marshal(result)
		if err != nil {
			return err
		}
		return b.Put(s.key(j), v)
	})
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) key(j *job.Job) []byte {
	h := sha256.Sum256([]byte(j.Dir + "\x00" + j.Command))
	k := hex.EncodeToString(h[:])
	return []byte(k)
}
