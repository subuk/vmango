package vmango

import (
	"github.com/unrolled/render"
	"gopkg.in/alexzorin/libvirt-go.v2"
)

var Render *render.Render
var DB *Database

type Database struct {
	Conn *libvirt.VirConnection
}

func NewDatabase(uri string) *Database {
	conn, err := libvirt.NewVirConnection(uri)
	if err != nil {
		panic(err)
	}
	return &Database{&conn}
}
