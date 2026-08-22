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
	// type 2: (func (result i32))
	callerType := []byte{0x60, 0x00, 0x01, 0x7f}
	importEntry := append(wasmtest.Name(Module), wasmtest.Name("args_read_array_i8")...)
	importEntry = append(importEntry, 0x00) // function import
	importEntry = append(importEntry, wasmtest.ULEB(1)...)
	// args_read_array_i8(0, strict) -> (array, errno); discard errno and read byte 0.
	body := []byte{
		0x41, 0x00, // i32.const 0: argument index
		0x41, 0x00, // i32.const 0: strict UTF mode
		0x10, 0x00, // call imported args_read_array_i8
		0x1a,       // drop errno
		0x41, 0x00, // i32.const 0
		0xfb, 0x0d, 0x00, // array.get_u type 0
		0x0b, // end
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(arrayType, funcType, callerType)),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
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
	values, err := inst.Call(context.Background(), "call")
	if err != nil {
		t.Fatalf("caller-typed i8 Facet call failed: %v", err)
	}
	if len(values) != 1 || values[0].I32() != int32('a') {
		t.Fatalf("caller-typed i8 Facet call = %v; want first argument byte %d", values, 'a')
	}
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
