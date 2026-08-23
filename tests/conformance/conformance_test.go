//go:build linux

package conformance_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	facet "github.com/jtenner/wago-facet"
	wago "github.com/wago-org/wago"
)

const resultPrefix = "FACETRESULT\t"

type catalog struct {
	Version       int           `json:"version"`
	Specification string        `json:"specification"`
	TestCount     int           `json:"test_count"`
	Tests         []catalogTest `json:"tests"`
}

type catalogTest struct {
	ID       string   `json:"id"`
	Path     string   `json:"path"`
	Profiles []string `json:"profiles"`
	Purpose  string   `json:"purpose"`
	Kind     string   `json:"kind"`
}

type manifest struct {
	Version    int                 `json:"version"`
	Operations []manifestOperation `json:"operations"`
}

type manifestOperation struct {
	Type      string            `json:"type"`
	Args      []string          `json:"args"`
	Env       map[string]string `json:"env"`
	Root      string            `json:"root"`
	Preopens  []manifestPreopen `json:"preopens"`
	ExitCode  *int              `json:"exit_code"`
	Path      string            `json:"path"`
	Data      string            `json:"data"`
	Address   string            `json:"address"`
	Port      int               `json:"port"`
	TimeoutMS int               `json:"timeout_ms"`
}

type manifestPreopen struct {
	Host   string   `json:"host"`
	Guest  string   `json:"guest"`
	Rights []string `json:"rights"`
}

type pluginConfig struct {
	Stdin    string          `json:"stdin,omitempty"`
	Stdout   string          `json:"stdout,omitempty"`
	Stderr   string          `json:"stderr,omitempty"`
	Env      []string        `json:"env,omitempty"`
	Preopens []pluginPreopen `json:"preopens,omitempty"`
}

type pluginPreopen struct {
	Host   string   `json:"host"`
	Guest  string   `json:"guest"`
	Rights []string `json:"rights,omitempty"`
}

type wastDocument struct {
	Commands []wastCommand `json:"commands"`
}

type wastCommand struct {
	Type           string      `json:"type"`
	Line           int         `json:"line"`
	Filename       string      `json:"filename"`
	BinaryFilename string      `json:"binary_filename"`
	ModuleType     string      `json:"module_type"`
	Name           string      `json:"name"`
	As             string      `json:"as"`
	Action         wastAction  `json:"action"`
	Expected       []wastValue `json:"expected"`
	Text           string      `json:"text"`
}

type wastAction struct {
	Type   string      `json:"type"`
	Module string      `json:"module"`
	Field  string      `json:"field"`
	Args   []wastValue `json:"args"`
}

type wastValue struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

type caseResult struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	Assertions  int    `json:"assertions"`
	Unsupported string `json:"unsupported,omitempty"`
	Failure     string `json:"failure,omitempty"`
}

type caseState struct {
	rt       *wago.Runtime
	ctx      context.Context
	wasmDir  string
	current  *wago.Instance
	named    map[string]*wago.Instance
	modules  []*wago.Module
	instances []*wago.Instance
}

