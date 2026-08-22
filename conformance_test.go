//go:build linux && amd64 && !tinygo && !wago_guardpage

package facet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	wago "github.com/wago-org/wago"
)

const (
	facetSpecDirEnv  = "FACET_SPEC_DIR"
	facetCaseEnv     = "FACET_CONFORMANCE_CASE"
	facetResultMark  = "FACETRESULT\t"
	facetCaseTimeout = 30 * time.Second
)

type facetCatalog struct {
	TestCount int                `json:"test_count"`
	Tests     []facetCatalogTest `json:"tests"`
}

type facetCatalogTest struct {
	ID   string `json:"id"`
	Path string `json:"path"`
	Kind string `json:"kind"`
}

type facetResult struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	Assertions int    `json:"assertions"`
	Reason     string `json:"reason,omitempty"`
}

type facetManifest struct {
	Version    int               `json:"version"`
	Operations []facetManifestOp `json:"operations"`
}

type facetManifestOp struct {
	Type     string                 `json:"type"`
	Args     []string               `json:"args"`
	Env      map[string]string      `json:"env"`
	Root     string                 `json:"root"`
	Preopens []facetManifestPreopen `json:"preopens"`
	ExitCode int                    `json:"exit_code"`
}

type facetManifestPreopen struct {
	Host   string   `json:"host"`
	Guest  string   `json:"guest"`
	Rights []string `json:"rights"`
}

type facetCommands struct {
	Commands []facetCommand `json:"commands"`
}

type facetCommand struct {
	Type           string       `json:"type"`
	Line           int          `json:"line"`
	Filename       string       `json:"filename"`
	BinaryFilename string       `json:"binary_filename"`
	Name           string       `json:"name"`
	Action         facetAction  `json:"action"`
	Expected       []facetValue `json:"expected"`
}

type facetAction struct {
	Type   string       `json:"type"`
	Module string       `json:"module"`
	Field  string       `json:"field"`
	Args   []facetValue `json:"args"`
}

type facetValue struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

type facetPluginConfig struct {
	Stdin    string               `json:"stdin"`
	Stdout   string               `json:"stdout"`
	Stderr   string               `json:"stderr"`
	Env      []string             `json:"env,omitempty"`
	Preopens []facetPluginPreopen `json:"preopens,omitempty"`
}

type facetPluginPreopen struct {
	Guest  string   `json:"guest"`
	Host   string   `json:"host"`
	Rights []string `json:"rights"`
}

func TestFacetConformance(t *testing.T) {
	specDir := os.Getenv(facetSpecDirEnv)
	if specDir == "" {
		t.Skip("set FACET_SPEC_DIR to the pinned facet-spec checkout")
	}
	if id := os.Getenv(facetCaseEnv); id != "" {
		result := runFacetCase(t, specDir, id)
		raw, _ := json.Marshal(result)
		fmt.Printf("%s%s\n", facetResultMark, raw)
		return
	}
	catalog, err := readFacetCatalog(specDir)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.TestCount != 143 || len(catalog.Tests) != 143 {
		t.Fatalf("Facet catalog has %d/%d tests; want 143", catalog.TestCount, len(catalog.Tests))
	}
	if _, err := exec.LookPath("wasm-tools"); err != nil {
		t.Fatal("wasm-tools is required")
	}

	var pass, fail, crash, timeout, harness, assertions int
	for _, test := range catalog.Tests {
		var result facetResult
		if test.Kind == "harness" {
			result = facetResult{ID: test.ID, Status: "HARNESS", Reason: "requires external manifest operations"}
		} else if test.Kind == "wast" {
			result = runFacetCaseProcess(test.ID)
		} else {
			result = facetResult{ID: test.ID, Status: "FAIL", Reason: "unknown catalog kind " + test.Kind}
		}
		assertions += result.Assertions
		switch result.Status {
		case "PASS":
			pass++
		case "FAIL":
			fail++
		case "CRASH":
			crash++
		case "TIMEOUT":
			timeout++
		case "HARNESS":
			harness++
		default:
			fail++
		}
		if result.Status != "PASS" && result.Status != "HARNESS" {
			t.Logf("%-48s %-7s %s", result.ID, result.Status, result.Reason)
		}
	}
	t.Logf("Facet 0.1: pass=%d fail=%d crash=%d timeout=%d harness=%d assertions=%d total=143", pass, fail, crash, timeout, harness, assertions)
	if fail+crash+timeout != 0 {
		t.Fatal("Facet conformance has unexpected failures")
	}
}

