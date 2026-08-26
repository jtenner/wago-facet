# Changelog

All notable changes to `wago-facet` are recorded in this file.

## [0.1.0] - 2026-08-26

### Added

- Complete 261-import Facet 0.1 Runtime plugin surface.
- Memory32, Memory64, multi-memory, and Wasm GC-array representations.
- Exact structural validation for abstract GC-array parameters and caller-selected array results.
- Filesystem, links, sockets, DNS, polling, clocks, randomness, arguments, environment, and standard I/O.
- Capability-beneath Linux filesystem resolution and pinned preopen authority.
- Complete 143-case Facet conformance execution on Linux amd64 and arm64.
- Canonical import inventory and full-module instantiation gates.
- Forced moving-GC conformance execution.
- Cross-representation differential integration coverage.
- Race-detector and minimum-Go-version CI jobs.
- Seed corpora for text-codec and plugin-configuration fuzz targets.

### Security

- Opaque instance-local resource handles reject stale and cross-instance reuse.
- Guest storage borrows are callback-scoped and cannot outlive an imported call.
- Guest-controlled runtime allocations have finite limits.
- Preopen authority is pinned before guest observation.
- DNS resolution has a finite deadline and plugin-shutdown cancellation.

[0.1.0]: https://github.com/jtenner/wago-facet/releases/tag/v0.1.0
