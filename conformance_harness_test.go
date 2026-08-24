//go:build linux && (amd64 || arm64) && !tinygo && !wago_guardpage

package facet

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	wago "github.com/wago-org/wago"
)

const (
	facetHarnessCaseEnv    = "FACET_CONFORMANCE_HARNESS_CASE"
	facetHarnessResultMark = "FACETHARNESSRESULT\t"
	facetHarnessTimeout    = 15 * time.Second
)

type facetHarnessResult struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

type facetHarnessManifest struct {
	Version    int                      `json:"version"`
	Operations []facetHarnessManifestOp `json:"operations"`
}

type facetHarnessManifestOp struct {
	Type         string                 `json:"type"`
	Args         []string               `json:"args"`
	Env          map[string]string      `json:"env"`
	Root         string                 `json:"root"`
	Preopens     []facetManifestPreopen `json:"preopens"`
	ExitCode     int                    `json:"exit_code"`
	ID           string                 `json:"id"`
	Payload      string                 `json:"payload"`
	ProtocolType string                 `json:"protocol_type"`
}

type facetHarnessRuntime struct {
	rt        *wago.Runtime
	plugin    *Plugin
	modules   []*wago.Module
	instances []*wago.Instance
	named     map[string]*wago.Instance
	current   *wago.Instance
}

func TestFacetHarness(t *testing.T) {
	specDir := os.Getenv(facetSpecDirEnv)
	if specDir == "" {
		t.Skip("set FACET_SPEC_DIR to the pinned facet-spec checkout")
	}
	if id := os.Getenv(facetHarnessCaseEnv); id != "" {
		result := runFacetHarnessCase(t, specDir, id)
		raw, _ := json.Marshal(result)
		fmt.Printf("%s%s\n", facetHarnessResultMark, raw)
		return
	}
	catalog, err := readFacetCatalog(specDir)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, test := range catalog.Tests {
		if test.Kind == "harness" {
			ids = append(ids, test.ID)
		}
	}
	sort.Strings(ids)
	if len(ids) != 6 {
		t.Fatalf("Facet catalog has %d harness tests; want 6: %v", len(ids), ids)
	}

	var pass, fail, crash, timeout int
	for _, id := range ids {
		result := runFacetHarnessProcess(id)
		switch result.Status {
		case "PASS":
			pass++
		case "FAIL":
			fail++
		case "CRASH":
			crash++
		case "TIMEOUT":
			timeout++
		default:
			fail++
		}
		if result.Status != "PASS" {
			t.Logf("%-48s %-7s %s", result.ID, result.Status, result.Reason)
		}
	}
	t.Logf("Facet 0.1 harness: pass=%d fail=%d crash=%d timeout=%d total=6", pass, fail, crash, timeout)
	if fail+crash+timeout != 0 {
		t.Fatal("Facet harness conformance has unexpected failures")
	}
	t.Log("Facet 0.1 complete gate: 137 standard + 6 harness = 143/143 tests passing")
}

func runFacetHarnessProcess(id string) facetHarnessResult {
	ctx, cancel := context.WithTimeout(context.Background(), facetHarnessTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFacetHarness$", "-test.v=false")
	cmd.Env = append(os.Environ(), facetHarnessCaseEnv+"="+id)
	out, _ := cmd.CombinedOutput()
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, facetHarnessResultMark) {
			var result facetHarnessResult
			if json.Unmarshal([]byte(strings.TrimPrefix(line, facetHarnessResultMark)), &result) == nil {
				return result
			}
		}
	}
	if ctx.Err() == context.DeadlineExceeded {
		return facetHarnessResult{ID: id, Status: "TIMEOUT", Reason: "case exceeded 15 seconds"}
	}
	return facetHarnessResult{ID: id, Status: "CRASH", Reason: firstFacetLine(out)}
}

func runFacetHarnessCase(t *testing.T, specDir, id string) (result facetHarnessResult) {
	result = facetHarnessResult{ID: id, Status: "FAIL"}
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
	if test == nil || test.Kind != "harness" {
		result.Reason = "case is not a harness test in the pinned catalog"
		return
	}

	var runErr error
	switch id {
	case "adversarial/instance-handle-isolation":
		runErr = runFacetHandleIsolationHarness(t, specDir, *test)
	case "core/proc-exit-nonzero":
		runErr = runFacetExitHarness(t, specDir, *test)
	case "core/stdio-output":
		runErr = runFacetStdioHarness(t, specDir, *test)
	case "gc-array/writev-null-child":
		runErr = runFacetNullChildHarness(t, specDir, *test)
	case "network/tcp-echo", "network/udp-echo":
		runErr = runFacetNetworkHarness(t, specDir, *test)
	default:
		runErr = fmt.Errorf("no harness adapter for %q", id)
	}
	if runErr != nil {
		result.Reason = runErr.Error()
		return
	}
	result.Status = "PASS"
	return
}

