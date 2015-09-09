package dal

import (
	"encoding/json"
	"fmt"
	"github.com/boltdb/bolt"
	"vmango/models"
)

type BoltPlanrep struct {
	db     *bolt.DB
	bucket []byte
}

func NewBoltPlanrep(db *bolt.DB) *BoltPlanrep {
	return &BoltPlanrep{
		db:     db,
		bucket: []byte("plans"),
	}
}

func (pool *BoltPlanrep) List(plans *[]*models.Plan) error {
	return pool.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(pool.bucket)
		if bucket == nil {
			return fmt.Errorf(`bucket "%s" not found`, string(pool.bucket))
		}
		return bucket.ForEach(func(k, v []byte) error {
			plan := &models.Plan{}
			if err := json.Unmarshal(v, plan); err != nil {
				return err
			}
			*plans = append(*plans, plan)
			return nil
		})
	})
}

func (pool *BoltPlanrep) Add(plan *models.Plan) error {
	return pool.db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists(pool.bucket)
		if err != nil {
			return err
		}
		value, err := json.Marshal(plan)
		if err != nil {
			return err
		}
		return bucket.Put([]byte(plan.Name), value)
	})
}

func (pool *BoltPlanrep) Get(*models.Plan) (bool, error) {
	return false, fmt.Errorf("not implemented")
}
