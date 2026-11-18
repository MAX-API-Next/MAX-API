# API extension management

The console's **API Extensions** page manages MAX package documents and retained
versions. Validation compiles JavaScript, reads bounded metadata and checks the
current catalog for revoked identities, changed content and route conflicts;
it never writes the catalog and does not reserve a revision. Installation still
requires the observed revision and CAS. Validation does not
enable a provider route or open paid admission. Declared models/protocols are
metadata, not runtime support.
The existing model/source prices remain under System Settings → Billing → Model
Pricing; legacy task cards and Go execution paths retain their own profiles.

API Extensions is the console name. Existing `/task-plugins` URLs,
`/api/task_plugins` endpoints and stored `task_plugins` configuration keys retain
their identifiers for compatibility.

**Upgrade every task writer before relying on deletion protection.** Serialized
task updates now validate recursive legacy map/array references as well as typed
bindings and frozen plans under the catalog-head lock. Unknown fields without
references remain writable; omitted fields do not participate in the check.

File uploads preserve a leading UTF-8 BOM and line endings. The server accepts a
leading BOM in JSON packages while hashing and retaining the original bytes.
Installation and activation require `bom_nodes_ready: true` for these packages,
including format 1. Upgrade every catalog reader first: previous loaders reject
the BOM and can suspend their entire Host. The console asks for this readiness
acknowledgement separately from the upload's optional publisher hash.
Previously installed text normalized by older clients keeps its immutable
identity; uploading the original BOM-bearing file does not overwrite that
identity. No migration rewrites existing packages or their digests.

Back up and restore the catalog head, all retained packages and all deletion
receipts as one consistent set. The last-deletion marker detects loss of the
entire receipt set; it is not a fingerprint of every individual receipt and
cannot certify a partially restored database. No normal catalog operation may
drop receipts. Restore consistency must be established before serving traffic.

All management routes require fresh Root authentication:

| Route | Behavior |
| --- | --- |
| `GET /api/task_plugins` | Refresh and return catalog revision plus metadata for all retained versions; no source bytes |
| `POST /api/task_plugins/validate` | Validate `{document, expected_sha256?}` without installing or calling execution hooks |
| `POST /api/task_plugins/mutate` | Apply exactly one version-guarded administrative operation |

Validation requests may be recorded by the normal administrative audit middleware;
they do not create/update catalog entries. Mutation audits contain operation,
key/version and resulting revision, never the document/source. Error responses
do not include validator/driver internals. Audit logging follows the existing
management audit mechanism; it is not a new transactional audit outbox.

`document` is the exact text of a MAX `PackageDocument` JSON or raw `plugin.js`,
limited to 1 MiB. `expected_sha256` is optional: omit it or send an empty string
to import a custom plugin without a publisher-provided hash. The server computes
the actual upload digest and then uses the same strict loader, manifest checks,
sandbox and immutable stored identities. If a nonempty SHA-256 is supplied, it
must be valid and match; a mismatch never falls back to optional mode. `null`,
incorrect types and unknown fields remain invalid. The bounded JSON request
permits escaping overhead.
Raw JavaScript must also fit within the 1 MiB generated package limit: wrapping
the source and manifest in JSON adds escaping and metadata overhead, so a source
file below 1 MiB can still exceed the stored-package bound. Local files must be
valid UTF-8; invalid byte sequences are rejected rather than silently replaced.

The validation response returns the stored package `digest`, and executable
format-3 uploads also return `source_sha256` for the raw JavaScript bytes. A
locally computed digest records content identity; it does not authenticate a
publisher. Internal/persisted package loading and bundled source pins remain
mandatory, independently of the optional upload field.
The console binds a bundled document to its supplied pin. Selecting a custom
file clears that bundle pin; a manually entered custom-file pin stays editable.

Every mutation includes `expected_revision` as a canonical decimal **string**
(including `"0"` for the empty catalog). Install also includes `document` and
optional `expected_sha256`; other operations include `key` and `version`:

A channel reference or pricing write can initialize a valid empty catalog at revision 1.
Reread the revision before installing if an earlier read observed revision 0;
that earlier revision correctly conflicts rather than replacing the bootstrap.

- `install` retains immutable bytes, makes this version active and enabled.
- `activate` activates an existing non-revoked version and enables it.
- `enable` / `disable` apply only to the specified currently active version.
- `delete` directly removes an installed version when no task/channel/pricing
  references exist; prior disabling or revocation is not required. It requires
  `deletion_nodes_ready: true` after upgrading all nodes. Source-free audit
  receipts and embedded distribution files remain available.

