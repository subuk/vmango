package domain

type Server struct {
	Data map[string]interface{}
	Type string
}

type ServerList []*Server

type StatusInfo struct {
	Name         string
	Type         string
	Description  string
	Connection   string
	MachineCount int
	Memory       struct {
		Total uint64
		Usage int
	}
	Storage struct {
		Total uint64
		Usage int
	}
}

type StatusInfoList []*StatusInfo
