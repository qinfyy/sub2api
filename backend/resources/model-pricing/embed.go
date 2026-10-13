// Package modelpricing contains the reviewed, offline fallback price catalog.
package modelpricing

import _ "embed"

//go:embed model_prices_and_context_window.json
var catalog string

func Catalog() []byte { return []byte(catalog) }