Channel creates and model/mapping updates acquire the same catalog-head lock as
deletion, including batch, tag, Save and map-based writes. Deletion receipts now
include a source-free JSON index of declared models (`models`: LONGTEXT on MySQL,
TEXT on SQLite/PostgreSQL); migrate
this column on the master and upgrade every channel writer before relying on
the guard. The guard rejects late references to deleted models, permits an
explicit identical reinstallation, and leaves unrelated channel patches alone.
Old experimental receipts without this index remain readable, but names of
already deleted models cannot be reconstructed from a digest alone.
These legacy receipts are explicitly excluded from model-name protection until
their model indexes are reconciled from trusted backups; do not claim complete
channel coverage for an unreconciled legacy catalog. The current channel guard
scans indexed receipts under the head lock, so deletion history size affects
channel-write latency. A durable per-model lookup/backfill remains separate
scalability work; the current implementation does not provide indexed lookup.

The console and management API no longer expose `revoke`; legacy requests are
rejected without committing. Existing revocation records remain readable for
runtime compatibility and are reported before installation. Ordinary deletion
does not create a permanent revocation; the identical file can be reimported.

Disabled/retired versions remain available for valid historical sessions until
explicitly deleted without references (historically revoked versions remain
blocked). There is no force-overwrite
operation. A key/version cannot be
reused with different bytes, even when its declared schema matches.

**Upgrade all Catalog consumers before installing a format-2 package.** Older
nodes reject format 2 and can suspend their entire local Host. Installing or
activating format 2 requires `format2_nodes_ready: true`; this is an operator
acknowledgement, not automatic multi-node compatibility verification. Management
status and a successful local publication do not prove a global cutover.

Executable format-3 installation/activation similarly requires
`format3_nodes_ready: true`. Omitting an upload hash does not waive either
compatibility acknowledgement, Root authorization or the revision guard.

Stale revisions return HTTP 409. Unconfirmed storage outcomes return 503 and suspend
the local Host until a successful refresh. Writes are not automatically retried.
The client rereads authoritative state and retains an unknown-result notice;
even matching state cannot prove the earlier request failed or finished. A
successful receipt contains `committed: true`, its exact resulting revision and
`execution_ready: false`, `admission_ready: false`.

A store-confirmed deletion still returns its committed receipt when the local
follow-up refresh fails. The local Host remains suspended until an authoritative
read succeeds; that publication failure does not make the confirmed deletion
unknown or suppress its management audit. A store that does not confirm the
commit still produces the existing unknown-result error, without automatic replay.

Interaction/protocol reference: QuantumNous/new-api `70e6af64`, task-plugin
list/upload/version/detail workflows (AGPL-3.0). MAX uses its independent pinned
transport, catalog CAS and retained history. Marketplace/URL download, source
editing, JavaScript debugging and upstream execution code are outside this API.

Pricing writers and deletion acquire the existing pricing-writer lock before
the catalog head; channel references use head-before-channel order. Pricing
validation reads current locked package rows rather than a prior MySQL snapshot.
Deletion opens a fresh transaction from the root database handle; a store backed
by an already active transaction is rejected to avoid inheriting an old read
view. Schema preflight runs before that transaction, and ordinary reference
reads follow the pricing/head locks. Task references are decoded with the same
typed, case-insensitive JSON field rules as persistence, while recursive legacy
map/array checks inside the private-data object remain supported. New serialized
non-null task writes still require an object, matching the typed task/recovery decoder;
accepting an array at the root would create an unreadable task record.
Valid task defaults that depend on the removed package are checked under the
same pricing lock. Deletion cannot newly invalidate such a default, but remains
allowed when another declared source satisfies it. Prices are never rewritten.

The source-free declared-model index uses LONGTEXT on MySQL and TEXT on SQLite
and PostgreSQL, with ASCII storage encoding for legacy character sets. Migrate
existing experimental index columns before using deletion on upgraded nodes.
The serialized JSON model index is limited to 1 MiB before storage encoding.
JSON escaping can expand model names beyond the uploaded package size. If the
index exceeds this bound, deletion is rejected and the transaction preserves
the installed version and catalog revision; it never commits an unreadable receipt.
