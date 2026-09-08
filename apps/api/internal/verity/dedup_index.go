package verity

import (
	"errors"
	"fmt"
	"os"
	"time"

	bolt "go.etcd.io/bbolt"
)

var dedupBucket = []byte("record_ids")

type dedupIndex struct {
	database *bolt.DB
	tx       *bolt.Tx
	bucket   *bolt.Bucket
	pending  int
}

func openDedupIndex(path string) (*dedupIndex, error) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	database, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: time.Second, NoGrowSync: false})
	if err != nil {
		return nil, fmt.Errorf("open dedup index: %w", err)
	}
	index := &dedupIndex{database: database}
	if err := index.begin(); err != nil {
		database.Close()
		return nil, err
	}
	return index, nil
}

func (d *dedupIndex) begin() error {
	tx, err := d.database.Begin(true)
	if err != nil {
		return err
	}
	bucket, err := tx.CreateBucketIfNotExists(dedupBucket)
	if err != nil {
		tx.Rollback()
		return err
	}
	d.tx = tx
	d.bucket = bucket
	d.pending = 0
	return nil
}

func (d *dedupIndex) Seen(recordID string) (bool, error) {
	key := []byte(recordID)
	if d.bucket.Get(key) != nil {
		return true, nil
	}
	if err := d.bucket.Put(key, []byte{1}); err != nil {
		return false, err
	}
	d.pending++
	if d.pending >= 10_000 {
		if err := d.tx.Commit(); err != nil {
			return false, err
		}
		d.tx = nil
		d.bucket = nil
		if err := d.begin(); err != nil {
			return false, err
		}
	}
	return false, nil
}

func (d *dedupIndex) Close(commit bool) error {
	var err error
	if d.tx != nil {
		if commit {
			err = d.tx.Commit()
		} else {
			err = d.tx.Rollback()
		}
		d.tx = nil
	}
	if closeErr := d.database.Close(); err == nil {
		err = closeErr
	}
	return err
}
