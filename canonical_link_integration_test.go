//go:build linux && (amd64 || arm64) && !tinygo && !wago_guardpage

package facet

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestCanonicalFacetModuleInstantiates is the reverse half of the canonical
// inventory gate. The inventory test proves that every registered host import
// has a canonical name and public ABI category. This test takes the normative
// spec/imports.wat module itself, compiles it with wasm-tools, and instantiates
// all 261 canonical imports through the real Wago plugin. The required
// pre-instantiation interceptor therefore also checks the exact structural GC
// reference types for the complete import surface before this test can pass.
func TestCanonicalFacetModuleInstantiates(t *testing.T) {
	specDir := os.Getenv(facetSpecDirEnv)
	if specDir == "" {
		t.Skip("set FACET_SPEC_DIR to the pinned facet-spec checkout")
	}
	tool, err := exec.LookPath("wasm-tools")
	if err != nil {
		t.Fatal("wasm-tools is required")
	}
	wat := filepath.Join(specDir, "spec", "imports.wat")
	wasmPath := filepath.Join(t.TempDir(), "facet-imports.wasm")
	if output, err := exec.Command(tool, "parse", wat, "-o", wasmPath).CombinedOutput(); err != nil {
		t.Fatalf("parse canonical Facet imports: %s", firstFacetLine(output))
	}
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatal(err)
	}

	rt := newFacetIntegrationRuntime(t)
	mod, err := rt.Compile(wasmBytes)
	if err != nil {
		t.Fatalf("compile canonical Facet import module: %v", err)
	}
	defer mod.Close()
	inst, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatalf("instantiate complete canonical Facet import module: %v", err)
	}
	defer inst.Close()
}
