package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCloudInitTemplateDirectoryConfiguration(t *testing.T) {
	for _, configured := range []string{"", "/var/lib/vmango/custom-templates"} {
		path := filepath.Join(t.TempDir(), "vmango.conf")
		body := ""
		if configured != "" {
			body = `cloud_init_templates_dir = "` + configured + `"`
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := Parse(path)
		if err != nil {
			t.Fatal(err)
		}
		want := configured
		if want == "" {
			want = "~/.vmango/cloud-init-templates"
		}
		if cfg.CloudInitTemplatesDir != want {
			t.Fatalf("directory = %q, want %q", cfg.CloudInitTemplatesDir, want)
		}
	}
}
