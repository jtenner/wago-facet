package facet

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	wago "github.com/wago-org/wago"
)

type canonicalSignature struct {
	params  []string
	results []string
}

func TestCanonicalFacetInventory(t *testing.T) {
	specDir := os.Getenv("FACET_SPEC_DIR")
	if specDir == "" {
		t.Skip("FACET_SPEC_DIR is set by the conformance gate")
	}
	raw, err := os.ReadFile(filepath.Join(specDir, "spec", "imports.wat"))
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := parseCanonicalFacetImports(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(canonical) != 261 {
		t.Fatalf("canonical Facet inventory contains %d imports, want 261", len(canonical))
	}

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

	if Module != "facet" {
		t.Fatalf("implementation module = %q, want facet", Module)
	}
	if len(bindings) != len(canonical) {
		t.Fatalf("implementation contains %d bindings, canonical inventory contains %d", len(bindings), len(canonical))
	}
	seen := make(map[string]struct{}, len(bindings))
	for _, b := range bindings {
		want, ok := canonical[b.name]
		if !ok {
			t.Errorf("implementation registers non-canonical import facet.%s", b.name)
			continue
		}
		if _, duplicate := seen[b.name]; duplicate {
			t.Errorf("implementation registers duplicate import facet.%s", b.name)
			continue
		}
		seen[b.name] = struct{}{}
		gotParams, err := canonicalizeValTypes(b.params)
		if err != nil {
			t.Errorf("facet.%s parameters: %v", b.name, err)
			continue
		}
		gotResults, err := canonicalizeValTypes(b.results)
		if err != nil {
			t.Errorf("facet.%s results: %v", b.name, err)
			continue
		}
		if strings.Join(gotParams, ",") != strings.Join(want.params, ",") {
			t.Errorf("facet.%s parameters = %v, want %v", b.name, gotParams, want.params)
		}
		if strings.Join(gotResults, ",") != strings.Join(want.results, ",") {
			t.Errorf("facet.%s results = %v, want %v", b.name, gotResults, want.results)
		}
	}
	for name := range canonical {
		if _, ok := seen[name]; !ok {
			t.Errorf("canonical import facet.%s is not registered", name)
		}
	}
}

func canonicalizeValTypes(types []wago.ValType) ([]string, error) {
	out := make([]string, 0, len(types))
	for _, typ := range types {
		switch typ {
		case wago.ValI32:
			out = append(out, "i32")
		case wago.ValI64:
			out = append(out, "i64")
		case wago.ValF32:
			out = append(out, "f32")
		case wago.ValF64:
			out = append(out, "f64")
		case wago.ValV128:
			out = append(out, "v128")
		case wago.ValFuncRef, wago.ValExternRef, wago.ValExnRef, wago.ValAnyRef, wago.ValI31Ref:
			out = append(out, "ref")
		default:
			return nil, fmt.Errorf("unsupported Wago value type %v", typ)
		}
	}
	return out, nil
}

func parseCanonicalFacetImports(src string) (map[string]canonicalSignature, error) {
	const prefix = `(import "facet" "`
	out := make(map[string]canonicalSignature)
	for at := 0; ; {
		rel := strings.Index(src[at:], prefix)
		if rel < 0 {
			break
		}
		start := at + rel
		end, err := matchingSExpr(src, start)
		if err != nil {
			return nil, err
		}
		nameStart := start + len(prefix)
		nameEndRel := strings.IndexByte(src[nameStart:end], '"')
		if nameEndRel < 0 {
			return nil, fmt.Errorf("unterminated Facet import name at byte %d", start)
		}
		name := src[nameStart : nameStart+nameEndRel]
		form := src[start : end+1]
		sig := canonicalSignature{
			params:  extractCanonicalGroups(form, "param"),
			results: extractCanonicalGroups(form, "result"),
		}
		if _, duplicate := out[name]; duplicate {
			return nil, fmt.Errorf("duplicate canonical Facet import %q", name)
		}
		out[name] = sig
		at = end + 1
	}
	return out, nil
}

func extractCanonicalGroups(form, keyword string) []string {
	needle := "(" + keyword
	var out []string
	for at := 0; ; {
		rel := strings.Index(form[at:], needle)
		if rel < 0 {
			break
		}
		start := at + rel
		end, err := matchingSExpr(form, start)
		if err != nil {
			return append(out, "<malformed>")
		}
		inside := form[start+len(needle) : end]
		out = append(out, parseCanonicalTypeList(inside)...)
		at = end + 1
	}
	return out
}

func parseCanonicalTypeList(src string) []string {
	var out []string
	for i := 0; i < len(src); {
		for i < len(src) && (src[i] == ' ' || src[i] == '\t' || src[i] == '\n' || src[i] == '\r') {
			i++
		}
		if i >= len(src) {
			break
		}
		if src[i] == '(' {
			end, err := matchingSExpr(src, i)
			if err != nil {
				return append(out, "<malformed>")
			}
			expr := strings.TrimSpace(src[i+1 : end])
			if strings.HasPrefix(expr, "ref ") || expr == "ref" {
				out = append(out, "ref")
			}
			i = end + 1
			continue
		}
		start := i
		for i < len(src) && src[i] != ' ' && src[i] != '\t' && src[i] != '\n' && src[i] != '\r' && src[i] != ')' {
			i++
		}
		token := src[start:i]
		switch token {
		case "i32", "i64", "f32", "f64", "v128":
			out = append(out, token)
		case "funcref", "externref", "anyref", "eqref", "i31ref", "exnref":
			out = append(out, "ref")
		}
	}
	return out
}

func matchingSExpr(src string, start int) (int, error) {
	if start < 0 || start >= len(src) || src[start] != '(' {
		return 0, fmt.Errorf("expected s-expression at byte %d", start)
	}
	depth := 0
	quoted := false
	escaped := false
	for i := start; i < len(src); i++ {
		ch := src[i]
		if quoted {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				quoted = false
			}
			continue
		}
		switch ch {
		case '"':
			quoted = true
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i, nil
			}
		}
	}
	return 0, fmt.Errorf("unterminated s-expression at byte %d", start)
}
