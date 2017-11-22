package dal

import (
	"vmango/domain"
)

type StubStatusrep struct {
	FetchResponse struct {
		Status *domain.StatusInfo
		Error  error
	}
}

func (repo *StubStatusrep) Fetch(status *domain.StatusInfo) error {
	if repo.FetchResponse.Status != nil {
		*status = *repo.FetchResponse.Status
	}
	return repo.FetchResponse.Error
}
