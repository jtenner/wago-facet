package facet

import "testing"

func TestCompleteFacetImportInventory(t *testing.T) {
	p := &Plugin{}
	bindings := append([]binding(nil), p.bindings()...)
	bindings = append(bindings, p.guestStorageBindings()...)
	bindings = append(bindings, p.fdIOBindings()...)
	bindings = append(bindings, p.positionalBindings()...)
	bindings = append(bindings, p.vectoredBindings()...)
	bindings = append(bindings, p.pathBindings()...)
	bindings = append(bindings, p.linkBindings()...)
	bindings = append(bindings, p.datagramBindings()...)
	bindings = append(bindings, p.dnsBindings()...)
	bindings = append(bindings, p.allocatingTextBindings()...)
	bindings = append(bindings, p.allocatingReadlinkBindings()...)

	if len(bindings) != 261 {
		t.Fatalf("Facet binding count = %d, want canonical 261", len(bindings))
	}
	seen := make(map[string]struct{}, len(bindings))
	for _, b := range bindings {
		if b.name == "" || b.fn == nil {
			t.Fatalf("invalid binding: %#v", b)
		}
		if _, duplicate := seen[b.name]; duplicate {
			t.Fatalf("duplicate Facet import %q", b.name)
		}
		seen[b.name] = struct{}{}
	}
	for _, name := range []string{
		"abi_version",
		"args_read_array_i8",
		"random_fill_array_v128",
		"fd_pread_mem64",
		"fd_writev_array_i32",
		"path_open_array_i16",
		"path_rename_mem64_i32",
		"path_readlink_array_i8",
		"socket_recvfrom_array_v128",
		"dns_resolve_array_i32",
		"poll_wait",
	} {
		if _, ok := seen[name]; !ok {
			t.Fatalf("canonical Facet import %q is missing", name)
		}
	}
}
