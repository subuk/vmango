package domain

type Plan struct {
	Name     string
	Memory   int
	Cpus     int
	DiskSize int
}

func (plan *Plan) DiskSizeGigabytes() int {
	return int(plan.DiskSize / 1024 / 1024 / 1024)
}

func (plan *Plan) MemoryMegabytes() int {
	return int(plan.Memory / 1024 / 1024)
}
