package dal

type StubProvider struct {
	TName     string
	TMachines Machinerep
	TImages   Imagerep
}

func (p *StubProvider) Name() string {
	return p.TName
}
func (p *StubProvider) Machines() Machinerep {
	return p.TMachines
}
func (p *StubProvider) Images() Imagerep {
	return p.TImages
}
