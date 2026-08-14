package golang

import "github.com/EzraStone/Lectio/internal/adapter"

// The Go adapter registers itself, so importing this package is what enables
// the language. Nothing else in the tree references it by name.
func init() { adapter.Register(New()) }

// Compile-time proof that the adapter satisfies the seam. If a method drifts,
// this is where it fails, rather than at a registration panic during a run.
var _ adapter.LanguageAdapter = (*Adapter)(nil)
var _ adapter.Configurable = (*Adapter)(nil)