func runFacetCaseProcess(id string) facetResult {
	ctx, cancel := context.WithTimeout(context.Background(), facetCaseTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFacetConformance$", "-test.v=false")
	cmd.Env = append(os.Environ(), facetCaseEnv+"="+id)
	out, _ := cmd.CombinedOutput()
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, facetResultMark) {
			var result facetResult
			if json.Unmarshal([]byte(strings.TrimPrefix(line, facetResultMark)), &result) == nil {
				return result
			}
		}
	}
	if ctx.Err() == context.DeadlineExceeded {
		return facetResult{ID: id, Status: "TIMEOUT", Reason: "case exceeded 30 seconds"}
	}
	return facetResult{ID: id, Status: "CRASH", Reason: firstFacetLine(out)}
}

func runFacetCase(t *testing.T, specDir, id string) (result facetResult) {
	result = facetResult{ID: id, Status: "FAIL"}
	defer func() {
		if recovered := recover(); recovered != nil {
			result.Reason = fmt.Sprintf("panic: %v", recovered)
		}
	}()
	catalog, err := readFacetCatalog(specDir)
	if err != nil {
		result.Reason = err.Error()
		return
	}
	var test *facetCatalogTest
	for i := range catalog.Tests {
		if catalog.Tests[i].ID == id {
			test = &catalog.Tests[i]
			break
		}
	}
	if test == nil {
		result.Reason = "case is not in the pinned catalog"
		return
	}
	if test.Kind != "wast" {
		result.Status = "HARNESS"
		return
	}
	count, err := executeFacetWAST(t, specDir, *test)
	if err != nil {
		result.Reason = err.Error()
		return
	}
	result.Status = "PASS"
	result.Assertions = count
	return
}

func readFacetCatalog(specDir string) (facetCatalog, error) {
	raw, err := os.ReadFile(filepath.Join(specDir, "spec", "tests", "catalog.json"))
	if err != nil {
		return facetCatalog{}, err
	}
	var catalog facetCatalog
	err = json.Unmarshal(raw, &catalog)
	return catalog, err
}

func executeFacetWAST(t *testing.T, specDir string, test facetCatalogTest) (int, error) {
	wast := filepath.Join(specDir, filepath.FromSlash(test.Path))
	run, manifestPath, err := readFacetRun(wast)
	if err != nil {
		return 0, err
	}
	config, args, err := facetConfigForRun(t.TempDir(), manifestPath, run)
	if err != nil {
		return 0, err
	}
	artifactDir := t.TempDir()
	commandsPath := filepath.Join(artifactDir, "commands.json")
	tool, _ := exec.LookPath("wasm-tools")
	if output, err := exec.Command(tool, "json-from-wast", "--wasm-dir", artifactDir, "-o", commandsPath, wast).CombinedOutput(); err != nil {
		return 0, fmt.Errorf("json-from-wast: %s", firstFacetLine(output))
	}
	raw, err := os.ReadFile(commandsPath)
	if err != nil {
		return 0, err
	}
	var document facetCommands
	if err := json.Unmarshal(raw, &document); err != nil {
		return 0, err
	}

	rt := wago.NewRuntime(
		wago.WithRuntimeConfig(wago.NewRuntimeConfig().WithCoreFeatures(wago.CoreFeaturesV3)),
		wago.WithGuestArguments(args),
	)
	defer rt.Close()
	set, err := conformancePluginSet(config)
	if err != nil {
		return 0, err
	}
	if err := rt.LoadPlugins(context.Background(), set); err != nil {
		return 0, err
	}

	instances := []*wago.Instance{}
	modules := []*wago.Module{}
	named := map[string]*wago.Instance{}
	var current *wago.Instance
	defer func() {
		for i := len(instances) - 1; i >= 0; i-- {
			_ = instances[i].Close()
		}
		for i := len(modules) - 1; i >= 0; i-- {
			_ = modules[i].Close()
		}
	}()

	compile := func(command facetCommand) (*wago.Module, error) {
		name := command.BinaryFilename
		if name == "" {
			name = command.Filename
		}
		bytes, err := os.ReadFile(filepath.Join(artifactDir, filepath.FromSlash(name)))
		if err != nil {
			return nil, err
		}
		return rt.Compile(bytes)
	}
	instantiate := func(command facetCommand) (*wago.Instance, error) {
		module, err := compile(command)
		if err != nil {
			return nil, err
		}
		instance, err := rt.Instantiate(context.Background(), module)
		if err != nil {
			_ = module.Close()
			return nil, err
		}
		modules = append(modules, module)
		instances = append(instances, instance)
		return instance, nil
	}
	action := func(a facetAction) ([]wago.Value, error) {
		instance := current
		if a.Module != "" {
			instance = named[a.Module]
		}
		if instance == nil {
			return nil, errors.New("action has no module")
		}
		if a.Type != "invoke" {
			return nil, fmt.Errorf("action type %q is not supported", a.Type)
		}
		args := make([]wago.Value, len(a.Args))
		for i, raw := range a.Args {
			value, err := facetArgument(raw)
			if err != nil {
				return nil, err
			}
			args[i] = value
		}
		return instance.Call(context.Background(), a.Field, args...)
	}

	assertions := 0
	for _, command := range document.Commands {
		switch command.Type {
		case "module":
			instance, err := instantiate(command)
			if err != nil {
				return assertions, fmt.Errorf("line %d module: %w", command.Line, err)
			}
			current = instance
			if command.Name != "" {
				named[command.Name] = instance
			}
		case "assert_return":
			assertions++
			values, err := action(command.Action)
			if err != nil {
				return assertions, fmt.Errorf("line %d trapped: %w", command.Line, err)
			}
			if err := compareFacetResults(values, command.Expected); err != nil {
				return assertions, fmt.Errorf("line %d: %w", command.Line, err)
			}
		case "assert_trap", "assert_exhaustion":
			assertions++
			if _, err := action(command.Action); err == nil {
				return assertions, fmt.Errorf("line %d expected a trap", command.Line)
			}
		case "action":
			assertions++
			if _, err := action(command.Action); err != nil {
				return assertions, fmt.Errorf("line %d action: %w", command.Line, err)
			}
		case "assert_malformed", "assert_invalid":
			assertions++
			module, err := compile(command)
			if err == nil {
				_ = module.Close()
				return assertions, fmt.Errorf("line %d %s unexpectedly compiled", command.Line, command.Type)
			}
		case "assert_unlinkable", "assert_uninstantiable":
			assertions++
			module, err := compile(command)
			if err != nil {
				return assertions, fmt.Errorf("line %d %s failed during compile: %w", command.Line, command.Type, err)
			}
			instance, instantiateErr := rt.Instantiate(context.Background(), module)
			if instantiateErr == nil {
				_ = instance.Close()
				_ = module.Close()
				return assertions, fmt.Errorf("line %d %s unexpectedly instantiated", command.Line, command.Type)
			}
			_ = module.Close()
		default:
			return assertions, fmt.Errorf("line %d WAST command %q is not implemented by the adapter", command.Line, command.Type)
		}
	}
	return assertions, nil
}