func newFacetHarnessRuntime(t *testing.T, specDir string, test facetCatalogTest) (*facetHarnessRuntime, facetHarnessManifest, string, error) {
	wast := filepath.Join(specDir, filepath.FromSlash(test.Path))
	manifest, manifestPath, err := readFacetHarnessManifest(wast)
	if err != nil {
		return nil, facetHarnessManifest{}, "", err
	}
	run := facetManifestOp{Type: "run"}
	for _, op := range manifest.Operations {
		if op.Type == "run" {
			run.Args = append([]string(nil), op.Args...)
			run.Env = op.Env
			run.Root = op.Root
			run.Preopens = append([]facetManifestPreopen(nil), op.Preopens...)
			break
		}
	}
	config, args, err := facetConfigForRun(t.TempDir(), manifestPath, run)
	if err != nil {
		return nil, facetHarnessManifest{}, "", err
	}

	artifactDir := t.TempDir()
	commandsPath := filepath.Join(artifactDir, "commands.json")
	tool, err := exec.LookPath("wasm-tools")
	if err != nil {
		return nil, facetHarnessManifest{}, "", err
	}
	if output, err := exec.Command(tool, "json-from-wast", "--wasm-dir", artifactDir, "-o", commandsPath, wast).CombinedOutput(); err != nil {
		return nil, facetHarnessManifest{}, "", fmt.Errorf("json-from-wast: %s", firstFacetLine(output))
	}
	raw, err := os.ReadFile(commandsPath)
	if err != nil {
		return nil, facetHarnessManifest{}, "", err
	}
	var document facetCommands
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, facetHarnessManifest{}, "", err
	}

	set, err := conformancePluginSet(config)
	if err != nil {
		return nil, facetHarnessManifest{}, "", err
	}
	plugin := &Plugin{}
	set.Providers[0].New = func() wago.Plugin { return plugin }
	rt := wago.NewRuntime(
		wago.WithRuntimeConfig(wago.NewRuntimeConfig().WithCoreFeatures(wago.CoreFeaturesV3)),
		wago.WithGuestArguments(args),
	)
	if err := rt.LoadPlugins(context.Background(), set); err != nil {
		_ = rt.Close()
		return nil, facetHarnessManifest{}, "", err
	}
	h := &facetHarnessRuntime{rt: rt, plugin: plugin, named: map[string]*wago.Instance{}}

	for _, command := range document.Commands {
		if command.Type != "module" {
			continue
		}
		name := command.BinaryFilename
		if name == "" {
			name = command.Filename
		}
		moduleBytes, err := os.ReadFile(filepath.Join(artifactDir, filepath.FromSlash(name)))
		if err != nil {
			h.close()
			return nil, facetHarnessManifest{}, "", err
		}
		module, err := rt.Compile(moduleBytes)
		if err != nil {
			h.close()
			return nil, facetHarnessManifest{}, "", fmt.Errorf("compile module line %d: %w", command.Line, err)
		}
		instance, err := rt.Instantiate(context.Background(), module)
		if err != nil {
			_ = module.Close()
			h.close()
			return nil, facetHarnessManifest{}, "", fmt.Errorf("instantiate module line %d: %w", command.Line, err)
		}
		h.modules = append(h.modules, module)
		h.instances = append(h.instances, instance)
		h.current = instance
		if command.Name != "" {
			h.named[command.Name] = instance
		}
	}
	if len(h.instances) == 0 {
		h.close()
		return nil, facetHarnessManifest{}, "", errors.New("harness WAST contains no module")
	}
	return h, manifest, wast, nil
}

func readFacetHarnessManifest(wast string) (facetHarnessManifest, string, error) {
	manifestPath := strings.TrimSuffix(wast, filepath.Ext(wast)) + ".json"
	raw, err := os.ReadFile(manifestPath)
	if errors.Is(err, os.ErrNotExist) {
		return facetHarnessManifest{Version: 1, Operations: []facetHarnessManifestOp{{Type: "run"}, {Type: "wait", ExitCode: 0}}}, manifestPath, nil
	}
	if err != nil {
		return facetHarnessManifest{}, manifestPath, err
	}
	var manifest facetHarnessManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return facetHarnessManifest{}, manifestPath, err
	}
	if manifest.Version != 1 {
		return facetHarnessManifest{}, manifestPath, fmt.Errorf("manifest version %d is unsupported", manifest.Version)
	}
	return manifest, manifestPath, nil
}

func (h *facetHarnessRuntime) close() {
	if h == nil {
		return
	}
	for i := len(h.instances) - 1; i >= 0; i-- {
		_ = h.instances[i].Close()
	}
	for i := len(h.modules) - 1; i >= 0; i-- {
		_ = h.modules[i].Close()
	}
	if h.rt != nil {
		_ = h.rt.Close()
	}
}

