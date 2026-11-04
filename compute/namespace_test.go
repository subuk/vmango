package compute

import (
	"errors"
	"testing"
)

func TestNamespaceRegistrySnapshotAndAdmission(t *testing.T) {
	definitions := []Namespace{{ID: "platform", Clouds: []string{"main", "edge"}}, {ID: "analytics", Name: "Analytics", Clouds: []string{"main"}}}
	s, err := NewNamespaceService(definitions)
	if err != nil {
		t.Fatal(err)
	}
	definitions[0].Clouds[0] = "changed"
	first, err := s.Get("platform")
	if err != nil || first.Name != "platform" || first.Clouds[0] != "main" {
		t.Fatalf("invalid accepted snapshot: %+v %v", first, err)
	}
	first.Clouds[0] = "changed-again"
	listed := s.List()
	listed[0].Clouds[0] = "changed-list"
	if err := s.Admit("platform", "main"); err != nil {
		t.Fatal("caller mutated registry", err)
	}
	if err := s.Admit("analytics", "edge"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unadmitted cloud: %v", err)
	}
	if _, err := s.Get(""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("implicit namespace: %v", err)
	}
	removed, _ := NewNamespaceService(nil)
	if _, err := removed.Get("platform"); !errors.Is(err, ErrNotFound) || len(removed.List()) != 0 {
		t.Fatal("removed namespace is selectable")
	}
}

func TestNamespaceRegistryRejectsInvalidDefinitions(t *testing.T) {
	for _, definitions := range [][]Namespace{
		{{ID: "bad/id", Clouds: []string{"main"}}},
		{{ID: "good"}},
		{{ID: "good", Clouds: []string{"main", "main"}}},
		{{ID: "good", Clouds: []string{"bad/cloud"}}},
		{{ID: "good", Clouds: []string{"main"}}, {ID: "good", Clouds: []string{"main"}}},
	} {
		if _, err := NewNamespaceService(definitions); err == nil {
			t.Fatalf("accepted invalid namespace definitions: %+v", definitions)
		}
	}
}
