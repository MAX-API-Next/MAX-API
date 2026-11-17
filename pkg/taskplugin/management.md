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

The validation response returns the stored package `digest`, and executable
format-3 uploads also return `source_sha256` for the raw JavaScript bytes. A
locally computed digest records content identity; it does not authenticate a
publisher. Internal/persisted package loading and bundled source pins remain
mandatory, independently of the optional upload field.

Every mutation includes `expected_revision` as a canonical decimal **string**
(including `"0"` for the empty catalog). Install also includes `document` and
optional `expected_sha256`; other operations include `key` and `version`:

- `install` retains immutable bytes, makes this version active and enabled.
- `activate` activates an existing non-revoked version and enables it.
- `enable` / `disable` apply only to the specified currently active version.
- `delete` directly removes an installed version when no task/channel/pricing
  references exist; prior disabling or revocation is not required. It requires
  `deletion_nodes_ready: true` after upgrading all nodes. Source-free audit
  receipts and embedded distribution files remain available.

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

Stale revisions return HTTP 409. Storage/unknown outcomes return 503 and suspend
the local Host until a successful refresh. Writes are not automatically retried.
The client rereads authoritative state and retains an unknown-result notice;
even matching state cannot prove the earlier request failed or finished. A
successful receipt contains `committed: true`, its exact resulting revision and
`execution_ready: false`, `admission_ready: false`.

Interaction/protocol reference: QuantumNous/new-api `70e6af64`, task-plugin
list/upload/version/detail workflows (AGPL-3.0). MAX uses its independent pinned
transport, catalog CAS and retained history. Marketplace/URL download, source
editing, JavaScript debugging and upstream execution code are outside this API.
