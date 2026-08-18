// Package register exposes the Facet provider catalog to generated Wago runtimes.
// Importing this package has no registration side effects.
package register

import (
	facet "github.com/jtenner/wago-facet"
	wago "github.com/wago-org/wago"
)

func Providers() []wago.PluginProvider {
	return []wago.PluginProvider{facet.Provider()}
}