func TestFacetConformance(t *testing.T) {
	specDir := os.Getenv("FACET_SPEC_DIR")
	if specDir == "" {
		t.Skip("FACET_SPEC_DIR is not set; use the pinned Facet checkout from tests/conformance/FACET_SPEC_REVISION")
	}
	wasmTools := os.Getenv("FACET_WASM_TOOLS")
	if wasmTools == "" {
		wasmTools = "wasm-tools"
	}
	if _, err := exec.LookPath(wasmTools); err != nil {
		t.Skipf("%s is not on PATH", wasmTools)
	}

	cat, err := readCatalog(specDir)
	if err != nil {
		t.Fatal(err)
	}
	if cat.Specification != "Facet 0.1" {
		t.Fatalf("suite specification = %q; want Facet 0.1", cat.Specification)
	}
	if cat.TestCount != len(cat.Tests) {
		t.Fatalf("catalog test_count=%d but contains %d tests", cat.TestCount, len(cat.Tests))
	}
	if cat.TestCount != 143 {
		t.Fatalf("pinned Facet suite contains %d tests; want 143", cat.TestCount)
	}

	if selected := os.Getenv("FACET_CONFORMANCE_CASE"); selected != "" {
		entry, ok := findCatalogTest(cat.Tests, selected)
		if !ok {
			t.Fatalf("unknown Facet conformance case %q", selected)
		}
		result := runCaseSafely(t, specDir, wasmTools, entry)
		raw, _ := json.Marshal(result)
		fmt.Printf("%s%s\n", resultPrefix, raw)
		return
	}

	results := make([]caseResult, 0, len(cat.Tests))
	for _, entry := range cat.Tests {
		results = append(results, runCaseSubprocess(t, specDir, wasmTools, entry.ID))
	}
	sort.Slice(results, func(i, j int) bool { return results[i].ID < results[j].ID })

	var passed, failed, unsupported, assertions int
	for _, result := range results {
		assertions += result.Assertions
		switch result.Status {
		case "PASS":
			passed++
		case "UNSUPPORTED":
			unsupported++
			t.Logf("UNSUPPORTED %-48s %s", result.ID, result.Unsupported)
		default:
			failed++
			t.Logf("FAIL        %-48s %s", result.ID, result.Failure)
		}
	}
		t.Logf("Facet 0.1 conformance: tests=%d pass=%d fail=%d unsupported=%d assertions=%d", len(results), passed, failed, unsupported, assertions)
	if failed != 0 {
		t.Fatalf("Facet conformance has %d failing test(s)", failed)
	}
	if os.Getenv("FACET_CONFORMANCE_REQUIRE_ALL") == "1" && unsupported != 0 {
		t.Fatalf("Facet conformance has %d unsupported test(s)", unsupported)
	}
}

func readCatalog(specDir string) (catalog, error) {
	raw, err := os.ReadFile(filepath.Join(specDir, "spec", "tests", "catalog.json"))
	if err != nil {
		return catalog{}, fmt.Errorf("read Facet catalog: %w", err)
	}
	var out catalog
	if err := json.Unmarshal(raw, &out); err != nil {
		return catalog{}, fmt.Errorf("decode Facet catalog: %w", err)
	}
	return out, nil
}

func findCatalogTest(tests []catalogTest, id string) (catalogTest, bool) {
	for _, test := range tests {
		if test.ID == id {
			return test, true
		}
	}
	return catalogTest{}, false
}

func runCaseSubprocess(t *testing.T, specDir, wasmTools, id string) caseResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFacetConformance$")
	cmd.Env = append(os.Environ(),
		"FACET_SPEC_DIR="+specDir,
		"FACET_WASM_TOOLS="+wasmTools,
		"FACET_CONFORMANCE_CASE="+id,
	)
	out, err := cmd.CombinedOutput()
	for _, line := range strings.Split(string(out), "\n") {
		if raw, ok := strings.CutPrefix(line, resultPrefix); ok {
			var result caseResult
			if json.Unmarshal([]byte(raw), &result) == nil {
				return result
			}
		}
	}
	if ctx.Err() == context.DeadlineExceeded {
		return caseResult{ID: id, Status: "FAIL", Failure: "runtime timeout"}
	}
	if err != nil {
		return caseResult{ID: id, Status: "FAIL", Failure: "runner crash: " + firstLine(string(out))}
	}
	return caseResult{ID: id, Status: "FAIL", Failure: "runner produced no result"}
}

func runCaseSafely(t *testing.T, specDir, wasmTools string, entry catalogTest) (result caseResult) {
	result = caseResult{ID: entry.ID, Status: "FAIL"}
	defer func() {
		if recovered := recover(); recovered != nil {
			result.Status = "FAIL"
			result.Failure = fmt.Sprintf("panic: %v", recovered)
		}
	}()
	if entry.Kind == "harness" {
		result.Status = "UNSUPPORTED"
		result.Unsupported = "Facet manifest requires harness-driven external interaction"
		return result
	}
	return runStandardCase(t, specDir, wasmTools, entry)
}

