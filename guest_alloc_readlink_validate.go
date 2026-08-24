package facet

import (
	"fmt"

	wago "github.com/wago-org/wago"
)

func allocatingReadlinkWidth(name string) (textWidth, bool) {
	switch name {
	case "path_readlink_array_i8":
		return textI8, true
	case "path_readlink_array_i16":
		return textI16, true
	case "path_readlink_array_i32":
		return textI32, true
	default:
		return 0, false
	}
}

func validateAllocatingReadlinkImports(module wago.ModuleView) error {
	for _, imp := range module.Imports() {
		if imp.Kind != wago.ImportFunc || imp.Module != Module {
			continue
		}
		width, relevant := allocatingReadlinkWidth(imp.Name)
		if !relevant {
			continue
		}
		if len(imp.ResultTypes) == 0 {
			return fmt.Errorf("facet: import %s.%s has no exact result type", imp.Module, imp.Name)
		}
		result := imp.ResultTypes[0]
		if result.Kind != wago.ValueTypeReference || !result.Ref.Nullable || !result.Ref.Heap.Defined {
			return fmt.Errorf("facet: import %s.%s must return a nullable caller-defined GC array", imp.Module, imp.Name)
		}
		defined, ok := module.DefinedType(result.Ref.Heap.TypeIndex)
		if !ok {
			return fmt.Errorf("facet: import %s.%s result type %d is unavailable", imp.Module, imp.Name, result.Ref.Heap.TypeIndex)
		}
		if !definedTextArrayMatches(defined, width) {
			return fmt.Errorf("facet: import %s.%s caller result type has the wrong array storage class", imp.Module, imp.Name)
		}
	}
	return nil
}

func validateAllAllocatingTextImports(module wago.ModuleView) error {
	if err := validateAllocatingTextImports(module); err != nil {
		return err
	}
	return validateAllocatingReadlinkImports(module)
}
