//go:build linux && (amd64 || arm64) && !tinygo && !wago_guardpage

package facet

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	wago "github.com/wago-org/wago"
)

const representationDifferentialArgument = "representation-equivalence"

const representationDifferentialWAT = `(module
  (type $bytes (array (mut i8)))
  (import "facet" "args_read_mem32_i8"
    (func $read32 (param i32 i32 i32 i32 i32) (result i64 i32)))
  (import "facet" "args_read_mem64_i8"
    (func $read64 (param i32 i32 i32 i64 i64) (result i64 i32)))
  (import "facet" "args_read_into_array_i8"
    (func $read_array (param i32 i32 (ref array) i32 i32) (result i64 i32)))

  (memory $mem32 1)
  (memory $mem64 i64 1)

  (func (export "run") (result i32)
    (local $array (ref $bytes))
    (local $length i64)
    (local $errno i32)
    (local $index i32)

    (call $read32
      (i32.const 0) (i32.const 0)
      (i32.const 0) (i32.const 0) (i32.const 26))
    (local.set $errno) (local.set $length)
    (if (i32.or (local.get $errno) (i64.ne (local.get $length) (i64.const 26)))
      (then (return (i32.const 1))))

    (call $read64
      (i32.const 0) (i32.const 0)
      (i32.const 1) (i64.const 0) (i64.const 26))
    (local.set $errno) (local.set $length)
    (if (i32.or (local.get $errno) (i64.ne (local.get $length) (i64.const 26)))
      (then (return (i32.const 2))))

    (local.set $array (array.new_default $bytes (i32.const 26)))
    (call $read_array
      (i32.const 0) (i32.const 0)
      (local.get $array) (i32.const 0) (i32.const 26))
    (local.set $errno) (local.set $length)
    (if (i32.or (local.get $errno) (i64.ne (local.get $length) (i64.const 26)))
      (then (return (i32.const 3))))

    (block $done
      (loop $compare
        (br_if $done (i32.ge_u (local.get $index) (i32.const 26)))
        (if
          (i32.or
            (i32.ne
              (i32.load8_u $mem32 (local.get $index))
              (i32.load8_u $mem64 (i64.extend_i32_u (local.get $index))))
            (i32.ne
              (i32.load8_u $mem32 (local.get $index))
              (array.get_u $bytes (local.get $array) (local.get $index))))
          (then (return (i32.const 4))))
        (local.set $index (i32.add (local.get $index) (i32.const 1)))
        (br $compare)))

    (i32.const 0)))
`

func TestArgumentRepresentationsAreDifferentiallyEquivalent(t *testing.T) {
	tool, err := exec.LookPath("wasm-tools")
	if err != nil {
		t.Skip("wasm-tools is required for the representation differential test")
	}
	watPath := filepath.Join(t.TempDir(), "representation-differential.wat")
	wasmPath := filepath.Join(t.TempDir(), "representation-differential.wasm")
	if err := os.WriteFile(watPath, []byte(representationDifferentialWAT), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(tool, "parse", watPath, "-o", wasmPath).CombinedOutput(); err != nil {
		t.Fatalf("parse representation differential module: %s", firstFacetLine(output))
	}
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		gc   *wago.GCConfig
	}{
		{name: "default"},
		{name: "moving-gc", gc: &wago.GCConfig{
			StressNurseryBytes:   64,
			CollectEveryAlloc:    true,
			ForceMajorEveryMinor: true,
			VerifyAfterCollect:   true,
			PoisonFreed:          true,
			StressBarriers:       true,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := wago.NewRuntimeConfig().WithCoreFeatures(wago.CoreFeaturesV3)
			rt := wago.NewRuntime(
				wago.WithRuntimeConfig(cfg),
				wago.WithGuestArguments([]string{representationDifferentialArgument}),
			)
			defer rt.Close()
			if err := rt.LoadPlugins(context.Background(), facetPluginSetForTest(t)); err != nil {
				t.Fatal(err)
			}
			mod, err := rt.Compile(wasmBytes)
			if err != nil {
				t.Fatal(err)
			}
			defer mod.Close()
			var options []wago.InstantiateOption
			if tc.gc != nil {
				options = append(options, wago.WithGC(*tc.gc))
			}
			inst, err := rt.Instantiate(context.Background(), mod, options...)
			if err != nil {
				t.Fatal(err)
			}
			defer inst.Close()
			values, err := inst.Call(context.Background(), "run")
			if err != nil {
				t.Fatal(err)
			}
			if len(values) != 1 || values[0].I32() != 0 {
				t.Fatalf("representation differential result = %v, want 0", values)
			}
		})
	}
}
