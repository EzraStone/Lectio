package adapter

import (
	"fmt"
	"sort"
	"sync"
)

var (
	mu         sync.RWMutex
	registered []LanguageAdapter
)

// Register adds an adapter to the global set consulted by Select. Adapters
// register from their package's init, so importing an adapter package is what
// enables the language.
func Register(a LanguageAdapter) {
	mu.Lock()
	defer mu.Unlock()
	for _, existing := range registered {
		if existing.Name() == a.Name() {
			panic(fmt.Sprintf("adapter: %q registered twice", a.Name()))
		}
	}
	registered = append(registered, a)
}

// Registered returns the adapters currently available, sorted by name.
func Registered() []LanguageAdapter {
	mu.RLock()
	defer mu.RUnlock()
	out := append([]LanguageAdapter(nil), registered...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// Select picks the adapter most confident about root.
//
// Polyglot repos are the common case, not the exception, so this returns one
// adapter rather than erroring on ambiguity — a Go repo with a JS frontend
// should still index its Go.
func Select(root string) (LanguageAdapter, error) {
	mu.RLock()
	defer mu.RUnlock()

	var best LanguageAdapter
	var bestConf float64
	for _, a := range registered {
		ok, conf := a.Detect(root)
		if !ok {
			continue
		}
		if best == nil || conf > bestConf {
			best, bestConf = a, conf
		}
	}
	if best == nil {
		return nil, fmt.Errorf("no language adapter recognizes %s (registered: %v)", root, names())
	}
	return best, nil
}

// ByName returns a specific adapter, for when the user overrides detection.
func ByName(name string) (LanguageAdapter, error) {
	mu.RLock()
	defer mu.RUnlock()
	for _, a := range registered {
		if a.Name() == name {
			return a, nil
		}
	}
	return nil, fmt.Errorf("unknown adapter %q (registered: %v)", name, names())
}

// names lists registered adapter names. Callers must hold mu.
func names() []string {
	out := make([]string, 0, len(registered))
	for _, a := range registered {
		out = append(out, a.Name())
	}
	sort.Strings(out)
	return out
}
