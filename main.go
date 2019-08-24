package main

import (
	"os"
	"subuk/vmango/bootstrap"
	"subuk/vmango/util"

	"github.com/akamensky/argparse"
)

func main() {
	// connPool := libvirt.NewConnectionPool("qemu+ssh://mkruglov@co101.ash1.fun.co/system?socket=/var/run/libvirt/libvirt-sock")
	// machinesRepo := libvirt.NewVirtualMachineRepository(connPool)
	// machines, err := machinesRepo.List()
	// if err != nil {
	// 	panic(err)
	// }
	// for _, machine := range machines {
	// 	fmt.Println(machine.Id, machine.VCpus, "cpus", machine.Memory/1024, "MiB of memory")
	// 	for _, volume := range machine.Volumes {
	// 		fmt.Println("  ", volume.Type, volume.Path)
	// 	}
	// 	for _, iface := range machine.Interfaces {
	// 		fmt.Println("  ", "interface", iface.Mac)
	// 	}
	// }
	// pw, _ := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
	// fmt.Println(string(pw))
	parser := argparse.NewParser("vmango", "Vmango Virtual Machine Manager")
	configFilename := parser.String("c", "config", &argparse.Options{
		Default: util.GetenvDefault("VMANGO_CONFIG", "vmango.conf"),
		Help:    "Configuration file path",
	})
	if err := parser.Parse(os.Args); err != nil {
		os.Exit(1)
	}
	bootstrap.Web(*configFilename)
}