func readFacetRun(wastPath string) (facetManifestOp, string, error) {
	manifestPath := strings.TrimSuffix(wastPath, filepath.Ext(wastPath)) + ".json"
	raw, err := os.ReadFile(manifestPath)
	if errors.Is(err, os.ErrNotExist) {
		return facetManifestOp{Type: "run"}, manifestPath, nil
	}
	if err != nil {
		return facetManifestOp{}, manifestPath, err
	}
	var manifest facetManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return facetManifestOp{}, manifestPath, err
	}
	if manifest.Version != 1 {
		return facetManifestOp{}, manifestPath, fmt.Errorf("manifest version %d is unsupported", manifest.Version)
	}
	var run *facetManifestOp
	for i := range manifest.Operations {
		op := &manifest.Operations[i]
		switch op.Type {
		case "run":
			if run != nil {
				return facetManifestOp{}, manifestPath, errors.New("multiple run operations require the harness adapter")
			}
			run = op
		case "wait":
			if op.ExitCode != 0 {
				return facetManifestOp{}, manifestPath, errors.New("nonzero wait requires the harness adapter")
			}
		default:
			return facetManifestOp{}, manifestPath, fmt.Errorf("operation %q requires the harness adapter", op.Type)
		}
	}
	if run == nil {
		return facetManifestOp{}, manifestPath, errors.New("manifest has no run operation")
	}
	return *run, manifestPath, nil
}

