package compute

import (
	"context"
	"errors"
	"testing"
)

type administrationTestBackend struct {
	AdministrationObservations
	calls       int
	unavailable map[string]bool
}

func (b *administrationTestBackend) Host(_ context.Context, _ string, node string) (*Node, error) {
	b.calls++
	if b.unavailable[node] {
		return nil, ErrUnavailable
	}
	return &Node{Id: node}, nil
}

func TestAdministrationCommonCloudHasNoHostDependency(t *testing.T) {
	snapshot := AdministrationSnapshot{Clouds: []AdministrationCloud{{ID: "external", Name: "External cloud", BackendKind: "external", Zones: []CatalogEntry{}, InstanceTypes: []InstanceType{}, Networks: []CatalogEntry{}, StorageClasses: []CatalogEntry{}, Images: []string{"linux"}}}, Images: []AdministrationImage{{CloudID: "external", ID: "linux", Name: "Linux"}}}
	s := NewAdministrationService(snapshot, nil)
	if c, err := s.Cloud("external"); err != nil || c.Name != "External cloud" || c.Libvirt != nil {
		t.Fatal(c, err)
	}
	if images, err := s.Images("external", ""); err != nil || len(images) != 1 {
		t.Fatal(images, err)
	}
	if _, err := s.Hosts("external"); !errors.Is(err, ErrNotFound) {
		t.Fatal("hostless cloud exposed hosts", err)
	}
	if _, err := s.ImageAvailability(context.Background(), "external", "linux"); !errors.Is(err, ErrNotFound) {
		t.Fatal("unsupported image observation exposed", err)
	}
	if _, err := s.Images("external", "invalid"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}
func TestAdministrationCopiesSnapshotAndValidatesBeforeObservation(t *testing.T) {
	snapshot := AdministrationSnapshot{Clouds: []AdministrationCloud{{ID: "b", Capabilities: AdministrationCapabilities{HostInventory: true}}, {ID: "a", Capabilities: AdministrationCapabilities{HostInventory: true}}}, Hosts: []AdministrationHost{{ID: "h", CloudID: "a", Labels: map[string]string{"rack": "original"}}, {ID: "other", CloudID: "b"}}, Images: []AdministrationImage{{ID: "same", CloudID: "a", Name: "A"}, {ID: "same", CloudID: "b", Name: "B"}}}
	b := &administrationTestBackend{unavailable: map[string]bool{}}
	s := NewAdministrationService(snapshot, map[string]AdministrationObservations{"a": b, "b": b})
	snapshot.Hosts[0].Labels["rack"] = "edited"
	snapshot.Clouds[0].ID = "edited"
	snapshot.Images[0].Name = "edited"
	h, _ := s.HostConfiguration("a", "h")
	h.Labels["rack"] = "mutated response"
	h, _ = s.HostConfiguration("a", "h")
	if h.Labels["rack"] != "original" {
		t.Fatal("snapshot is mutable")
	}
	if s.Clouds()[0].ID != "a" {
		t.Fatal("clouds are not deterministically ordered")
	}
	i, _ := s.Image("a", "same")
	if i.Name != "A" {
		t.Fatal(i)
	}
	for _, pair := range [][2]string{{"missing", "h"}, {"a", "other"}, {"a", "missing"}} {
		if _, err := s.LiveHost(context.Background(), pair[0], pair[1]); !errors.Is(err, ErrNotFound) {
			t.Fatal(pair, err)
		}
	}
	if b.calls != 0 {
		t.Fatal("backend consulted before membership validation")
	}
	if _, err := s.LiveHost(context.Background(), "a", "h"); err != nil || b.calls != 1 {
		t.Fatal(err, b.calls)
	}
}
func TestAdministrationPartialAndAllUnavailableHosts(t *testing.T) {
	snapshot := AdministrationSnapshot{Clouds: []AdministrationCloud{{ID: "a", Capabilities: AdministrationCapabilities{HostInventory: true}}}, Hosts: []AdministrationHost{{ID: "z", CloudID: "a"}, {ID: "a", CloudID: "a"}}}
	b := &administrationTestBackend{unavailable: map[string]bool{"z": true}}
	s := NewAdministrationService(snapshot, map[string]AdministrationObservations{"a": b})
	list, err := s.LiveHosts(context.Background(), "a")
	if err != nil || len(list.Items) != 1 || list.Items[0].Id != "a" || len(list.UnavailableHosts) != 1 || list.UnavailableHosts[0].Code != "host_unavailable" {
		t.Fatal(list, err)
	}
	b.unavailable["a"] = true
	list, err = s.LiveHosts(context.Background(), "a")
	if err != nil || list.Items == nil || len(list.Items) != 0 || len(list.UnavailableHosts) != 2 {
		t.Fatal(list, err)
	}
}
