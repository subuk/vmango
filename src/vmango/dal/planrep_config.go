package dal

import (
	"vmango/cfg"
	"vmango/models"
)

type ConfigPlanrep struct {
	plans []*models.Plan
}

func NewConfigPlanrep(planConfigs []cfg.PlanConfig) *ConfigPlanrep {
	repo := &ConfigPlanrep{
		plans: []*models.Plan{},
	}
	for _, planConfig := range planConfigs {
		plan := &models.Plan{
			Name:     planConfig.Name,
			Memory:   planConfig.Memory * 1024 * 1024,
			Cpus:     planConfig.Cpus,
			DiskSize: planConfig.DiskSize * 1024 * 1024 * 1024,
		}
		repo.plans = append(repo.plans, plan)
	}
	return repo
}

func (repo *ConfigPlanrep) List(plans *[]*models.Plan) error {
	*plans = *(&repo.plans)
	return nil

}
func (repo *ConfigPlanrep) Get(needle *models.Plan) (bool, error) {
	for _, plan := range repo.plans {
		if plan.Name == needle.Name {
			*needle = *plan
			return true, nil
		}
	}
	return false, nil
}
