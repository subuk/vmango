package models

import (
	"fmt"
	"gopkg.in/alexzorin/libvirt-go.v2"
)

type LibvirtStorage struct {
	conn libvirt.VirConnection
}

func NewLibvirtStorage(uri string) (*LibvirtStorage, error) {
	conn, err := libvirt.NewVirConnection(uri)
	if err != nil {
		return nil, err
	}
	return &LibvirtStorage{conn: conn}, nil
}

func (store *LibvirtStorage) ListMachines() []*VirtualMachine {
	domains, err := store.conn.ListAllDomains(0)
	if err != nil {
		panic(err)
	}
	machines := []*VirtualMachine{}
	for _, domain := range domains {
		name, err := domain.GetName()
		if err != nil {
			panic(err)
		}
		uuid, err := domain.GetUUID()
		if err != nil {
			panic(err)
		}
		info, err := domain.GetInfo()
		if err != nil {
			panic(err)
		}

		machines = append(machines, &VirtualMachine{
			Name:   name,
			Uuid:   fmt.Sprintf("%x", uuid),
			State:  info.GetState(),
			Memory: info.GetMaxMem(),
		})
	}
	return machines
}