func facetConfigForRun(tempRoot, manifestPath string, run facetManifestOp) (json.RawMessage, []string, error) {
	cfg := facetPluginConfig{Stdin: "eof", Stdout: "discard", Stderr: "discard"}
	keys := make([]string, 0, len(run.Env))
	for key := range run.Env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		cfg.Env = append(cfg.Env, key+"="+run.Env[key])
	}
	base := filepath.Dir(manifestPath)
	seen := map[string]string{}
	add := func(host, guest string, rights []string) error {
		source := host
		if !filepath.IsAbs(source) {
			source = filepath.Join(base, filepath.FromSlash(source))
		}
		source = filepath.Clean(source)
		destination := seen[source]
		if destination == "" {
			destination = filepath.Join(tempRoot, fmt.Sprintf("preopen-%d", len(seen)))
			if err := os.MkdirAll(destination, 0o755); err != nil {
				return err
			}
			if output, err := exec.Command("cp", "-a", source+"/.", destination).CombinedOutput(); err != nil {
				return fmt.Errorf("copy fixture %q: %s", source, firstFacetLine(output))
			}
			seen[source] = destination
		}
		translated, err := facetManifestRights(rights)
		if err != nil {
			return err
		}
		cfg.Preopens = append(cfg.Preopens, facetPluginPreopen{Guest: guest, Host: destination, Rights: translated})
		return nil
	}
	if run.Root != "" {
		if err := add(run.Root, "/", []string{"read", "write", "seek", "tell", "stat", "set-size", "sync", "open", "create", "remove", "rename", "link", "symlink", "readlink", "iterate"}); err != nil {
			return nil, nil, err
		}
	}
	for _, preopen := range run.Preopens {
		if err := add(preopen.Host, preopen.Guest, preopen.Rights); err != nil {
			return nil, nil, err
		}
	}
	raw, err := json.Marshal(cfg)
	return raw, append([]string(nil), run.Args...), err
}

func facetManifestRights(rights []string) ([]string, error) {
	out := make([]string, 0, len(rights))
	for _, right := range rights {
		switch right {
		case "open":
			right = "path-open"
		case "create":
			right = "path-create"
		case "remove":
			right = "path-remove"
		case "rename":
			right = "path-rename"
		case "link":
			right = "path-link"
		case "symlink":
			right = "path-symlink"
		case "readlink":
			right = "path-readlink"
		case "iterate":
			right = "dir-iterate"
		case "read", "write", "seek", "tell", "stat", "set-size", "sync":
		default:
			return nil, fmt.Errorf("unknown manifest right %q", right)
		}
		out = append(out, right)
	}
	return out, nil
}

func conformancePluginSet(config json.RawMessage) (wago.PluginSet, error) {
	provider := Provider()
	digest, err := wago.DefinitionDigest(provider.Definition)
	if err != nil {
		return wago.PluginSet{}, err
	}
	grants := make([]wago.AuthorityGrant, 0, len(provider.Definition.Authorities))
	for _, request := range provider.Definition.Authorities {
		grants = append(grants, wago.AuthorityGrant{Name: request.Name, Scope: request.Scope})
	}
	return wago.PluginSet{Providers: []wago.PluginProvider{provider}, Selections: []wago.PluginSelection{{
		ID: provider.Definition.ID, DefinitionDigest: digest, Direct: true, Dependencies: map[string]string{}, Grants: grants, Config: config,
	}}}, nil
}

func facetArgument(value facetValue) (wago.Value, error) {
	text := strings.Trim(string(value.Value), "\"")
	switch value.Type {
	case "i32":
		v, err := strconv.ParseInt(text, 10, 32)
		return wago.ValueI32(int32(v)), err
	case "i64":
		v, err := strconv.ParseInt(text, 10, 64)
		return wago.ValueI64(v), err
	default:
		return wago.Value{}, fmt.Errorf("argument type %q is not implemented by the adapter", value.Type)
	}
}

func compareFacetResults(actual []wago.Value, expected []facetValue) error {
	if len(actual) != len(expected) {
		return fmt.Errorf("result count=%d; want %d", len(actual), len(expected))
	}
	for i, want := range expected {
		text := strings.Trim(string(want.Value), "\"")
		switch want.Type {
		case "i32":
			value, err := strconv.ParseInt(text, 10, 32)
			if err != nil {
				return err
			}
			if actual[i].Type() != wago.ValI32 || actual[i].I32() != int32(value) {
				return fmt.Errorf("result %d=%v; want i32(%d)", i, actual[i], int32(value))
			}
		case "i64":
			value, err := strconv.ParseInt(text, 10, 64)
			if err != nil {
				return err
			}
			if actual[i].Type() != wago.ValI64 || actual[i].I64() != value {
				return fmt.Errorf("result %d=%v; want i64(%d)", i, actual[i], value)
			}
		default:
			return fmt.Errorf("expected type %q is not implemented by the adapter", want.Type)
		}
	}
	return nil
}

func firstFacetLine(output []byte) string {
	text := strings.TrimSpace(string(output))
	if text == "" {
		return "child exited without a result"
	}
	if at := strings.IndexByte(text, '\n'); at >= 0 {
		text = text[:at]
	}
	if len(text) > 240 {
		text = text[:240] + "..."
	}
	return text
}
