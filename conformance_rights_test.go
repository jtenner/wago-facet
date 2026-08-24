//go:build linux && (amd64 || arm64) && !tinygo && !wago_guardpage

package facet

import (
	"encoding/json"
	"testing"
)

func TestFacetManifestRightsPreservesOmission(t *testing.T) {
	omitted, err := facetManifestRights(nil)
	if err != nil {
		t.Fatal(err)
	}
	if omitted != nil {
		t.Fatalf("omitted manifest rights became %#v; want nil", omitted)
	}

	empty, err := facetManifestRights([]string{})
	if err != nil {
		t.Fatal(err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("explicit empty manifest rights became %#v; want non-nil empty slice", empty)
	}

	raw, err := json.Marshal(facetPluginConfig{Preopens: []facetPluginPreopen{
		{Guest: "/default", Host: "/tmp/default", Rights: omitted},
		{Guest: "/zero", Host: "/tmp/zero", Rights: empty},
	}})
	if err != nil {
		t.Fatal(err)
	}

	var decoded pluginConfig
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Preopens) != 2 {
		t.Fatalf("decoded %d preopens; want 2", len(decoded.Preopens))
	}
	if decoded.Preopens[0].Rights != nil {
		t.Fatalf("omitted rights decoded as explicit grant: %#v", *decoded.Preopens[0].Rights)
	}
	if decoded.Preopens[1].Rights == nil || len(*decoded.Preopens[1].Rights) != 0 {
		t.Fatalf("explicit empty rights lost during adapter JSON: %#v", decoded.Preopens[1].Rights)
	}
}
