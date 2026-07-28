# Runtime bootstrap prototype

This isolated Go module is the P0-05 feasibility proof. It uses only the Go
standard library and `crypto/ed25519`. The Plugin's Python `run.py` remains the
compatibility entry point; P5 packages one small bootstrap binary for each target
platform and `run.py` selects it without implementing cryptography in Python.

The bootstrap verifies the detached manifest signature before decoding fields,
then validates product version, full Plugin version, Git commit, SDK version, Tag,
route, platform, asset name, size, and SHA-256. A binary enters the cache only by
an atomic rename after all checks pass. A cache entry is rehashed on every launch.

Build identity and the public verification key are injected by the release job
with Go linker variables. An unset key is a hard failure. The command line exposes
no release URL or trust-root override.

This is not the current distribution path. P5 must package, sign, cross-platform
test, and canary the bootstrap before the production `run.py` delegates to it.