func runStandardCase(t *testing.T, specDir, wasmTools string, entry catalogTest) caseResult {
	result := caseResult{ID: entry.ID, Status: "FAIL"}
	wastPath := filepath.Join(specDir, filepath.FromSlash(entry.Path))
	manifestPath := strings.TrimSuffix(wastPath, filepath.Ext(wastPath)) + ".json"
	manifestValue, err := readManifest(manifestPath)
	if err != nil {
		result.Failure = err.Error()
		return result
	}
	for _, op := range manifestValue.Operations {
		if op.Type != "run" && op.Type != "wait" {
			result.Status = "UNSUPPORTED"
			result.Unsupported = "manifest operation " + op.Type
			return result
		}
	}

	work := t.TempDir()
	wasmDir := filepath.Join(work, "wasm")
	if err := os.MkdirAll(wasmDir, 0o755); err != nil {
		result.Failure = err.Error()
		return result
	}
	commandJSON := filepath.Join(work, "commands.json")
	cmd := exec.Command(wasmTools, "json-from-wast", "--wasm-dir", wasmDir, "-o", commandJSON, wastPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		result.Failure = "json-from-wast: " + firstLine(string(out))
		return result
	}
	raw, err := os.ReadFile(commandJSON)
	if err != nil {
		result.Failure = err.Error()
		return result
	}
	var document wastDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		result.Failure = "decode WAST command JSON: " + err.Error()
		return result
	}

	runOp := firstRun(manifestValue)
	args := append([]string(nil), runOp.Args...)
	pluginCfg, err := buildPluginConfig(work, filepath.Dir(manifestPath), runOp)
	if err != nil {
		result.Failure = err.Error()
		return result
	}
	rt, err := newFacetRuntime(args, pluginCfg)
	if err != nil {
		result.Failure = err.Error()
		return result
	}
	state := &caseState{
		rt:      rt,
		ctx:     context.Background(),
		wasmDir: wasmDir,
		named:   map[string]*wago.Instance{},
	}
	defer state.close()

	for _, command := range document.Commands {
		status, why, count := state.execute(command)
		result.Assertions += count
		if status == "UNSUPPORTED" {
			result.Status = status
			result.Unsupported = fmt.Sprintf("line %d: %s", command.Line, why)
			return result
		}
		if status == "FAIL" {
			result.Status = status
			result.Failure = fmt.Sprintf("line %d: %s", command.Line, why)
			return result
		}
	}
	result.Status = "PASS"
	return result
}

func readManifest(path string) (manifest, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return manifest{Version: 1, Operations: []manifestOperation{{Type: "run"}, {Type: "wait", ExitCode: intPtr(0)}}}, nil
	}
	if err != nil {
		return manifest{}, fmt.Errorf("read manifest: %w", err)
	}
	var out manifest
	if err := json.Unmarshal(raw, &out); err != nil {
		return manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	if out.Version != 1 {
		return manifest{}, fmt.Errorf("manifest version %d is unsupported", out.Version)
	}
	return out, nil
}

func intPtr(v int) *int { return &v }

func firstRun(m manifest) manifestOperation {
	for _, op := range m.Operations {
		if op.Type == "run" {
			return op
		}
	}
	return manifestOperation{Type: "run"}
}

func buildPluginConfig(work, manifestDir string, run manifestOperation) (json.RawMessage, error) {
	cfg := pluginConfig{Stdin: "eof", Stdout: "discard", Stderr: "discard"}
	keys := make([]string, 0, len(run.Env))
	for key := range run.Env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		cfg.Env = append(cfg.Env, key+"="+run.Env[key])
	}

	preopens := append([]manifestPreopen(nil), run.Preopens...)
	if run.Root != "" {
		preopens = append([]manifestPreopen{{Host: run.Root, Guest: "/", Rights: allManifestRights()}}, preopens...)
	}
	for i, preopen := range preopens {
		source := preopen.Host
		if !filepath.IsAbs(source) {
			source = filepath.Join(manifestDir, filepath.FromSlash(source))
		}
		target := filepath.Join(work, fmt.Sprintf("preopen-%d", i))
		if err := copyTree(source, target); err != nil {
			return nil, fmt.Errorf("copy preopen %q: %w", preopen.Guest, err)
		}
		rights := make([]string, 0, len(preopen.Rights))
		for _, right := range preopen.Rights {
			mapped, ok := manifestRightToPlugin(right)
			if !ok {
				return nil, fmt.Errorf("manifest preopen right %q is unknown", right)
			}
			rights = append(rights, mapped)
		}
		cfg.Preopens = append(cfg.Preopens, pluginPreopen{Host: target, Guest: preopen.Guest, Rights: rights})
	}
	return json.Marshal(cfg)
}

