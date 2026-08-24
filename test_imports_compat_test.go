package facet

import (
	"os"
	"sync"
	"testing"

	wago "github.com/wago-org/wago"
)

var (
	testImportBundlesMu sync.Mutex
	testImportBundles   []*InstanceImports
)

// Imports preserves the historical test fixture shape without restoring the
// ownership-free production API. Every bundle is retained and deterministically
// closed by TestMain after the package test suite finishes.
func Imports(cfg Config) wago.Imports {
	bundle, err := NewInstanceImports(cfg)
	if err != nil {
		panic(err)
	}
	testImportBundlesMu.Lock()
	testImportBundles = append(testImportBundles, bundle)
	testImportBundlesMu.Unlock()
	return bundle.Imports
}

func TestMain(m *testing.M) {
	code := m.Run()
	testImportBundlesMu.Lock()
	bundles := testImportBundles
	testImportBundles = nil
	testImportBundlesMu.Unlock()
	for _, bundle := range bundles {
		_ = bundle.Close()
	}
	os.Exit(code)
}
