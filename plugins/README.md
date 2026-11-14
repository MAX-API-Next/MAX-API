# Official task plugin sources

The original JavaScript releases under `tasks/` are by QuantumNous from
[new-api-plugins](https://github.com/QuantumNous/new-api-plugins), licensed under
Apache-2.0. The full license is in [tasks/LICENSE](tasks/LICENSE).

`SOURCES.json` records the exact upstream commit, paths, versions and published
SHA-256 pins. Sources are copied from Git blobs, preserving the published bytes
(a Windows checkout may use different line endings). Do not edit a published
script in place; adopt a new reviewed version and retain the previous identity.

Versions containing `-maxapi.` are explicit MAX-API-Next compatibility revisions,
not unmodified upstream releases. `SOURCES.json` records their original version
and SHA-256, modified source SHA-256, maintainer and individual changes. Original
files remain byte-for-byte intact in `retainedVersions`; existing pinned download
references continue to work. The picker recommends the compatible revisions.
Upgrading an installed package remains an explicit catalog operation; old tasks
keep their original package identity and are not silently repriced or rewritten.

Bundling does not install, activate or certify a plugin's models, SDK features,
provider access or paid settlement. Installation and deployment validation are
separate operations. Upstream usage guides and changelogs are available at the
recorded repository and commit beside each original source.

The runtime uses MoeJS v0.1.0 (Apache-2.0) and moego v0.1.0-alpha.3 (MIT).
Their license and third-party notice texts are retained in `runtime-licenses/`
and embedded with the sources. The SDK host is independently implemented from
the published API v1 contract; the upstream gateway host is not vendored.

The host currently implements a bounded JSON submit/query driver, including
native and declared protocol decoding, selected-channel identity checks and
immutable package bindings. Outbound I/O uses the host's protected HTTP policy
and an independent authorization callback. An accepted request whose response
is lost is never automatically resubmitted. Old tasks retain their pinned
package for queries; revocation blocks further hook calls and sends.

Query hooks may use GET, HEAD or POST (for providers such as Suno and Vertex).
They still require the original task's ownership/binding and host authorization;
this does not permit POST artifact downloads or enable paid task admission.

This is a staged SDK implementation, not a complete API v1 or paid gateway.
Multipart/files, SSE, batch polling, OAuth/signing variants, artifact delivery
and the durable task/funding caller still require integration and validation.
Protected destination DNS with a channel proxy is currently refused rather
than bypassed. Installation readiness remains separate from these SDK helpers.

The MAX compatibility revision of xAI refuses video edits and extensions before
authorization: their output-resolution pricing requires a bounded media probe,
and this SDK does not implement `utils.fetch`. It does not silently turn the
720p reservation cap into measured usage. Generation remains available within
the JSON SDK boundary. Retained upstream versions are historical evidence,
not recommendations to bypass this limitation.

SDK non-proxy requests retain a protected dialer even when protection is off,
revalidate policy immediately before transport, and disable connection reuse so
connections opened under an older policy cannot bypass a later policy change.
Proxy brokers also reject sends if protection has since been enabled.

Synchronous image paths that require a host-issued gateway task ID, and remix
paths that require an ownership-checked origin task, are refused before dispatch
until the durable task caller supplies those contexts. Accepted image/frame
counts are validated rather than silently truncated. Profile inheritance is
bounded before Go map expansion (8,192 fields and 1 MiB serialized schemas).

The recommended Google and Vertex video revisions accept one output per task:
missing counts default to one, and explicit `sampleCount` or `numberOfVideos`
values other than the integer one are rejected rather than rewritten. Jimeng
compatibility requests with both URL and Base64 frames are rejected before
submission because splitting them into separate arrays loses first/tail order.
Use a single ordered representation until an owned conversion path is available.
Kling's OpenAI Video JSON path also rejects nonempty `images` lists; use its
supported `image` or `input_reference` field. xAI rejects a legacy `images`
list combined with scalar or `reference_images` input instead of discarding
one representation. These restrictions are enforced before authorization.

Metadata export and hook discovery use an isolated deterministic environment:
Date and SDK time are the Unix epoch, UUID is fixed, and Math.random is 0.5.
Execution hooks and their initialization retain frozen intrinsics and current
host time/randomness. Existing experimental format-3 catalogs must be revalidated before
upgrading; this does not migrate previously persisted nondeterministic metadata.
Direct MiniMax file lookup and Volcengine V4 signing remain unavailable SDK
capabilities and fail without external I/O; they are not certified integrations.
