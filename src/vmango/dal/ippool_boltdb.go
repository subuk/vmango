package dal

import (
	"encoding/json"
	"fmt"
	"github.com/boltdb/bolt"
	"vmango/models"
)

type BoltIPPool struct {
	db     *bolt.DB
	bucket []byte
}

func NewBoltIPPool(db *bolt.DB) *BoltIPPool {
	return &BoltIPPool{
		db:     db,
		bucket: []byte("ip-pool"),
	}
}

func (pool *BoltIPPool) List(ips *[]*models.IP) error {
	return pool.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(pool.bucket)
		if bucket == nil {
			return fmt.Errorf(`bucket "%s" not found`, string(pool.bucket))
		}
		return bucket.ForEach(func(k, v []byte) error {
			ip := &models.IP{}
			if err := json.Unmarshal(v, ip); err != nil {
				return err
			}
			*ips = append(*ips, ip)
			return nil
		})
	})
}

func (pool *BoltIPPool) Add(ip *models.IP) error {
	return pool.db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists(pool.bucket)
		if err != nil {
			return err
		}
		value, err := json.Marshal(ip)
		if err != nil {
			return err
		}
		return bucket.Put([]byte(ip.Address), value)
	})
}

func (pool *BoltIPPool) Get(*models.IP) (bool, error) {
	return false, fmt.Errorf("not implemented")
}
