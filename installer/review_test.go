package installer

import (
	"bytes"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"subuk/vmango/config"
)

func TestReviewIPv6ConnectionWithoutExplicitPort(t *testing.T) {
	_, opts := newTestController(t, true, nil, nil)
	uri, err := ConnectionURI(opts.Document, Host{ID: "ipv6", Address: "2001:db8::1", User: "deploy"})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(uri)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Hostname() != "2001:db8::1" || parsed.Port() != "" {
		t.Fatalf("IPv6 connection changed target: hostname=%q port=%q", parsed.Hostname(), parsed.Port())
	}
}

func TestReviewPrivateLegacyURLStaysOutOfStateAndPreservesSource(t *testing.T) {
	for _, baseURL := range []string{
		"https://private-user:private-password@example.test/base?private-query=token#private-fragment",
		"https://example.test/%private-malformed-url",
	} {
		t.Run(strconv.Itoa(len(baseURL)), func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "vmango.conf")
			source := []byte("api { listen=\"127.0.0.1:8080\" base_url=" + strconv.Quote(baseURL) + " user \"owner\" { hashed_password=\"existing-private-hash\" } }\n")
			if err := os.WriteFile(path, source, 0600); err != nil {
				t.Fatal(err)
			}
			doc, err := config.OpenInstallDocument(path)
			if err != nil {
				t.Fatal(err)
			}
			opts := Options{Document: doc, StateDir: filepath.Join(root, "state"), Provisioner: &fakeProvisioner{}, Resources: func([]Host) Resources { return &fakeResources{} }}
			c, err := New(opts)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = c.Close() })
			checkSafe := func(value any) {
				t.Helper()
				data, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(data), "private-") {
					t.Fatal("private legacy configuration entered browser state")
				}
			}
			checkSafe(c.Snapshot())
			a := testAnswers(c)
			a.API = c.Snapshot().API
			if err := c.Answers(a); err != nil {
				t.Fatal(err)
			}
			applyStage(t, c, "hosts")
			applyStage(t, c, "resources")
			review := prepareStage(t, c, "config")
			checkSafe(review)
			data, err := os.ReadFile(filepath.Join(opts.StateDir, "session.json"))
			if err != nil {
				t.Fatal(err)
			}
			var recovered saved
			if err := json.Unmarshal(data, &recovered); err != nil {
				t.Fatal(err)
			}
			checkSafe(recovered.State)
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			c, err = New(opts)
			if err != nil {
				t.Fatal(err)
			}
			checkSafe(c.Snapshot())
			if err := c.Apply(review.Revision, review.Plan.ID); err != nil {
				t.Fatal(err)
			}
			if state := waitInstaller(t, c); state.Error != "" {
				t.Fatal(state.Error)
			}
			published, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(published, source) {
				t.Fatal("unchanged legacy API block was rewritten")
			}
		})
	}
}

func TestReviewRejectedAnswersRetainConfigurationCandidate(t *testing.T) {
	c, _ := newTestController(t, true, nil, nil)
	answers := testAnswers(c)
	if err := c.Answers(answers); err != nil {
		t.Fatal(err)
	}
	applyStage(t, c, "hosts")
	applyStage(t, c, "resources")
	review := prepareStage(t, c, "config")
	candidate := c.candidate
	answers = testAnswers(c)
	answers.Hosts[0].Cloud = "invalid/cloud"
	if err := c.Answers(answers); err == nil {
		t.Fatal("invalid cloud was accepted")
	}
	current := c.Snapshot()
	if current.Revision != review.Revision || current.Plan == nil || current.Plan.ID != review.Plan.ID {
		t.Fatal("rejected answers changed the reviewed configuration")
	}
	if c.candidate != candidate {
		t.Fatal("rejected answers discarded the reviewed configuration candidate")
	}
	if err := c.Apply(current.Revision, current.Plan.ID); err != nil {
		t.Fatalf("original reviewed configuration is no longer applicable: %v", err)
	}
	if state := waitInstaller(t, c); state.Error != "" {
		t.Fatal(state.Error)
	}
}

func TestReviewPersistenceFailureRetainsConfigurationCandidate(t *testing.T) {
	c, opts := newTestController(t, true, nil, nil)
	if err := c.Answers(testAnswers(c)); err != nil {
		t.Fatal(err)
	}
	applyStage(t, c, "hosts")
	applyStage(t, c, "resources")
	review := prepareStage(t, c, "config")
	candidate := c.candidate
	statePath := opts.StateDir + ".preserved"
	if err := os.Rename(opts.StateDir, statePath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(opts.StateDir, []byte("unavailable"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := c.Answers(testAnswers(c)); err == nil {
		t.Fatal("answers accepted despite unavailable persistence directory")
	}
	if err := os.Remove(opts.StateDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(statePath, opts.StateDir); err != nil {
		t.Fatal(err)
	}
	if c.candidate != candidate || c.Snapshot().Revision != review.Revision {
		t.Fatal("failed persistence discarded the reviewed configuration")
	}
	if err := c.Apply(review.Revision, review.Plan.ID); err != nil {
		t.Fatal(err)
	}
	if state := waitInstaller(t, c); state.Error != "" {
		t.Fatal(state.Error)
	}
}
