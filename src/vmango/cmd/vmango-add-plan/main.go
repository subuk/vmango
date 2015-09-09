package main

import (
	"flag"
	log "github.com/Sirupsen/logrus"
	"github.com/boltdb/bolt"
	"vmango/dal"
	"vmango/models"
)

var (
	DB_PATH = flag.String("db", "vmango.db", "Database path")
	NAME    = flag.String("name", "", "Plan name")
	MEMORY  = flag.Int("memory", 0, "Memory limit (MB)")
	CPUS    = flag.Int("cpus", 0, "Cpus count")
	DISK    = flag.Int("disk", 0, "Disk size (GB)")
)

func main() {
	flag.Parse()

	if *NAME == "" {
		log.Fatal("name required")
	}
	if *MEMORY == 0 {
		log.Fatal("memory required")
	}
	if *CPUS == 0 {
		log.Fatal("cpus required")
	}

	db, err := bolt.Open(*DB_PATH, 0600, nil)
	if err != nil {
		log.WithError(err).Fatal("failed to open database")
	}

	planrep := dal.NewBoltPlanrep(db)

	plan := &models.Plan{
		Name:     *NAME,
		Memory:   *MEMORY * 1024 * 1024,
		Cpus:     *CPUS,
		DiskSize: *DISK * 1024 * 1024 * 1024,
	}
	if err := planrep.Add(plan); err != nil {
		log.WithError(err).WithField("plan", plan).Fatal("failed to add ip address")
	}
}
