package compute

import "sync"

// visitCloudHosts bounds independent backend reads and preserves configured order.
func visitCloudHosts[T any](c Cloud, read func(CloudHost) (T, error)) ([]T, []error) {
	results, failures := make([]T, len(c.Hosts)), make([]error, len(c.Hosts))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range min(hostReadConcurrency, len(c.Hosts)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := range jobs {
				results[n], failures[n] = read(c.Hosts[n])
			}
		}()
	}
	for n := range c.Hosts {
		jobs <- n
	}
	close(jobs)
	wg.Wait()
	return results, failures
}

// Discover preserves configured catalogs even when some backend reads fail.
// Offers and storage availability include only successfully checked hosts.
func (s *CloudService) Discover(c Cloud) ([]CloudOffer, []StorageAvailability, []string) {
	type hostCatalog struct {
		offers  []CloudOffer
		storage []StorageAvailability
	}
	results, failures := visitCloudHosts(c, func(h CloudHost) (hostCatalog, error) {
		single := c
		single.Hosts = []CloudHost{h}
		storage, err := s.StorageAvailability(single)
		if err != nil {
			return hostCatalog{}, err
		}
		offers, err := s.Offers(single)
		return hostCatalog{offers, storage}, err
	})
	offers, storage, unavailable := []CloudOffer{}, []StorageAvailability{}, []string{}
	byZone := map[string]map[string]bool{}
	for n, result := range results {
		if failures[n] != nil {
			unavailable = append(unavailable, c.Hosts[n].Node)
			continue
		}
		offers = append(offers, result.offers...)
		for _, zone := range result.storage {
			if byZone[zone.Zone] == nil {
				byZone[zone.Zone] = map[string]bool{}
			}
			for _, class := range zone.StorageClasses {
				byZone[zone.Zone][class] = true
			}
		}
	}
	for _, zone := range c.Zones {
		entry := StorageAvailability{Zone: zone.ID, StorageClasses: []string{}}
		for _, class := range c.StorageClasses {
			if byZone[zone.ID][class.ID] {
				entry.StorageClasses = append(entry.StorageClasses, class.ID)
			}
		}
		storage = append(storage, entry)
	}
	return offers, storage, unavailable
}
