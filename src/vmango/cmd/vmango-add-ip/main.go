package main

import (
	"flag"
	log "github.com/Sirupsen/logrus"
	"github.com/boltdb/bolt"
	"vmango/models"
)

var (
	DB_PATH = flag.String("db", "vmango.db", "Database path")
	MASK    = flag.Int("mask", 0, "IP address netmask to add")
	ADDRESS = flag.String("ip", "", "IP address to add")
	GW      = flag.String("gw", "", "IP address gateway to add")
)

func main() {
	flag.Parse()

	if *ADDRESS == "" {
		log.Fatal("address not specified")
	}
	if *MASK == 0 {
		log.Fatal("mask not specified")
	}
	if *GW == "" {
		log.Fatal("gateway not specified")
	}

	db, err := bolt.Open(*DB_PATH, 0600, nil)
	if err != nil {
		log.WithError(err).Fatal("failed to open database")
	}

	pool := models.NewBoltIPPool(db)

	ip := &models.IP{
		Address: *ADDRESS,
		Gateway: *GW,
		Netmask: *MASK,
		UsedBy:  "",
	}
	if err := pool.Add(ip); err != nil {
		log.WithError(err).WithField("ip", ip).Fatal("failed to add ip address")
	}
}
