# wago-facet

`wago-facet` is the reference [Wago](https://github.com/wago-org/wago) plugin for [Facet](https://github.com/jtenner/facet-spec).

Facet is a small system interface for Core WebAssembly. The import module is `"facet"`.

This repository follows Facet 0.1.

## Status

The plugin is experimental.

The Runtime plugin path implements the canonical Facet 0.1 import surface. The inventory test requires exactly **261** Facet imports with no duplicate names.

Wago callback-scoped guest-storage APIs provide the representation-sensitive operations that Facet needs:

- Memory32 and Memory64 by exact WebAssembly memory index;
- multi-memory guest buffers and iovecs;
- numeric and `v128` Wasm GC arrays;
- nested GC arrays for scatter/gather I/O;
- exact caller-defined GC types;
- caller-typed GC array result allocation;
- zero-copy mutable GC-array payload access.

GC references remain opaque Wago tokens. `wago-facet` does not interpret collector references or object pointers. It resolves them only through Wago's callback-scoped guest-storage APIs.

The preferred integration is `Provider()` through Wago's plugin system. Some caller-typed allocating imports require Wago's optional `instance.instantiate.intercept` authority so the plugin can reject an incompatible caller-selected result type before the instance starts.

The low-level `Imports(Config)` API intentionally exposes only the subset that does not require Runtime callback-scoped guest storage or caller-type metadata. Use `Provider()` for complete Facet support.

## Implemented surface

The Runtime plugin path includes:

- ABI version, process exit, yield, and opaque handle lifecycle;
- stdin, stdout, and stderr descriptors;
- arguments and environment for UTF-8, UTF-16, and UTF-32;
- strict and WTF text handling;
- system and monotonic clocks;
- cryptographic randomness;
- preopens, descriptor metadata, seek/tell, synchronization, and sizing;
- sequential, positional, and vectored I/O;
- Memory32, Memory64, and GC-array buffer families;
- directory iteration and rewind;
- capability-beneath path operations;
- hard links, symbolic links, and readlink;
- IPv4 and IPv6 socket lifecycle operations;
- stream and datagram I/O;
- DNS resolution;
- level-triggered polling and monotonic timers;
- caller-typed allocating GC string and readlink results.

Facet uses normal Core WebAssembly linking for feature detection. The plugin does not substitute memory 0 for another memory index and does not copy GC arrays through a hidden linear-memory scratch area.

## Conformance

`wago-facet` runs the normative Facet 0.1 suite from a pinned `facet-spec` revision.

The current gate is:

```text
137 / 137 standard WAST tests   PASS
  6 /   6 harness tests         PASS
143 / 143 total tests           PASS
0 failures
0 crashes
0 timeouts
```

Each standard WAST file runs in an isolated subprocess. A runtime crash or timeout is therefore reported as a conformance failure instead of terminating the complete test run.

The six harness tests cover behavior outside standard WAST command composition:

- cross-instance handle isolation;
- process exit status;
- stdout and stderr validation;
- null nested-GC child validation before I/O;
- TCP loopback interaction;
- UDP loopback interaction.

See [`tests/conformance/README.md`](tests/conformance/README.md) for the pinned-suite and local-run instructions.

## Plugin configuration

A plugin selection can configure streams, environment entries, preopens, and the maximum number of live resource handles.

Example:

```json
{
  "stdin": "inherit",
  "stdout": "inherit",
  "stderr": "inherit",
  "env": ["MODE=development"],
  "preopens": [
    {
      "guest": "~",
      "host": "/srv/my-guest",
      "rights": ["stat", "dir-iterate"]
    }
  ],
  "maxHandles": 1024
}
```

The `~` name has no special ABI behavior. It is an ordinary Facet preopen display name.

If `rights` is omitted, a preopen receives `stat`, `path-open`, and `dir-iterate` rights.

## Guest capabilities

Every import declares a Wago guest capability. Important capability names include:

```text
facet.arguments.read
facet.environment.read
facet.clock.read
facet.random.read
facet.process.exit
facet.stdio.read
facet.stdio.write
facet.fd.manage
facet.filesystem.read
facet.filesystem.write
facet.network
facet.poll
```

An embedder can deny these capabilities through normal Wago policy.

## Development

Run the normal Go checks with:

```sh
go test ./...
go vet ./...
```

Run the complete Facet 0.1 conformance gate with the pinned suite as described in [`tests/conformance/README.md`](tests/conformance/README.md).

CI pins both the Wago revision used by the plugin and the Facet specification revision used by conformance testing.

## Runtime integration

See [`docs/wago-runtime-gaps.md`](docs/wago-runtime-gaps.md) for the Wago guest-storage requirements that enabled the complete Facet representation model and the remaining design rules that the plugin relies on.

## License

MIT
