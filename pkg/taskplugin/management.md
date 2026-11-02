# Task plugin management

The console's **Task Plugins** page manages MAX package documents and retained
versions. It does not execute uploaded JavaScript, enable a provider route or
open paid admission. Declared models/protocols are metadata, not runtime support.
The existing model/source prices remain under System Settings → Billing → Model
Pricing; legacy task cards and Go execution paths retain their own profiles.

All management routes require fresh Root authentication:

| Route | Behavior |
| --- | --- |
| `GET /api/task_plugins` | Refresh and return catalog revision plus metadata for all retained versions; no source bytes |
| `POST /api/task_plugins/validate` | Validate `{document, expected_sha256}` without installing or executing it |
| `POST /api/task_plugins/mutate` | Apply exactly one version-guarded administrative operation |

Validation requests may be recorded by the normal administrative audit middleware;
they do not create/update catalog entries. Mutation audits contain operation,
key/version and resulting revision, never the document/source. Error responses
do not include validator/driver internals. Audit logging follows the existing
management audit mechanism; it is not a new transactional audit outbox.

`document` is the exact JSON text of a MAX `PackageDocument`, not an upstream
`.js` file. Its complete byte stream is limited to 1 MiB and must match a SHA-256
obtained from a trusted source. Computing a hash from the upload alone proves
integrity, not publisher authenticity. The bounded JSON request permits escaping
overhead; unknown/null/incorrectly typed outer fields are rejected.

Every mutation includes `expected_revision` as a canonical decimal **string**
(including `"0"` for the empty catalog). Install also includes `document` and
`expected_sha256`; other operations include `key` and `version`:

- `install` retains immutable bytes, makes this version active and enabled.
- `activate` activates an existing non-revoked version and enables it.
- `enable` / `disable` apply only to the specified currently active version.
- `revoke` irreversibly rejects this version, including historical sessions.

Disabled/retired versions remain available for valid historical sessions until
revoked. There is no delete/force-overwrite endpoint. A key/version cannot be
reused with different bytes, even when its declared schema matches.

**Upgrade all Catalog consumers before installing a format-2 package.** Older
nodes reject format 2 and can suspend their entire local Host. Installing or
activating format 2 requires `format2_nodes_ready: true`; this is an operator
acknowledgement, not automatic multi-node compatibility verification. Management
status and a successful local publication do not prove a global cutover.

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
