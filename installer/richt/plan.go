package richt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func (c *Client) Prepare(ctx context.Context, targets []Target, log func(string)) (*Plan, error) {
	if len(targets) == 0 || len(targets) > 32 {
		return nil, errors.New("select between 1 and 32 hosts")
	}
	inv, pins, err := inventory(targets)
	if err != nil {
		return nil, err
	}
	dir, err := c.newDirectory("plan")
	if err != nil {
		return nil, err
	}
	invPath := filepath.Join(dir, "inventory.yaml")
	if err := writeYAML(invPath, inv); err != nil {
		return nil, err
	}
	p := &Plan{ID: filepath.Base(dir), Path: filepath.Join(dir, "plan.json"), Version: PlanVersion, BinaryDigest: c.binaryDigest, ExecutableVersion: c.executableVersion, RecipeRevision: RecipeRevision, RecipeDigests: pins}
	for _, t := range targets {
		p.Hosts = append(p.Hosts, t.ID)
	}
	out, err := c.run(ctx, nil, log, "diff", "-i", invPath, "--refresh-facts", "--no-facts-cache", "--progress=plain", "-o", p.Path)
	if err != nil {
		return nil, err
	}
	p.Summary = safeText(string(out))
	p.Digest, _, err = readPlan(p.Path)
	if err != nil {
		return nil, err
	}
	if err := c.VerifyPlan(p); err != nil {
		return nil, err
	}
	return p, nil
}

func (c *Client) VerifyPlan(p *Plan) error {
	if p == nil || p.Version != PlanVersion || p.BinaryDigest != c.binaryDigest {
		return errors.New("saved installation requires the exact Richt executable used during preparation")
	}
	if err := verifyExecutable(c.binary, c.binaryDigest); err != nil {
		return err
	}
	abs, err := filepath.Abs(p.Path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(c.stateDir, abs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return errors.New("saved plan is outside the private installation directory")
	}
	resolved, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return err
	}
	state, err := filepath.EvalSymlinks(c.stateDir)
	if err != nil {
		return err
	}
	r, err := filepath.Rel(state, resolved)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(os.PathSeparator)) {
		return errors.New("saved plan directory escapes the installation directory")
	}
	sum, hosts, err := readPlan(abs)
	if err != nil {
		return err
	}
	if sum != p.Digest {
		return errors.New("saved Richt plan changed after review; prepare and approve a new plan")
	}
	if len(hosts) != len(p.Hosts) {
		return errors.New("saved plan host selection changed")
	}
	for i, h := range hosts {
		if h.Name != p.Hosts[i] {
			return errors.New("saved plan host selection changed")
		}
	}
	return nil
}

func (c *Client) Apply(ctx context.Context, p *Plan, log func(string)) error {
	if err := c.VerifyPlan(p); err != nil {
		return err
	}
	out, err := c.run(ctx, nil, log, "apply", "-y", "--progress=plain", fmt.Sprintf("--parallelism=%d", c.parallelism), p.Path)
	if log != nil && len(out) > 0 {
		log(safeText(string(out)))
	}
	if verifyErr := c.VerifyPlan(p); verifyErr != nil {
		return verifyErr
	}
	if err == nil {
		outcomes, outcomeErr := c.Outcomes(p)
		if outcomeErr != nil {
			return outcomeErr
		}
		for _, state := range outcomes {
			if state != "success" {
				return errors.New("Richt exited without a successful final outcome for every selected host")
			}
		}
	}
	return err
}

type savedHost struct {
	Name  string `json:"name"`
	Tasks []struct {
		Progress struct {
			Status string `json:"status"`
		} `json:"progress"`
	} `json:"tasks"`
}

// Outcomes returns only safe status values; task diagnostics and executable
// inputs remain private. An unfinished operation requires re-observation.
func (c *Client) Outcomes(p *Plan) (map[string]string, error) {
	if err := c.VerifyPlan(p); err != nil {
		return nil, err
	}
	_, hosts, err := readPlan(p.Path)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, h := range hosts {
		state := "success"
		for _, t := range h.Tasks {
			switch t.Progress.Status {
			case "success":
			case "error", "failed":
				state = "failed"
			default:
				if state != "failed" {
					state = "pending"
				}
			}
		}
		out[h.Name] = state
	}
	return out, nil
}

func readPlan(path string) (string, []savedHost, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return "", nil, err
	}
	if !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 || st.Size() > 32<<20 {
		return "", nil, errors.New("saved plan must be a private regular file of at most 32 MiB")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (32<<20)+1))
	if err != nil {
		return "", nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return "", nil, errors.New("invalid saved Richt plan")
	}
	if len(raw) != 2 {
		return "", nil, errors.New("unsupported saved Richt plan fields")
	}
	var version string
	if json.Unmarshal(raw["version"], &version) != nil || version != PlanVersion {
		return "", nil, errors.New("unsupported saved Richt plan version")
	}
	var hosts []map[string]json.RawMessage
	if json.Unmarshal(raw["hosts"], &hosts) != nil {
		return "", nil, errors.New("invalid saved Richt hosts")
	}
	var statuses []savedHost
	if json.Unmarshal(raw["hosts"], &statuses) != nil {
		return "", nil, errors.New("invalid saved Richt progress")
	}
	// Only this exact schema location is mutable. A field named progress inside
	// an executable operation remains included in the approved input digest.
	for _, h := range hosts {
		var tasks []map[string]json.RawMessage
		if json.Unmarshal(h["tasks"], &tasks) != nil {
			return "", nil, errors.New("invalid saved Richt tasks")
		}
		for _, t := range tasks {
			delete(t, "progress")
		}
		h["tasks"], err = json.Marshal(tasks)
		if err != nil {
			return "", nil, err
		}
	}
	raw["hosts"], err = json.Marshal(hosts)
	if err != nil {
		return "", nil, err
	}
	// Decode/re-encode numbers without float64 conversion. Whitespace and map
	// ordering must not make Richt's normal progress persistence invalidate review.
	normal, err := json.Marshal(raw)
	if err != nil {
		return "", nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(normal))
	dec.UseNumber()
	var canonical any
	if err := dec.Decode(&canonical); err != nil {
		return "", nil, err
	}
	normal, err = json.Marshal(canonical)
	if err != nil {
		return "", nil, err
	}
	return digest(normal), statuses, nil
}
