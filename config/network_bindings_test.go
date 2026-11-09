package config

import (
	"strings"
	"subuk/vmango/compute"
	"testing"
)

func TestExplicitNetworkBindings(t *testing.T) {
	for _, tc := range []struct{ body, kind, target string }{
		{`network="default"`, compute.NetworkLibvirt, "default"},
		{`network="qemu-usernet"`, compute.NetworkLibvirt, "qemu-usernet"},
		{`type="bridge" bridge="br0"`, compute.NetworkBridge, "br0"},
		{`type="qemu-user"`, compute.NetworkUser, ""},
	} {
		cfg, err := ParseBytes([]byte(strings.Replace(cloudConfig, `network="default"`, tc.body, 1)))
		if err != nil {
			t.Fatal(tc.body, err)
		}
		clouds, err := cfg.CloudDefinitions()
		if err != nil {
			t.Fatal(err)
		}
		b := clouds[0].Hosts[0].Networks["lan"]
		if b.Kind() != tc.kind || b.Target() != tc.target {
			t.Fatal(b)
		}
	}
	for _, body := range []string{`bridge="br0"`, `type="bridge" network="default"`, `type="qemu-user" network="default"`, `type="qemu-user" network=""`, `type="bridge" bridge="br0" socket=""`, `type="qemu-user" subnet=""`, `type="invalid"`, `type="libvirt-network"`, `network="default" model=""`, `network="default" vlan=0`} {
		if _, err := ParseBytes([]byte(strings.Replace(cloudConfig, `network="default"`, body, 1))); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}
