package config

import (
	"fmt"
	"github.com/hashicorp/hcl/hcl/ast"
	"github.com/hashicorp/hcl/hcl/token"
)

func validateGuestSystemBlock(item *ast.ObjectItem, kind string) error {
	object, ok := item.Val.(*ast.ObjectType)
	if !ok || (kind != "timer" && len(item.Keys) != 1) || (kind == "timer" && len(item.Keys) != 2) {
		return fmt.Errorf("%s must be %s block", kind, map[bool]string{true: "a named", false: "one unnamed"}[kind == "timer"])
	}
	allowed := map[string]map[string]bool{
		"smbios":    {"bios": true, "system": true, "baseboard": true},
		"bios":      {"vendor": true, "version": true, "date": true, "release": true},
		"system":    {"manufacturer": true, "product": true, "version": true, "sku": true, "family": true},
		"baseboard": {"manufacturer": true, "product": true, "version": true},
		"clock":     {"mode": true, "timezone": true},
		"timer":     {"tickpolicy": true, "present": true},
	}
	seen := map[string]bool{}
	for _, field := range object.List.Items {
		if len(field.Keys) != 1 {
			return fmt.Errorf("%s requires unnamed fields", kind)
		}
		key := fmt.Sprint(field.Keys[0].Token.Value())
		if !allowed[kind][key] {
			return fmt.Errorf("%s: unsupported field %s", kind, key)
		}
		if seen[key] {
			return fmt.Errorf("%s.%s may be declared only once", kind, key)
		}
		seen[key] = true
		if kind == "smbios" {
			if err := validateGuestSystemBlock(field, key); err != nil {
				return err
			}
			continue
		}
		value, ok := field.Val.(*ast.LiteralType)
		if !ok {
			return fmt.Errorf("%s.%s must be a literal value", kind, key)
		}
		if key == "present" {
			if value.Token.Type != token.BOOL {
				return fmt.Errorf("timer.present must be a boolean")
			}
			continue
		}
		if value.Token.Type != token.STRING || value.Token.Value() == "" {
			return fmt.Errorf("%s.%s must be a nonempty string", kind, key)
		}
	}
	return nil
}
