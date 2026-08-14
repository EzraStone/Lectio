package probe

import (
	"sort"
	"strings"

	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/index"
)

// Context is everything generation and grading need, assembled once.
type Context struct {
	View  *index.View
	Stems StemWriter

	// byName resolves what a person typed onto real symbols.
	byName map[string][]core.SymbolID
}

// NewContext builds a probe context over an index view.
func NewContext(v *index.View, stems StemWriter) *Context {
	if stems == nil {
		stems = TemplateStems{}
	}
	c := &Context{View: v, Stems: stems, byName: make(map[string][]core.SymbolID, len(v.Symbols)*3)}

	for id, sym := range v.Symbols {
		c.add(string(id), id)
		c.add(id.Short(), id)
		c.add(sym.Name, id)
		// Method names are frequently given bare: someone answering "what
		// breaks" will write "Next", not "scheduler.(*Scheduler).Next".
		if i := strings.LastIndex(sym.Name, "."); i >= 0 {
			c.add(sym.Name[i+1:], id)
		}
	}
	return c
}

func (c *Context) add(key string, id core.SymbolID) {
	key = normalize(key)
	if key == "" {
		return
	}
	for _, existing := range c.byName[key] {
		if existing == id {
			return
		}
	}
	c.byName[key] = append(c.byName[key], id)
}

func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(strings.Trim(s, "(),.;\"'`")))
}

// Resolve maps one answer token onto the symbols it could mean.
//
// People answer in whatever form is shortest — "Cycle", "billing.Cycle", or
// the file. Demanding a canonical identifier would measure typing accuracy
// rather than understanding, and the person answering has been in the codebase
// four days.
//
// Ambiguity resolves in the answerer's favor: a bare name matching three
// symbols counts as naming any of them. The alternative punishes someone for
// a naming collision they did not create and probably have not noticed.
func (c *Context) Resolve(token string) []core.SymbolID {
	key := normalize(token)
	if key == "" {
		return nil
	}
	if ids, ok := c.byName[key]; ok {
		return ids
	}

	// Fall back to a suffix match, so "Scheduler.Next" finds
	// "(*Scheduler).Next" without the punctuation having to line up.
	var out []core.SymbolID
	for name, ids := range c.byName {
		if strings.HasSuffix(name, "."+key) || strings.HasSuffix(key, "."+name) {
			out = append(out, ids...)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// ReadableSymbols returns production symbols in deterministic order, which is
// the candidate pool every generator draws from.
func (c *Context) ReadableSymbols() []core.Symbol {
	return c.View.Readable()
}

// symbolLabel renders a symbol for display in a choice or an answer key.
func symbolLabel(sym core.Symbol) string {
	return sym.ID.Short()
}
