package config

import (
	"fmt"
	"github.com/hashicorp/hcl/hcl/ast"
	"github.com/hashicorp/hcl/hcl/token"
)

func validateCPUBlock(item *ast.ObjectItem, kind string) error {
	object, ok := item.Val.(*ast.ObjectType)
	if !ok || (kind == "feature" && len(item.Keys) != 2) || (kind != "feature" && len(item.Keys) != 1) {
		return fmt.Errorf("%s must be a %s block", kind, map[bool]string{true: "named", false: "single unnamed"}[kind == "feature"])
	}
	fields := map[string]map[string]token.Type{
		"cpu":       {"mode": token.STRING, "model": token.STRING, "vendor": token.STRING},
		"feature":   {"policy": token.STRING},
		"placement": {"mode": token.STRING, "memory_mode": token.STRING},
		"hyperv":    {"mode": token.STRING, "relaxed": token.BOOL, "vapic": token.BOOL, "vpindex": token.BOOL, "runtime": token.BOOL, "synic": token.BOOL, "reset": token.BOOL, "frequencies": token.BOOL, "tlbflush": token.BOOL, "ipi": token.BOOL},
		"spinlocks": {"enabled": token.BOOL, "retries": token.NUMBER},
		"stimer":    {"enabled": token.BOOL, "direct": token.BOOL},
		"kvm":       {"hidden": token.BOOL, "poll_control": token.BOOL, "pv_ipi": token.BOOL},
	}
	seen := map[string]bool{}
	for _, f := range object.List.Items {
		key := fmt.Sprint(f.Keys[0].Token.Value())
		nested := kind == "cpu" && key == "feature" || kind == "hyperv" && (key == "spinlocks" || key == "stimer")
		id := key
		if key == "feature" && len(f.Keys) == 2 {
			id += "." + fmt.Sprint(f.Keys[1].Token.Value())
		}
		if seen[id] {
			return fmt.Errorf("%s.%s is duplicated", kind, id)
		}
		seen[id] = true
		if nested {
			if err := validateCPUBlock(f, key); err != nil {
				return err
			}
			continue
		}
		typ, allowed := fields[kind][key]
		if !allowed || len(f.Keys) != 1 {
			return fmt.Errorf("%s: unsupported field %s", kind, key)
		}
		v, ok := f.Val.(*ast.LiteralType)
		if !ok || v.Token.Type != typ || typ == token.STRING && v.Token.Value() == "" {
			return fmt.Errorf("%s.%s has an invalid literal type/value", kind, key)
		}
	}
	return nil
}