func allManifestRights() []string {
	return []string{"read", "write", "seek", "tell", "stat", "set-size", "sync", "open", "create", "remove", "rename", "link", "symlink", "readlink", "iterate"}
}

func manifestRightToPlugin(right string) (string, bool) {
	switch right {
	case "read", "write", "seek", "tell", "stat", "set-size", "sync":
		return right, true
	case "open":
		return "path-open", true
	case "create":
		return "path-create", true
	case "remove":
		return "path-remove", true
	case "rename":
		return "path-rename", true
	case "link":
		return "path-link", true
	case "symlink":
		return "path-symlink", true
	case "readlink":
		return "path-readlink", true
	case "iterate":
		return "dir-iterate", true
	default:
		return "", false
	}
}

func newFacetRuntime(args []string, cfg json.RawMessage) (*wago.Runtime, error) {
	provider := facet.Provider()
	digest, err := wago.DefinitionDigest(provider.Definition)
	if err != nil {
		return nil, err
	}
	grants := make([]wago.AuthorityGrant, 0, len(provider.Definition.Authorities))
	for _, request := range provider.Definition.Authorities {
		grants = append(grants, wago.AuthorityGrant{Name: request.Name, Scope: request.Scope})
	}
	set := wago.PluginSet{
		Providers: []wago.PluginProvider{provider},
		Selections: []wago.PluginSelection{{
			ID:               provider.Definition.ID,
			DefinitionDigest: digest,
			Direct:           true,
			Dependencies:     map[string]string{},
			Grants:           grants,
			Config:           cfg,
		}},
	}
	runtimeCfg := wago.NewRuntimeConfig().WithCoreFeatures(wago.CoreFeaturesV3)
	rt := wago.NewRuntime(wago.WithRuntimeConfig(runtimeCfg), wago.WithGuestArguments(args))
	if err := rt.LoadPlugins(context.Background(), set); err != nil {
		_ = rt.Close()
		return nil, fmt.Errorf("load Facet plugin: %w", err)
	}
	return rt, nil
}

func (s *caseState) execute(command wastCommand) (status, why string, assertions int) {
	switch command.Type {
	case "module":
		path, ok := s.binaryPath(command)
		if !ok {
			return "UNSUPPORTED", "text-only module artifact", 0
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return "FAIL", err.Error(), 0
		}
		module, err := s.rt.Compile(raw)
		if err != nil {
			return "FAIL", "compile module: " + err.Error(), 0
		}
		s.modules = append(s.modules, module)
		for _, imp := range module.Imports() {
			if imp.Module != facet.Module {
				return "UNSUPPORTED", "cross-module/non-Facet import " + imp.Module + "." + imp.Name, 0
			}
		}
		instance, err := s.rt.Instantiate(s.ctx, module)
		if err != nil {
			return "FAIL", "instantiate module: " + err.Error(), 0
		}
		s.instances = append(s.instances, instance)
		s.current = instance
		if command.Name != "" {
			s.named[command.Name] = instance
		}
		return "PASS", "", 0
	case "register":
		instance := s.current
		if command.Name != "" {
			instance = s.named[command.Name]
		}
		if instance == nil {
			return "FAIL", "register names an unknown module", 0
		}
		// Registration is tracked for action lookup. Cross-module import wiring is
		// reported as unsupported when a later module declares a non-Facet import.
		if command.As != "" {
			s.named[command.As] = instance
		}
		return "PASS", "", 0
	case "assert_return":
		values, err := s.action(command.Action)
		if err != nil {
			return "FAIL", "assert_return trapped: " + err.Error(), 1
		}
		if err := compareExpected(values, command.Expected); err != nil {
			return "FAIL", err.Error(), 1
		}
		return "PASS", "", 1
	case "assert_trap", "assert_exhaustion":
		_, err := s.action(command.Action)
		if err == nil {
			return "FAIL", "expected trap", 1
		}
		return "PASS", "", 1
	case "action":
		if _, err := s.action(command.Action); err != nil {
			return "FAIL", "action failed: " + err.Error(), 1
		}
		return "PASS", "", 1
	case "assert_invalid", "assert_malformed", "assert_malformed_custom", "assert_invalid_custom":
		path, ok := s.binaryPath(command)
		if !ok {
			return "UNSUPPORTED", command.Type + " has no binary artifact", 1
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return "FAIL", err.Error(), 1
		}
		module, err := s.rt.Compile(raw)
		if err == nil {
			_ = module.Close()
			return "FAIL", "expected compile rejection", 1
		}
		return "PASS", "", 1
	case "assert_unlinkable":
		path, ok := s.binaryPath(command)
		if !ok {
			return "UNSUPPORTED", "assert_unlinkable has no binary artifact", 1
		}
		module, err := s.compileTemporary(path)
		if err != nil {
			return "FAIL", "unlinkable module failed compilation: " + err.Error(), 1
		}
		defer module.Close()
		if instance, err := s.rt.Instantiate(s.ctx, module); err == nil {
			_ = instance.Close()
			return "FAIL", "expected link failure", 1
		}
		return "PASS", "", 1
	case "assert_uninstantiable":
		path, ok := s.binaryPath(command)
		if !ok {
			return "UNSUPPORTED", "assert_uninstantiable has no binary artifact", 1
		}
		module, err := s.compileTemporary(path)
		if err != nil {
			return "FAIL", "uninstantiable module failed compilation: " + err.Error(), 1
		}
		defer module.Close()
		if instance, err := s.rt.Instantiate(s.ctx, module); err == nil {
			_ = instance.Close()
			return "FAIL", "expected instantiation failure", 1
		}
		return "PASS", "", 1
	case "module_definition", "module_instance", "thread", "wait", "assert_exception", "assert_suspension":
		return "UNSUPPORTED", "WAST command " + command.Type, 0
	default:
		return "UNSUPPORTED", "unknown WAST command " + command.Type, 0
	}
}

