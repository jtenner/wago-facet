package facet

import (
	"sync"
	"time"

	wago "github.com/wago-org/wago"
)

// InstanceImports owns one low-level single-instance import map and every host
// resource that backs it. Close must be called when the guest instance is done.
// Provider remains the preferred integration because Wago drives its lifecycle.
type InstanceImports struct {
	Imports wago.Imports

	once       sync.Once
	state      *instanceState
	preopenFDs []int
}

// NewInstanceImports creates a closable low-level Facet import bundle.
// Callback-scoped indexed-memory and GC-array imports remain Provider-only.
func NewInstanceImports(cfg Config) (*InstanceImports, error) {
	cfg = normalizeConfig(cfg)
	preopenFDs, err := pinPreopens(cfg.Preopens)
	if err != nil {
		return nil, err
	}
	state := newInstanceState(cfg, preopenFDs)
	p := &Plugin{cfg: cfg, raw: state, clockBase: time.Now()}
	imports := make(wago.Imports)
	for _, b := range p.bindings() {
		imports[Module+"."+b.name] = b.fn
	}
	return &InstanceImports{Imports: imports, state: state, preopenFDs: preopenFDs}, nil
}

// Close releases all descriptors and other host resources owned by this bundle.
func (i *InstanceImports) Close() error {
	if i == nil {
		return nil
	}
	i.once.Do(func() {
		if i.state != nil {
			i.state.closeAll()
		}
		closePinnedPreopens(i.preopenFDs)
		i.preopenFDs = nil
	})
	return nil
}
