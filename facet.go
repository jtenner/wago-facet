// Package facet implements the Facet 0.1 system interface as an explicit Wago
// plugin. It exposes only imports that current Wago can implement exactly.
package facet

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	wago "github.com/wago-org/wago"
)

const (
	ID      = "github.com/jtenner/wago-facet"
	Module  = "facet"
	Version = "0.1.0"
)

const (
	CapCore            wago.Capability = "facet.core"
	CapArgumentsRead   wago.Capability = "facet.arguments.read"
	CapEnvironmentRead wago.Capability = "facet.environment.read"
	CapClockRead       wago.Capability = "facet.clock.read"
	CapRandomRead      wago.Capability = "facet.random.read"
	CapProcessExit     wago.Capability = "facet.process.exit"
	CapSchedulerYield  wago.Capability = "facet.scheduler.yield"
	CapStdinRead       wago.Capability = "facet.stdio.read"
	CapStdoutWrite     wago.Capability = "facet.stdio.write"
	CapFDManage        wago.Capability = "facet.fd.manage"
	CapFDRead          wago.Capability = "facet.fd.read"
	CapFDWrite         wago.Capability = "facet.fd.write"
	CapFilesystemRead  wago.Capability = "facet.filesystem.read"
	CapFilesystemWrite wago.Capability = "facet.filesystem.write"
	CapFilesystemOpen  wago.Capability = "facet.filesystem.open"
	CapNetwork         wago.Capability = "facet.network"
	CapPoll            wago.Capability = "facet.poll"
)

func Definition() wago.PluginDefinition {
	return wago.PluginDefinition{
		ID:          ID,
		Name:        "Facet",
		Version:     Version,
		Description: "Facet 0.1 portable system interface for Core WebAssembly.",
		Stability:   wago.Experimental,
		Compatibility: wago.Compatibility{
			Engines:   map[string]string{"wago": ">=0.1.0", "go": ">=1.22"},
			Platforms: []string{"linux/amd64", "linux/arm64"},
		},
		Provenance: wago.PluginProvenance{
			Homepage:   "https://github.com/jtenner/wago-facet",
			Repository: "https://github.com/jtenner/wago-facet",
			License:    "MIT",
			Authors:    []string{"Joshua Tenner"},
		},
		Authorities: []wago.AuthorityRequest{
			{Name: wago.AuthorityHostImportDefine, Mode: wago.AuthorityRequired, Reason: "define the Facet Core WebAssembly import module", Scope: wago.AuthorityScope{Modules: []string{Module}}},
			{Name: wago.AuthorityHostCallerIdentify, Mode: wago.AuthorityRequired, Reason: "isolate opaque Facet resource handles by guest instance"},
			{Name: wago.AuthorityHostArgumentsRead, Mode: wago.AuthorityRequired, Reason: "expose the runtime-scoped guest argument vector through Facet"},
			{Name: wago.AuthorityInstanceCloseObserve, Mode: wago.AuthorityRequired, Reason: "release Facet resources when their guest instance closes"},
			{Name: wago.AuthorityInstanceInstantiateIntercept, Mode: wago.AuthorityOptional, Reason: "validate caller-selected concrete GC-array result storage before instantiation"},
		},
		ConfigSchema: ConfigSchema(),
	}
}

func Provider() wago.PluginProvider {
	return wago.PluginProvider{Definition: Definition(), New: func() wago.Plugin { return &Plugin{} }, ValidateConfig: validatePluginConfig}
}

type Plugin struct {
	cfg        Config
	arguments  *wago.GuestArgumentsAccess
	callers    *wago.CallerResolver
	states     *stateStore
	raw        *instanceState
	preopenFDs []int
	clockBase  time.Time
}

