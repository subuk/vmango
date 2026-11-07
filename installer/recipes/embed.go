// Package recipes retains the reviewed source bytes for immutable publication.
package recipes

import "embed"

//go:embed *.yaml
var Sources embed.FS
