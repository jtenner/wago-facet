# Security policy

## Supported versions

| Version | Supported |
| --- | --- |
| `main` (pre-release) | Yes |

No tagged release is currently published. The planned `wago-facet` 0.1 release is experimental. Security fixes can change implementation details, limits, diagnostics, and external error normalization while preserving the Facet 0.1 ABI and normative behavior.

## Reporting a vulnerability

Report a vulnerability through GitHub private vulnerability reporting for `jtenner/wago-facet` when it is available.

If private reporting is unavailable, contact the repository owner privately instead of opening a public issue with exploit details.

Useful reports include:

- the affected commit or release;
- the guest module or smallest reproducer;
- required plugin configuration and capability grants;
- the observed authority escape, memory-safety failure, denial of service, or isolation failure;
- whether the issue reproduces on Linux amd64, Linux arm64, or both.

Do not include secrets, unrelated host data, or destructive public proof-of-concept instructions.

## Security boundaries

Facet resources are capabilities. A report is security-relevant when a guest can exceed configured authority, reuse stale or cross-instance handles, escape a preopen, retain guest-storage access after a callback, bypass structural import validation, or cause an unbounded runtime-owned allocation or wait.