func (s *caseState) binaryPath(command wastCommand) (string, bool) {
	name := command.BinaryFilename
	if name == "" {
		name = command.Filename
	}
	if name == "" || filepath.Ext(name) != ".wasm" {
		return "", false
	}
	if filepath.IsAbs(name) {
		return name, true
	}
	return filepath.Join(s.wasmDir, filepath.Base(name)), true
}

func (s *caseState) compileTemporary(path string) (*wago.Module, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return s.rt.Compile(raw)
}

func (s *caseState) action(action wastAction) ([]wago.Value, error) {
	instance := s.current
	if action.Module != "" {
		instance = s.named[action.Module]
	}
	if instance == nil {
		return nil, errors.New("action has no active module")
	}
	switch action.Type {
	case "invoke":
		args := make([]wago.Value, len(action.Args))
		for i, value := range action.Args {
			arg, err := decodeWastValue(value)
			if err != nil {
				return nil, fmt.Errorf("argument %d: %w", i, err)
			}
			args[i] = arg
		}
		return instance.Call(s.ctx, action.Field, args...)
	case "get":
		value, err := instance.GlobalValue(action.Field)
		if err != nil {
			return nil, err
		}
		return []wago.Value{value}, nil
	default:
		return nil, fmt.Errorf("unsupported action %q", action.Type)
	}
}

func decodeWastValue(value wastValue) (wago.Value, error) {
	text, err := rawString(value.Value)
	if err != nil {
		return wago.Value{}, err
	}
	switch value.Type {
	case "i32":
		v, err := strconv.ParseInt(text, 10, 32)
		if err != nil {
			return wago.Value{}, err
		}
		return wago.ValueI32(int32(v)), nil
	case "i64":
		v, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return wago.Value{}, err
		}
		return wago.ValueI64(v), nil
	case "f32":
		bits, err := strconv.ParseUint(text, 10, 32)
		if err != nil {
			return wago.Value{}, err
		}
		return wago.ValueF32(math.Float32frombits(uint32(bits))), nil
	case "f64":
		bits, err := strconv.ParseUint(text, 10, 64)
		if err != nil {
			return wago.Value{}, err
		}
		return wago.ValueF64(math.Float64frombits(bits)), nil
	case "funcref", "externref", "anyref", "eqref", "arrayref", "structref", "i31ref", "ref.null", "nullref", "nullfuncref", "nullexternref", "nullexnref":
		if text == "" || text == "null" {
			return wago.ValueOf(referenceValType(value.Type), 0), nil
		}
		return wago.Value{}, fmt.Errorf("non-null %s WAST arguments are not supported by the Facet adapter", value.Type)
	default:
		return wago.Value{}, fmt.Errorf("WAST value type %q is unsupported", value.Type)
	}
}

