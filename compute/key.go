package compute

type Key struct {
	Type        string
	Value       []byte
	Comment     string
	Options     []string
	Fingerprint string
}

func (key *Key) ValueString() string {
	return string(key.Value)
}
