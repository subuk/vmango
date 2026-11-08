package config

import (
	"strings"
	"testing"
)

func TestCPUConfigurationValidation(t *testing.T) {
	for _, fragment := range []string{
		`cpu { mode="custom" model="Skylake-Server-v4" vendor="Intel" feature "aes" {} feature "avx2" {policy="disable"} }`,
		`cpu {} placement {mode="auto"} kvm {hidden=false poll_control=true pv_ipi=false}`,
		`hyperv {vpindex=true synic=true spinlocks {retries=4294967295} stimer {direct=false}} timer "hypervclock" {present=true}`,
	} {
		cfg, err := parseCloudTest(t, strings.Replace(cloudConfig, `hardware_profile "default" {`, `hardware_profile "default" { `+fragment, 1))
		if err != nil {
			t.Fatal(fragment, err)
		}
		if _, err = cfg.HardwareProfiles(); err != nil {
			t.Fatal(err)
		}
	}
	for _, fragment := range []string{
		`hyperv {spinlocks {retries=-1}}`, `hyperv {spinlocks {retries=4094}}`, `hyperv {spinlocks {retries=4294967296}}`, `cpu {topology {sockets=1}}`, `cpu {mode="maximum" mode="maximum"}`, `cpu {feature "aes" {} feature "aes" {}}`, `cpu {feature "aes" {policy="optional"}}`, `cpu {vendor_id="bad"}`,
		`placement {mode="static" memory_mode="strict"}`, `placement {mode="auto"} placement {}`, `hyperv {mode="host-model" relaxed=false}`, `hyperv {hint_dedicated=true}`, `hyperv {spinlocks {enabled=false retries=4096}}`, `hyperv {stimer {}}`, `kvm {dirty_ring=true}`, `kvm {hidden="false"}`, `cpu {mode=""}`, `cpu {feature "aes" {policy=""}}`,
	} {
		_, err := parseCloudTest(t, strings.Replace(cloudConfig, `hardware_profile "default" {`, `hardware_profile "default" { `+fragment, 1))
		if err == nil {
			t.Fatalf("accepted %s", fragment)
		}
	}
}
