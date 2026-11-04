package config

import "testing"

func TestNamespaceConfiguration(t *testing.T) {
	cfg, err := parseCloudTest(t, cloudConfig+`
namespace "platform" { description = "Shared workloads" clouds = ["test"] }
namespace "analytics" { name = "Analytics" clouds = ["test"] }
namespace_directory = "/var/lib/vmango/namespaces"
`)
	if err != nil {
		t.Fatal(err)
	}
	definitions, err := cfg.NamespaceDefinitions()
	if err != nil || len(definitions) != 2 || definitions[0].Name != "platform" || definitions[0].Description != "Shared workloads" || definitions[1].Name != "Analytics" || cfg.NamespaceDirectory != "/var/lib/vmango/namespaces" {
		t.Fatalf("invalid definitions: %+v %v", definitions, err)
	}
	for _, invalid := range []string{
		`namespace "bad/id" { clouds = ["test"] }`,
		`namespace "platform" { clouds = [] }`,
		`namespace "platform" { clouds = ["test", "test"] }`,
		`namespace "platform" { clouds = ["unknown"] }`,
		`namespace "platform" { clouds = ["test"] } namespace "platform" { clouds = ["test"] }`,
	} {
		if _, err := parseCloudTest(t, cloudConfig+invalid); err == nil {
			t.Fatalf("accepted invalid namespace configuration: %s", invalid)
		}
	}
	// Discovery and removal use accepted definitions without consulting hosts.
	cfg, err = parseCloudTest(t, "")
	if err != nil {
		t.Fatal(err)
	}
	if definitions, err := cfg.NamespaceDefinitions(); err != nil || len(definitions) != 0 {
		t.Fatalf("empty configuration created an implicit namespace: %+v %v", definitions, err)
	}
}