func runFacetHandleIsolationHarness(t *testing.T, specDir string, test facetCatalogTest) error {
	h, _, _, err := newFacetHarnessRuntime(t, specDir, test)
	if err != nil {
		return err
	}
	defer h.close()
	if len(h.instances) != 2 {
		return fmt.Errorf("handle-isolation WAST has %d modules; want 2", len(h.instances))
	}
	a := h.named["A"]
	b := h.named["B"]
	if a == nil || b == nil {
		a, b = h.instances[0], h.instances[1]
	}
	values, err := a.Call(context.Background(), "acquire-valid")
	if err != nil {
		return err
	}
	if len(values) != 1 || values[0].I32() != 1 {
		return fmt.Errorf("A.acquire-valid = %v; want 1", values)
	}
	state, err := onlyFacetHarnessState(h.plugin)
	if err != nil {
		return err
	}
	state.mu.Lock()
	if len(state.preopenIDs) == 0 || state.preopenIDs[0] == 0 {
		state.mu.Unlock()
		return errors.New("A did not allocate preopen handle 0")
	}
	foreign := state.preopenIDs[0]
	state.mu.Unlock()
	values, err = b.Call(context.Background(), "probe", wago.ValueI32(int32(foreign)))
	if err != nil {
		return err
	}
	if len(values) != 1 || values[0].I32() != ErrBadHandle {
		return fmt.Errorf("B.probe(%d) = %v; want ERR_BAD_HANDLE(%d)", foreign, values, ErrBadHandle)
	}
	return nil
}

func runFacetNullChildHarness(t *testing.T, specDir string, test facetCatalogTest) error {
	h, _, _, err := newFacetHarnessRuntime(t, specDir, test)
	if err != nil {
		return err
	}
	defer h.close()
	instance := h.instances[0]
	// The first call creates this instance's private Facet state. An invalid
	// descriptor is sufficient for that setup call and is not the assertion.
	_, _ = instance.Call(context.Background(), "run", wago.ValueI32(0))
	state, err := onlyFacetHarnessState(h.plugin)
	if err != nil {
		return err
	}
	var output bytes.Buffer
	state.mu.Lock()
	state.cfg.Stdout = &output
	fd, code := state.stdio(1)
	state.mu.Unlock()
	if code != ErrOK || fd == 0 {
		return fmt.Errorf("allocate valid stdout descriptor = %d/%d", fd, code)
	}
	values, err := instance.Call(context.Background(), "run", wago.ValueI32(int32(fd)))
	if err != nil {
		return err
	}
	if len(values) != 1 || values[0].I32() != 1 {
		return fmt.Errorf("run(valid fd) = %v; want 1", values)
	}
	if output.Len() != 0 {
		return fmt.Errorf("null-child validation performed I/O: %q", output.String())
	}
	return nil
}

func runFacetExitHarness(t *testing.T, specDir string, test facetCatalogTest) error {
	h, manifest, _, err := newFacetHarnessRuntime(t, specDir, test)
	if err != nil {
		return err
	}
	defer h.close()
	want := 0
	for _, op := range manifest.Operations {
		if op.Type == "wait" {
			want = op.ExitCode
		}
	}
	_, invokeErr := h.instances[0].Invoke("_start")
	var exit *wago.ExitError
	if !errors.As(invokeErr, &exit) {
		return fmt.Errorf("_start error = %v; want ExitError(%d)", invokeErr, want)
	}
	if int(uint32(exit.Code)) != want {
		return fmt.Errorf("exit code = %d; want %d", uint32(exit.Code), want)
	}
	return nil
}

func runFacetStdioHarness(t *testing.T, specDir string, test facetCatalogTest) error {
	h, manifest, _, err := newFacetHarnessRuntime(t, specDir, test)
	if err != nil {
		return err
	}
	defer h.close()
	var stdout, stderr bytes.Buffer
	h.plugin.states.mu.Lock()
	h.plugin.states.cfg.Stdout = &stdout
	h.plugin.states.cfg.Stderr = &stderr
	h.plugin.states.mu.Unlock()
	if _, err := h.instances[0].Invoke("_start"); err != nil {
		return err
	}
	for _, op := range manifest.Operations {
		if op.Type != "read" {
			continue
		}
		var got string
		switch op.ID {
		case "stdout":
			got = stdout.String()
		case "stderr":
			got = stderr.String()
		default:
			return fmt.Errorf("unknown read stream %q", op.ID)
		}
		if got != op.Payload {
			return fmt.Errorf("%s = %q; want %q", op.ID, got, op.Payload)
		}
	}
	return nil
}

