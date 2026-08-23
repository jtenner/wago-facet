package facet

import (
	"fmt"

	wago "github.com/wago-org/wago"
)

// validateFacetImportSignatures closes the gap between Wago's compact host
// registration ABI and Facet's structural GC-reference signatures. Wago checks
// every scalar ABI category before instantiation; Facet additionally requires
// each ValAnyRef parameter to be exactly the canonical non-null abstract array
// reference. Caller-allocated array results are the intentional exception: the
// importing module selects a concrete nullable array type with the required
// element storage class.
func validateFacetImportSignatures(module wago.ModuleView) error {
	for _, imp := range module.Imports() {
		if imp.Kind != wago.ImportFunc || imp.Module != Module {
			continue
		}
		for i, typ := range imp.Params {
			if typ != wago.ValAnyRef {
				continue
			}
			if i >= len(imp.ParamTypes) {
				return fmt.Errorf("facet: import %s.%s parameter %d has no exact structural type", imp.Module, imp.Name, i)
			}
			if !canonicalFacetArrayParameter(imp.ParamTypes[i]) {
				return fmt.Errorf("facet: import %s.%s parameter %d must be the canonical non-null (ref array) type", imp.Module, imp.Name, i)
			}
		}
		for i, typ := range imp.Results {
			if typ != wago.ValAnyRef {
				continue
			}
			if i >= len(imp.ResultTypes) {
				return fmt.Errorf("facet: import %s.%s result %d has no exact structural type", imp.Module, imp.Name, i)
			}
			if i != 0 || !callerAllocatedArrayResult(imp.Name) {
				return fmt.Errorf("facet: import %s.%s has a non-canonical GC-reference result at index %d", imp.Module, imp.Name, i)
			}
		}
	}
	return validateAllAllocatingTextImports(module)
}

func canonicalFacetArrayParameter(typ wago.ValueTypeDescriptor) bool {
	return typ.Kind == wago.ValueTypeReference &&
		!typ.Ref.Nullable &&
		!typ.Ref.Exact &&
		!typ.Ref.Heap.Defined &&
		typ.Ref.Heap.Abstract == wago.AbstractHeapArray
}

func callerAllocatedArrayResult(name string) bool {
	if _, ok := allocatingTextWidth(name); ok {
		return true
	}
	_, ok := allocatingReadlinkWidth(name)
	return ok
}
