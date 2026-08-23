# Facet 0.1 conformance

`wago-facet` executes the normative Facet 0.1 WAST suite from a pinned `facet-spec` revision.

The pin is stored in [`FACET_SPEC_REVISION`](FACET_SPEC_REVISION). CI checks out that exact commit. Do not silently test against whatever `facet-spec/main` contains today.

## Current gate

```text
137 / 137 standard WAST tests   PASS
  6 /   6 harness tests         PASS
143 / 143 total tests           PASS
0 failures
0 crashes
0 timeouts
```

The standard runner is `TestFacetConformance` in the repository root. Each executable WAST case runs in an isolated subprocess with a timeout. A runtime crash or hang is therefore a test failure that does not hide the remaining cases.

The harness runner is `TestFacetHarness`. It implements the six catalog cases that need operations outside standard WAST command composition:

- cross-instance handle isolation;
- process exit status;
- stdout and stderr validation;
- null nested-GC child validation before I/O;
- TCP loopback interaction;
- UDP loopback interaction.

## Requirements

The runner currently targets Linux/amd64.

Install:

- Go 1.23 or a compatible version for this module;
- `wasm-tools` 1.256.0.

CI uses the pinned `wasm-tools` 1.256.0 release artifact and verifies its SHA-256 checksum before execution.

## Run locally

Check out the pinned Facet specification revision:

```sh
REVISION=$(cat tests/conformance/FACET_SPEC_REVISION)
git clone https://github.com/jtenner/facet-spec /tmp/facet-spec
git -C /tmp/facet-spec checkout "$REVISION"
```

Then run both conformance layers:

```sh
FACET_SPEC_DIR=/tmp/facet-spec \
  go test -count=1 -run '^(TestFacetConformance|TestFacetHarness)$' -v .
```

A successful run includes:

```text
Facet 0.1 harness: pass=6 fail=0 crash=0 timeout=0 total=6
Facet 0.1 complete gate: 137 standard + 6 harness = 143/143 tests passing
Facet 0.1: pass=137 fail=0 crash=0 timeout=0 harness=6 assertions=104 total=143
```

## Run one standard case

Use the child-runner environment variable when you need a focused WAST result:

```sh
FACET_SPEC_DIR=/tmp/facet-spec \
FACET_CONFORMANCE_CASE=args-env/args-read-array-i8 \
  go test -count=1 -run '^TestFacetConformance$' -v .
```

## Run one harness case

For example:

```sh
FACET_SPEC_DIR=/tmp/facet-spec \
FACET_CONFORMANCE_HARNESS_CASE=network/tcp-echo \
  go test -count=1 -run '^TestFacetHarness$' -v .
```

The TCP and UDP harness tests use the fixed loopback ports declared by the normative suite. The runner checks that the selected port is free before guest execution and runs the network cases serially.

## Classify failures before changing code

When a new conformance failure appears, identify its source before editing the implementation:

1. If the WAST or manifest disagrees with the existing normative Facet behavior, fix the conformance source in `facet-spec` and add that exact revision to this pin only after the source-suite checks pass.
2. If the adapter cannot express a valid Facet test, fix the conformance adapter in this repository.
3. If `wago-facet` returns the wrong observable result, fix the plugin implementation and keep the normative test unchanged.
4. If the Facet specification itself is genuinely ambiguous, resolve the rule in `facet-spec` first and update the conformance case with the normative text.

Do not turn an unexpected failure into a skip. A crash, panic, timeout, wrong error code, or wrong guest-visible value is a failure.