func referenceValType(kind string) wago.ValType {
	switch kind {
	case "funcref", "nullfuncref":
		return wago.ValFuncRef
	case "externref", "nullexternref":
		return wago.ValExternRef
	case "i31ref":
		return wago.ValI31Ref
	default:
		return wago.ValAnyRef
	}
}

func compareExpected(actual []wago.Value, expected []wastValue) error {
	if len(actual) != len(expected) {
		return fmt.Errorf("result count = %d; want %d", len(actual), len(expected))
	}
	for i, want := range expected {
		if err := compareValue(actual[i], want); err != nil {
			return fmt.Errorf("result %d: %w", i, err)
		}
	}
	return nil
}

func compareValue(actual wago.Value, expected wastValue) error {
	text, err := rawString(expected.Value)
	if err != nil {
		return err
	}
	switch expected.Type {
	case "i32":
		want, err := strconv.ParseInt(text, 10, 32)
		if err != nil {
			return err
		}
		if actual.Type() != wago.ValI32 || actual.I32() != int32(want) {
			return fmt.Errorf("got %v; want i32(%d)", actual, want)
		}
	case "i64":
		want, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return err
		}
		if actual.Type() != wago.ValI64 || actual.I64() != want {
			return fmt.Errorf("got %v; want i64(%d)", actual, want)
		}
	case "f32":
		if strings.HasPrefix(text, "nan:") {
			if actual.Type() != wago.ValF32 || !math.IsNaN(float64(actual.F32())) {
				return fmt.Errorf("got %v; want %s", actual, text)
			}
			return nil
		}
		want, err := strconv.ParseUint(text, 10, 32)
		if err != nil {
			return err
		}
		if actual.Type() != wago.ValF32 || math.Float32bits(actual.F32()) != uint32(want) {
			return fmt.Errorf("got %v; want f32 bits %d", actual, want)
		}
	case "f64":
		if strings.HasPrefix(text, "nan:") {
			if actual.Type() != wago.ValF64 || !math.IsNaN(actual.F64()) {
				return fmt.Errorf("got %v; want %s", actual, text)
			}
			return nil
		}
		want, err := strconv.ParseUint(text, 10, 64)
		if err != nil {
			return err
		}
		if actual.Type() != wago.ValF64 || math.Float64bits(actual.F64()) != want {
			return fmt.Errorf("got %v; want f64 bits %d", actual, want)
		}
	case "funcref", "externref", "anyref", "eqref", "arrayref", "structref", "i31ref", "ref.null", "nullref", "nullfuncref", "nullexternref", "nullexnref":
		if text != "" && text != "null" {
			return fmt.Errorf("non-null reference expectation %q is unsupported", text)
		}
		if actual.Bits() != 0 {
			return fmt.Errorf("got %v; want null reference", actual)
		}
	default:
		return fmt.Errorf("expected WAST type %q is unsupported", expected.Type)
	}
	return nil
}

func rawString(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return "", err
	}
	return text, nil
}

func (s *caseState) close() {
	for i := len(s.instances) - 1; i >= 0; i-- {
		_ = s.instances[i].Close()
	}
	for i := len(s.modules) - 1; i >= 0; i-- {
		_ = s.modules[i].Close()
	}
	if s.rt != nil {
		_ = s.rt.Close()
	}
}

func copyTree(source, target string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("fixture %q is not a directory", source)
	}
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(target, rel)
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			return os.Symlink(link, dst)
		}
		if info.IsDir() {
			return os.MkdirAll(dst, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeErr := out.Close()
		return errors.Join(copyErr, closeErr)
	})
}

func firstLine(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "no diagnostic output"
	}
	if at := strings.IndexByte(text, '\n'); at >= 0 {
		return text[:at]
	}
	return text
}
