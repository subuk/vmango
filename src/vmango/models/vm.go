package models

import (
	"fmt"
	"vmango"
)

type VirtualMachine struct {
	Name  string
	State int
	Uuid  string
}

func (v *VirtualMachine) String() string {
	return v.Name
}

func VirtualMachineList() []*VirtualMachine {
	domainIds, err := vmango.DB.Conn.ListDomains()
	if err != nil {
		panic(err)
	}
	machines := []*VirtualMachine{}
	for _, domainId := range domainIds {
		domain, err := vmango.DB.Conn.LookupDomainById(domainId)
		if err != nil {
			panic(err)
		}
		name, err := domain.GetName()
		if err != nil {
			panic(err)
		}
		uuid, err := domain.GetUUID()
		if err != nil {
			panic(err)
		}
		machines = append(machines, &VirtualMachine{
			Name: name,
			Uuid: fmt.Sprintf("%x", uuid),
		})
	}
	return machines
}
