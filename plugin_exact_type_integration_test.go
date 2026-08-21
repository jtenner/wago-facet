//go:build linux && amd64 && !tinygo && !wago_guardpage

package facet

import (
	"context"
	"testing"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/tests/wasmtest"
)

func facetPluginSetForTest(t *testing.T) wago.PluginSet {
	t.Helper()
	provider := Provider()
	digest, err := wago.DefinitionDigest(provider.Definition)
	if err != nil {
		t.Fatal(err)
	}
	grants := make([]wago.AuthorityGrant, 0, len(provider.Definition.Authorities))
	for _, req := range provider.Definition.Authorities {
		grants = append(grants, wago.AuthorityGrant{Name: req.Name, Scope: req.Scope})
	}
	return wago.PluginSet{
		Providers: []wago.PluginProvider{provider},
		Selections: []wago.PluginSelection{{
			ID:               provider.Definition.ID,
			DefinitionDigest: digest,
			Direct:           true,
			Dependencies:     map[string]string{},
			Grants:           grants,
			Config:           []byte(`{}`),
		}},
	}
}

func callerTypedArrayImportModule(storage byte) []byte {
	// type 0: (array (mut <storage>))
	arrayType := []byte{0x5e, storage, 0x01}
	// type 1: (func (param i32 i32) (result (ref null 0) i32))
	funcType := []byte{0x60, 0x02, 0x7f, 0x7f, 0x02, 0x63, 0x00, 0x7f}
	importEntry := append(wasmtest.Name(Module), wasmtest.Name("args_read_array_i8")...)
	importEntry = append(importEntry, 0x00) // function import
	importEntry = append(importEntry, wasmtest.ULEB(1)...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(arrayType, funcType)),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
	)
}

func newFacetIntegrationRuntime(t *testing.T) *wago.Runtime {
	t.Helper()
	cfg := wago.NewRuntimeConfig().WithCoreFeatures(wago.CoreFeaturesV3)
	rt := wago.NewRuntime(wago.WithRuntimeConfig(cfg), wago.WithGuestArguments([]string{"alpha"}))
	if err := rt.LoadPlugins(context.Background(), facetPluginSetForTest(t)); err != nil {
		rt.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	return rt
}

func TestCallerTypedArrayImportInstantiatesWithMatchingStorage(t *testing.T) {
	rt := newFacetIntegrationRuntime(t)
	// Packed i8 storage type is encoded as 0x78.
	mod, err := rt.Compile(callerTypedArrayImportModule(0x78))
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	inst, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatalf("matching caller-typed i8 import failed instantiation: %v", err)
	}
	defer inst.Close()
}

func TestCallerTypedArrayImportRejectsWrongStorageBeforeStart(t *testing.T) {
	rt := newFacetIntegrationRuntime(t)
	// Packed i16 storage type is encoded as 0x77, but the imported Facet name
	// requires caller-selected i8 storage.
	mod, err := rt.Compile(callerTypedArrayImportModule(0x77))
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	if inst, err := rt.Instantiate(context.Background(), mod); err == nil {
		inst.Close()
		t.Fatal("wrong caller-selected storage type unexpectedly instantiated")
	}
}