func (p *Plugin) Register(reg *wago.Registrar) error {
	var cfg pluginConfig
	if err := reg.Config(&cfg); err != nil {
		return err
	}
	resolved, err := configFromPlugin(cfg)
	if err != nil {
		return err
	}
	p.cfg = resolved
	p.arguments, err = reg.GuestArguments()
	if err != nil {
		return err
	}
	p.callers, err = reg.HostCallers()
	if err != nil {
		return err
	}
	closed, err := reg.InstanceCloseObserver()
	if err != nil {
		return err
	}
	if err := closed.After(func(event wago.InstanceCloseEvent) {
		if p.states != nil {
			p.states.remove(event.Instance)
		}
	}); err != nil {
		return err
	}

	allocatingTextEnabled := reg.Granted(wago.AuthorityInstanceInstantiateIntercept)
	if allocatingTextEnabled {
		instantiate, err := reg.InstanceInstantiateInterceptor()
		if err != nil {
			return err
		}
		if err := instantiate.Before(func(request wago.InstantiationRequest) error {
			return validateAllAllocatingTextImports(request.Module)
		}); err != nil {
			return err
		}
	}

	imports, err := reg.HostImports()
	if err != nil {
		return err
	}
	module, err := imports.Module(Module)
	if err != nil {
		return err
	}
	for _, capability := range guestCapabilities {
		if err := reg.GuestCapability(capability.cap, wago.CapabilityDocs(capability.docs)); err != nil {
			return err
		}
	}
	allBindings := append(p.bindings(), p.guestStorageBindings()...)
	allBindings = append(allBindings, p.fdIOBindings()...)
	allBindings = append(allBindings, p.positionalBindings()...)
	allBindings = append(allBindings, p.vectoredBindings()...)
	allBindings = append(allBindings, p.pathBindings()...)
	allBindings = append(allBindings, p.linkBindings()...)
	allBindings = append(allBindings, p.datagramBindings()...)
	allBindings = append(allBindings, p.dnsBindings()...)
	if allocatingTextEnabled {
		allBindings = append(allBindings, p.allocatingTextBindings()...)
		allBindings = append(allBindings, p.allocatingReadlinkBindings()...)
	}
	for _, b := range allBindings {
		module.Func(b.name, b.fn).Params(b.params...).Results(b.results...).Capability(b.cap).Docs(b.docs)
	}
	return reg.Lifecycle(wago.PluginLifecycle{Start: p.start, Stop: p.stop})
}

func (p *Plugin) start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	args, err := p.arguments.Args()
	if err != nil {
		return err
	}
	p.cfg.Args = args
	preopenFDs, err := pinPreopens(p.cfg.Preopens)
	if err != nil {
		return err
	}
	p.preopenFDs = preopenFDs
	p.clockBase = time.Now()
	p.states = newStateStore(p.cfg, p.preopenFDs)
	return nil
}

func (p *Plugin) stop(context.Context) error {
	if p.states != nil {
		p.states.closeAll()
		p.states = nil
	}
	closePinnedPreopens(p.preopenFDs)
	p.preopenFDs = nil
	return nil
}

func (p *Plugin) stateFor(m wago.HostModule) *instanceState {
	if p.raw != nil {
		return p.raw
	}
	if p.callers == nil || p.states == nil {
		panic(wago.HostTrap{Err: fmt.Errorf("facet: plugin state is not active")})
	}
	id, err := p.callers.Resolve(m)
	if err != nil {
		panic(wago.HostTrap{Err: fmt.Errorf("facet: resolve active caller: %w", err)})
	}
	return p.states.get(id)
}

func (p *Plugin) monotonicNow() uint64 {
	base := p.clockBase
	if base.IsZero() {
		return 0
	}
	d := time.Since(base)
	if d < 0 {
		return 0
	}
	return uint64(d)
}

// Imports returns one low-level, single-instance Facet import map.
//
// Deprecated: use NewInstanceImports and call Close on the returned bundle.
// Imports cannot expose an ownership handle for host descriptors and other
// resources created by guest calls.
func Imports(cfg Config) wago.Imports {
	return mustLegacyImports(cfg)
}

func MarshalConfig(cfg any) (json.RawMessage, error) {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	if err := validatePluginConfig(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

type guestCapability struct {
	cap  wago.Capability
	docs string
}

var guestCapabilities = []guestCapability{
	{CapCore, "use scalar Facet core operations and opaque resource handles"},
	{CapArgumentsRead, "read runtime-scoped guest argument metadata"},
	{CapEnvironmentRead, "read configured guest environment metadata"},
	{CapClockRead, "read Facet system and monotonic clocks and sleep"},
	{CapRandomRead, "read cryptographic scalar randomness"},
	{CapProcessExit, "terminate the current guest invocation"},
	{CapSchedulerYield, "yield guest execution to the Go scheduler"},
	{CapStdinRead, "obtain the configured standard-input descriptor"},
	{CapStdoutWrite, "obtain configured standard-output and standard-error descriptors"},
	{CapFDManage, "inspect and manage Facet descriptor resources"},
	{CapFDRead, "read from an already-authorized Facet descriptor"},
	{CapFDWrite, "write to an already-authorized Facet descriptor"},
	{CapFilesystemRead, "inspect configured filesystem preopens and directory entries"},
	{CapFilesystemWrite, "perform filesystem namespace mutations permitted by configured rights"},
	{CapFilesystemOpen, "open filesystem resources; per-call Facet rights decide whether the open may mutate the namespace"},
	{CapNetwork, "create and manage IPv4 and IPv6 sockets"},
	{CapPoll, "wait for descriptor and monotonic-timer readiness"},
}

type binding struct {
	name            string
	fn              wago.HostFunc
	params, results []wago.ValType
	cap             wago.Capability
	docs            string
}
