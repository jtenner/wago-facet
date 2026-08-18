# wago-facet

`wago-facet` is the reference [Wago](https://github.com/wago-org/wago) plugin for [Facet](https://github.com/jtenner/facet-spec).

Facet is a small system interface for Core WebAssembly. The import module is `"facet"`.

This repository follows Facet 0.1.

## Status

The plugin is experimental.

The current Wago host-call API can implement the scalar and resource parts of Facet exactly. It cannot yet expose an arbitrary guest memory by memory index. It also cannot expose a general WebAssembly GC array to a host callback.

For this reason, this plugin does **not** register an import when Wago cannot implement that import correctly.

Facet uses normal Core WebAssembly linking for feature detection. A missing import therefore means that the operation is not available.

The plugin does not silently use memory 0 for another memory index. It does not copy GC arrays through a scratch memory.

## Implemented imports

The current implementation includes:

- Facet ABI version and handle close;
- process exit and cooperative yield;
- standard stream handles;
- argument and environment counts and width-specific length queries;
- system and monotonic clocks;
- sleep operations;
- scalar cryptographic randomness;
- filesystem preopen count, handle, and display-name length queries;
- descriptor rights, flags, and metadata;
- descriptor positioning and synchronization error semantics;
- directory iterator creation, length snapshots, and rewind;
- IPv4 and IPv6 socket lifecycle operations;
- blocking and nonblocking connect state;
- bind, listen, accept, local address, peer address, and shutdown;
- level-triggered descriptor polling;
- one-shot monotonic timers;
- snapshot-based `poll_wait` and `poll_next` behavior.

## Imports intentionally absent

The following representation families are not registered yet:

- `_mem32` guest-buffer operations;
- `_mem64` guest-buffer operations;
- `_array_*` guest-buffer operations;
- caller-typed GC allocation results;
- path operations that need guest string storage;
- DNS resolution operations that need a guest string;
- datagram payload operations.

These imports need Wago to provide callback-scoped indexed-memory or GC-array access.

See [`docs/wago-runtime-gaps.md`](docs/wago-runtime-gaps.md) for the required runtime hooks.

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

## Direct low-level use

The preferred integration is `Provider()` through Wago's plugin system.

`Imports(Config)` is also available for low-level embedders. It creates one stateful import bundle for one guest instance. Call `Imports` again for another instance.

## Development

Run:

```sh
go test ./...
go vet ./...
```

CI pins the Wago commit used by this implementation.

## License

MIT
