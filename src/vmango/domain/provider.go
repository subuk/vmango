package domain

type Provider struct {
	Name     string
	Machines Machinerep
	Images   Imagerep
	Status   Statusrep
}
