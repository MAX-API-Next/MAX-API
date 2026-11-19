# API extension sources

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

Custom JavaScript or MAX JSON packages may be uploaded without an external
SHA-256: leave the optional field blank. MAX computes content digests and retains
its existing compilation, manifest, size and immutable-version checks. A supplied
hash is always checked; a mismatch is rejected. Computed digests identify bytes
and do not authenticate a publisher.

For a UTF-8 BOM-prefixed MAX JSON package, upgrade every catalog reader before
installation or activation, including format 1, and acknowledge `bom_nodes_ready`.
Validation alone does not establish that older nodes accept those retained bytes.
See the [BOM compatibility guidance](../pkg/taskplugin/management.md#utf-8-bom-packages).

For a separately downloaded official `plugin.js`, use the publisher's expected
SHA-256 from release metadata when available; otherwise leave the field blank.
Upload the exact published bytes:
Git's Windows line-ending conversion changes the hash. Extract the Git blob or
download the raw file at the pinned commit rather than hashing a converted
checkout and treating that new hash as the publisher's pin.

An installed version can be deleted directly from its management details;
disabling or revoking it first is not required. Deletion refuses historical task bindings, frozen task evidence, declared or
mapped channel models, and plugin pricing references. It removes the installed
source and version row without promoting another version. Distribution files
embedded in the program remain available in the bundled picker. A source-free
deletion receipt retains the identity and digest: identical bytes can be
explicitly reinstalled if the version was not historically revoked. A revoked
receipt continues to reject identical bytes. Changed bytes under the same
identity are always rejected.

Unversioned channel models and plugin-key prices conservatively reference every
matching installed version. Keeping another version with the same declaration
does not permit deletion; remove those references first. Channel configuration
can still use any retained non-revoked declaration, including a retired version.
This metadata existence rule does not transfer ownership or open paid routing.

Deletion prefilters task JSON by literal identities and Unicode escapes before
decoding. Potentially related corrupt rows still require repair; provably
unrelated corrupt rows no longer block the operation and are left unchanged.
This reduces transferred/decoded data but adds no reference index: native
database scanning and lock duration still depend on historical data size.
See the [management guide](../pkg/taskplugin/management.md) for compatibility
and isolated large-history validation requirements.

Before acknowledging deletion readiness, upgrade every catalog consumer,
channel writer, task writer **and pricing writer**, including workers that persist serialized
historical task references. Read the [management upgrade guide](../pkg/taskplugin/management.md)
before confirming readiness. Legacy receipts without a model index are readable
but excluded from model-name protection; reconcile their names from trusted
backups before relying on complete channel model protection.
The management UI and API no longer offer permanent revocation. Historical
revocation records from older releases remain enforced and are reported during
validation, before an installation attempt.

Before using deletion, upgrade every catalog node and let the master migrate
the deletion-receipt table, including its declared-model index. Channel writers
must be upgraded too: their model and mapping writes share the deletion lock
and reject references to deleted models. Old experimental receipts without the
index cannot identify model names from a digest alone. Confirm the upgrade in
the deletion dialog. Older nodes do
not understand authorized history removal and may suspend their entire plugin
host; rolling back to an older binary after deleting versions has the same risk.

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
supported `image` or `input_reference` field. Its Responses path accepts at most
two frame positions including metadata frames, preserves repeated list entries,
deduplicates redundant scalar aliases, and rejects overrides that discard
one of two submitted frames, and selects the action from the merged image fields.
An explicit metadata override of a single image remains supported, including in
Kling 4.0; it replaces the shorthand image rather than adding a second frame.
Kling 4.0 host protocols reject `metadata.contents`; use `image`/`image_tail` or
the native API for an explicit `contents` request.
xAI rejects a legacy `images`
list combined with scalar or `reference_images` input instead of discarding
one representation. These restrictions are enforced before authorization.

Metadata export and hook discovery use an isolated deterministic environment:
Date and SDK time are the Unix epoch, UUID is fixed, and Math.random is 0.5.
Execution hooks and their initialization retain frozen intrinsics and current
host time/randomness. Existing experimental format-3 catalogs must be revalidated before
upgrading; this does not migrate previously persisted nondeterministic metadata.
Direct MiniMax file lookup and Volcengine V4 signing remain unavailable SDK
capabilities and fail without external I/O; they are not certified integrations.

Pricing writers and deletion acquire the existing pricing-writer lock before
the catalog head; channel references use head-before-channel order. Pricing
validation reads current locked package rows rather than a prior MySQL snapshot.
The source-free declared-model index uses LONGTEXT on MySQL and TEXT on SQLite
and PostgreSQL, with ASCII storage encoding for legacy character sets. Migrate
existing experimental index columns before using deletion on upgraded nodes.
The decoded JSON model index is bounded to 6 MiB to cover worst-case escaping
for every accepted 1 MiB package; the upload limit stays unchanged. Storage keeps
the existing ASCII/base64 encoding and accepts old raw/encoded receipts. Upgrade
all catalog/channel writers before creating indexes above the old 1 MiB limit.
Maximum encoded indexes are about 8 MiB plus receipt/SQL overhead; ensure MySQL
client and server packet limits can carry them (16 MiB or higher for this write).
See the [packet-size guidance](https://dev.mysql.com/doc/refman/8.0/en/packet-too-large.html).