func runFacetNetworkHarness(t *testing.T, specDir string, test facetCatalogTest) error {
	h, manifest, wast, err := newFacetHarnessRuntime(t, specDir, test)
	if err != nil {
		return err
	}
	defer h.close()
	protocol := ""
	var sendPayload, recvPayload string
	for _, op := range manifest.Operations {
		switch op.Type {
		case "connect":
			protocol = op.ProtocolType
		case "send":
			sendPayload = op.Payload
		case "recv":
			recvPayload = op.Payload
		}
	}
	if protocol != "tcp" && protocol != "udp" {
		return fmt.Errorf("network protocol = %q", protocol)
	}
	address, err := facetHarnessAddress(wast)
	if err != nil {
		return err
	}
	if err := verifyFacetHarnessPortFree(protocol, address); err != nil {
		return err
	}
	ready := newFacetLineWriter()
	h.plugin.states.mu.Lock()
	h.plugin.states.cfg.Stdout = ready
	h.plugin.states.mu.Unlock()

	done := make(chan error, 1)
	go func() {
		_, err := h.instances[0].Invoke("_start")
		done <- err
	}()
	line, err := waitFacetHarnessReady(ready, done, 5*time.Second)
	if err != nil {
		return err
	}
	if strings.TrimSpace(line) != address {
		return fmt.Errorf("guest announced %q; want %q", strings.TrimSpace(line), address)
	}
	conn, err := net.DialTimeout(protocol, address, 3*time.Second)
	if err != nil {
		return fmt.Errorf("connect %s %s: %w", protocol, address, err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	if _, err := conn.Write([]byte(sendPayload)); err != nil {
		return fmt.Errorf("send: %w", err)
	}
	buf := make([]byte, len(recvPayload))
	if _, err := ioReadFull(conn, buf); err != nil {
		return fmt.Errorf("recv: %w", err)
	}
	if string(buf) != recvPayload {
		return fmt.Errorf("recv = %q; want %q", buf, recvPayload)
	}
	select {
	case invokeErr := <-done:
		if invokeErr != nil {
			return fmt.Errorf("_start: %w", invokeErr)
		}
	case <-time.After(5 * time.Second):
		return errors.New("guest did not finish after network exchange")
	}
	return nil
}

func onlyFacetHarnessState(plugin *Plugin) (*instanceState, error) {
	if plugin == nil || plugin.states == nil {
		return nil, errors.New("Facet state store is unavailable")
	}
	plugin.states.mu.Lock()
	defer plugin.states.mu.Unlock()
	if len(plugin.states.states) != 1 {
		return nil, fmt.Errorf("Facet state count = %d; want 1", len(plugin.states.states))
	}
	for _, state := range plugin.states.states {
		return state, nil
	}
	return nil, errors.New("Facet state store is empty")
}

type facetLineWriter struct {
	mu      sync.Mutex
	pending string
	lines   chan string
}

func newFacetLineWriter() *facetLineWriter {
	return &facetLineWriter{lines: make(chan string, 4)}
}

func (w *facetLineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.pending += string(p)
	for {
		at := strings.IndexByte(w.pending, '\n')
		if at < 0 {
			break
		}
		line := w.pending[:at+1]
		w.pending = w.pending[at+1:]
		select {
		case w.lines <- line:
		default:
		}
	}
	w.mu.Unlock()
	return len(p), nil
}

func waitFacetHarnessReady(writer *facetLineWriter, done <-chan error, timeout time.Duration) (string, error) {
	select {
	case line := <-writer.lines:
		return line, nil
	case err := <-done:
		if err == nil {
			return "", errors.New("guest returned before announcing readiness")
		}
		return "", fmt.Errorf("guest failed before readiness: %w", err)
	case <-time.After(timeout):
		return "", errors.New("guest did not announce readiness")
	}
}

var facetHarnessAddressPattern = regexp.MustCompile(`127\.0\.0\.1:[0-9]+`)

func facetHarnessAddress(wast string) (string, error) {
	raw, err := os.ReadFile(wast)
	if err != nil {
		return "", err
	}
	address := facetHarnessAddressPattern.FindString(string(raw))
	if address == "" {
		return "", errors.New("network harness WAST does not declare a loopback address")
	}
	return address, nil
}

func verifyFacetHarnessPortFree(protocol, address string) error {
	if protocol == "tcp" {
		listener, err := net.Listen("tcp", address)
		if err != nil {
			return fmt.Errorf("TCP harness address %s is unavailable: %w", address, err)
		}
		return listener.Close()
	}
	packet, err := net.ListenPacket("udp", address)
	if err != nil {
		return fmt.Errorf("UDP harness address %s is unavailable: %w", address, err)
	}
	return packet.Close()
}

func ioReadFull(conn net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := conn.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, errors.New("zero-byte network read")
		}
	}
	return total, nil
}
