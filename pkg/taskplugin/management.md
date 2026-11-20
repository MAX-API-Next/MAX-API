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

**Writer grant and throughput compatibility:** steady-state PostgreSQL writers
need INSERT on the catalog head and UPDATE on its `mutation_id`, not just the
privileges for locking reads. Provision these existing write requirements before
rollout. Applied recovery now uses at most 50 successful updates per sweep,
reserving at least 50 of the shared 100 processing slots for ordinary timeouts.
Idle ordinary capacity is not borrowed back, so applied-only recovery throughput
can be lower than before. The independent 2,000-row scan caps remain unchanged.
PostgreSQL runtime connections must also put the application schema first on
`search_path`, matching the migration schema and the unqualified application
queries. Current GORM table/column preflights use `CURRENT_SCHEMA()`; a table
visible only through a later schema can fail those checks even when its DML
grants exist. Apply this connection configuration to all catalog readers and
writers before rollout, and verify it using the actual runtime roles. This
change documents the required deployment contract; it does not alter roles,
schemas or search paths on the business database.

**Existing subscription refund limit:** a legacy task reserved at 20 and reduced
to 15 by request finalization can later have its timeout refund of 15 held for
manual reconciliation against the original pre-consume record of 20. The legacy
timeout may already show `FAILURE` while that credit is manual; inspect the
settlement state and remaining task quota, not status alone. This change keeps
the existing financial coverage guard and does not implement automatic
reconciliation for this case. Validate the original reservation, request-finalize
and refund records in isolated business acceptance before rollout.

Deletion now prefilters historical task JSON for the target identity before
decoding it. A corrupt row proven unrelated to that identity no longer blocks
its deletion; candidate rows with invalid JSON or typed data still require
repair. This changes the scope of the deletion precondition, not worker recovery:
unreadable tasks remain isolated and require trusted repair. The prefilter is
not indexed, and large-history lock duration still needs deployment validation.

Package loading, compilation/metadata checks and catalog operations share the
administrative operation's 10-second context, starting before candidate loading.
The existing JavaScript per-call execution limits remain secondary bounds.
Catalog mutex waits use that context too; an operation canceled while waiting
does not read storage or unpublish an otherwise healthy Host.
Upload preflight also preserves an unchanged published Host when its own
catalog read/metadata budget expires. A conflicting, invalid or changed
authoritative generation remains fail-closed; this never restores a Host after
such evidence or an unknown write. Activation, enable/disable and retained
version checks propagate the same operation context through every reload.

After deletion verifies the expected locked head, expiration during preparation
or reference checks before the first catalog mutation returns HTTP 503 with
`code: "catalog_operation_not_committed"` and
`committed: false`, preserving the healthy Host. Refresh before retrying.
A newer generation or an error after a mutation attempt still suspends the
Host until an authoritative refresh; an error alone cannot prove non-commit.
CAS input validation that ends before opening its database transaction also
returns the explicit no-write sentinel; the manager preserves the published
Host only for that result, not a bare context error after a write attempt.
After a successful complete read, an exact snapshot match reuses the validated
Host only when that generation is still published. Changed, conflicting or
invalid reads retain validation and suspension behavior. No suspended Host is
restored merely because the old snapshot matches.

## UTF-8 BOM packages

File uploads preserve a leading UTF-8 BOM and line endings. The server accepts a
leading BOM in JSON packages while hashing and retaining the original bytes.
New JSON imports require each member name to appear once per object, including
objects nested inside the manifest. Packages already recorded in a catalog keep
their legacy JSON interpretation, original bytes and digest across upgrades.
Installation and activation require `bom_nodes_ready: true` for these packages,
including format 1. Upgrade every catalog reader first: previous loaders reject
the BOM and can suspend their entire Host. The console asks for this readiness
acknowledgement separately from the upload's optional publisher hash.
Previously installed text normalized by older clients keeps its immutable
identity; uploading the original BOM-bearing file does not overwrite that
identity. No migration rewrites existing packages or their digests.

## Backup and restore

Back up and restore the catalog head, all retained packages and all deletion
receipts as one consistent set. The last-deletion marker detects loss of the
entire receipt set; it is not a fingerprint of every individual receipt and
cannot certify a partially restored database. No normal catalog operation may
drop receipts. Task, channel and pricing records must come from the same recovery
point or be reconciled against the restored catalog before traffic resumes.
An old receipt does not prove that a reinstalled version was deleted again.
Do not use a partially restored set as proof of valid history.
Stop every catalog reader and writer before restoring an older backup. Restore
the consistent set, create fresh Catalog instances, and verify their load before
resuming traffic. A live instance remembers its highest observed revision and
will reject a rollback; repeated Refresh calls cannot reset that history.

## Root management API

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

Deletion also checks malformed historical data before it can prove that no
references remain. An unrelated channel with an invalid model mapping blocks
deletion with HTTP 422, `code: "invalid_channel_mapping"` and a decimal-string
`channel_id`. The console also accepts safe numeric IDs from older servers.
Candidate historical task JSON that is invalid, or an object that cannot be
decoded into typed task private data, blocks it with HTTP 422,
`code: "invalid_task_data"` and the decimal-string database row `task_id`.
Historical root arrays are checked recursively for references; an array with
no matching reference is not rejected solely because the typed decoder cannot
read it. New serialized writes still reject root arrays.
Malformed plugin pricing returns HTTP 422, `code: "invalid_plugin_pricing"`
and the public `option_key`; no stored expression or option value is returned.
Repair the identified record before retrying; these precondition errors do not
remove the package or suspend an otherwise healthy local Host, and never expose
the record's private payload. MySQL and PostgreSQL writers must have access to
the deletion table: a table hidden by role privileges is an error, not an absent
optional catalog. MySQL confirms an apparently absent table with a direct empty
SELECT; only error 1146 permits the legacy missing-table path. Other errors,
including 1142 (access denied), block the write.

The current PostgreSQL writer paths use `SELECT ... FOR UPDATE` on catalog-head,
retained-version and deletion-receipt rows. In addition to SELECT, PostgreSQL
requires UPDATE privilege on at least one column of each table being locked,
even when the receipt table is empty. A SELECT-only catalog reader role is not a
writer role. Provision the required lock/write privileges before serving writes;
the head bootstrap/update paths also require their INSERT/UPDATE privileges.
The current head-lock helper issues `INSERT ... ON CONFLICT DO NOTHING` on
every call, even when the head already exists. Every PostgreSQL channel, task
and pricing writer using that helper therefore needs INSERT on the head's
written columns and UPDATE on `mutation_id` for its no-op write, as well as the
SELECT/locking privileges above. INSERT is not a bootstrap-only grant. Keep
these existing bootstrap/race/orphan checks and provision the steady-state
writer grants before rollout; management mutations need their additional
operation-specific grants. See the [PostgreSQL INSERT privilege requirements](https://www.postgresql.org/docs/9.6/sql-insert.html).
See the [PostgreSQL SELECT privilege requirements](https://www.postgresql.org/docs/current/sql-select.html).

Channel creates and model/mapping updates acquire the same catalog-head lock as
deletion, including typed batch creates, tag/Save writes and map updates. The
application DB initializer rejects map and slice-of-map channel creates, because
GORM skips model hooks for those destinations. Use typed `Channel` creates;
upgrade every writer before relying on this guard. Custom DB handles must install
the same guard; raw SQL and explicit `SkipHooks` writes remain unsupported.
Typed upserts also validate explicit `models` and `model_mapping` conflict
assignments. Literal strings are supported; dynamic SQL assignments to these
protected columns are rejected because their resulting references cannot be
certified. Metadata-only conflict updates and ordinary typed `UpdateAll` remain
supported using their already-validated inserted values.
Deletion receipts now
include a source-free JSON index of declared models (`models`: LONGTEXT on MySQL,
TEXT on SQLite/PostgreSQL); migrate
this column on the master and upgrade every channel writer before relying on
the guard. The guard rejects late references to deleted models, permits an
explicit identical reinstallation, and leaves unrelated channel patches alone.
Task binding, frozen-plan and pricing-envelope identities remain reference
containers. The known `billing_context.task_billing.fields` and `matched` maps
contain arbitrary billing facts, so their values are not interpreted as package
identities; other historical reference objects remain recursively checked.
Serialized TEXT and binary task private-data updates decode into the same fresh
typed metadata, preserving the original binding and funding identity on reload.
Old experimental receipts without this index remain readable, but names of
already deleted models cannot be reconstructed from a digest alone.
These legacy receipts are explicitly excluded from model-name protection until
their model indexes are reconciled from trusted backups; do not claim complete
channel coverage for an unreconciled legacy catalog. The current channel guard
scans indexed receipts under the head lock, so deletion history size affects
channel-write latency. A durable per-model lookup/backfill remains separate
scalability work; the current implementation does not provide indexed lookup.
Bulk channel statements validate the union of emitted model/mapping references
once under the same head lock and transaction. This includes Select/Omit rules;
an invalid member rejects the whole statement. Only later row callbacks in that
create execution can reuse its check. The first row always revalidates, including
when the same initialized GORM handle is reused. Separate executions, chunks and transactions reread the
history; there is no cross-transaction cache or constant-time receipt lookup.
Any retained non-revoked declaration intentionally satisfies this metadata
reference check, including inactive or disabled versions. This preserves channel
configuration and does not prove an active route, executor or paid admission.
Deletion uses a stricter ownership rule: an unversioned channel model or
plugin-key pricing override can refer to every matching installed version.
For example, if v1 and v2 declare the same model, its channel reference blocks
deletion of either version, even when the other is retained. A key-scoped price
similarly blocks each version of that key. Another declaration does not transfer
or version-pin these references; remove them before deleting the version.

The console and management API no longer expose `revoke`; legacy requests are
rejected without committing. Existing revocation records remain readable for
runtime compatibility and are reported before installation. Ordinary deletion
does not create a permanent revocation. An identical file that passes current
upload validation can be reimported. Validate the original file before deleting
if you may need to reinstall it. Some retained legacy packages have repeated JSON
members that current upload validation rejects. Editing those members changes
the digest, so the edited file cannot reuse the same key and version.

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
unknown or suppress its management audit. When the outcome of a deletion attempt
cannot be established, the console requires a fresh read before another write.
Known rejections show their repair or conflict response directly.

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
Before decoding, SQL selects rows containing the complete quoted package key,
the complete quoted digest, their opening-quote prefixes without the closing
quote, or any JSON Unicode escape marker (`\u`). Canonical
identity strings must use a literal or Unicode escapes, so escaped identities,
case variants of field names and unknown legacy containers remain candidates.
The original typed and recursive checks then decide whether a reference exists.
SQLite compares TEXT and BLOB values using their respective native types, also
preserving UTF-16 TEXT alongside UTF-8 blobs. A disjoint corrupt row is left
unchanged. This reduces JSON transfer and Go decoding under the existing locks;
the native database predicate still scans history and may be O(task count).
Many matching identities or unrelated Unicode escapes can retain many rows.
No durable reference index or backfill is added, and this is not a guarantee of
bounded lock time. Validate large histories and real database isolation/roles
in an isolated environment before relying on deletion latency.
Typed billing fact containers (`task_billing.fields`/`matched`,
`pricing_usage_envelope.facts` and `task_billing_plan.expression_plan.submit_facts`
within `billing_context`) are data, even when a declared fact is named
`package_digest` or `plugin_key`. Adjacent package identity fields remain strict.
Known fact paths use the typed JSON decoder's Unicode case folding, including
spellings such as `factſ`. Conflicting case aliases for `plugin_key`,
`selected_plugin_key` or `package_digest` in identity containers are invalid,
even if one alias uses the canonical spelling. New writes reject them; candidate
historical rows block deletion with the existing repairable `invalid_task_data`
response. Equal aliases and single case variants remain supported. Conflicting
case variants such as `version` and `VERSION` likewise require repair. Distinct
fields `version` and `plugin_version` with different values yield a conservative
key-only reference.
Valid task defaults that depend on the removed package are checked under the
same pricing lock. Deletion cannot newly invalidate such a default, but remains
allowed when another declared source satisfies it. Prices are never rewritten.

The source-free declared-model index uses LONGTEXT on MySQL and TEXT on SQLite
and PostgreSQL, with ASCII storage encoding for legacy character sets. Migrate
existing experimental index columns before using deletion on upgraded nodes.
The serialized JSON model index is limited to 6 MiB before storage encoding,
covering the worst-case six-byte JSON escape expansion of every accepted 1 MiB
package. Readers and writers use the same bound and existing ASCII/base64
encoding; older raw JSON and encoded receipts remain readable. Upgrade every
catalog and channel writer before deleting versions whose indexes exceed the
older 1 MiB reader limit. The upload/package limit remains 1 MiB.
The maximum index occupies about 8 MiB after base64, plus the receipt and SQL
overhead. Ensure MySQL client and server packet limits can carry it; 16 MiB or
higher leaves room for this write. See MySQL's [packet-size guidance](https://dev.mysql.com/doc/refman/8.0/en/packet-too-large.html).
Workers isolate unreadable task private data, log the database row ID, and
continue valid neighbors. Timeout cursors advance past bad rows; those rows
retain their status, quota and original metadata without a new settlement.
Repair their metadata from trusted evidence before expecting further processing.
Applied-finalize recovery uses its own `(submit_time, id)` cursor and captures a
fixed inclusive upper cursor for each pass. It wraps after reaching that bound
or an empty page, so a sustained newer backlog cannot starve repaired older rows.
The per-cycle scan allowance is unchanged. Applied-finalize timeout recovery now
uses at most 50 transitions per sweep. Ordinary video polling can also recover
already funded terminal evidence before contacting the provider, even when task
timeouts are disabled. Recovery matches the stored status, version and quota.
It preserves the stored provider payload and never replays funding.
When an applied finalize record has no recoverable pending terminal status,
recovery can instead mark the nonterminal task `FAILURE` with a manual
reconciliation reason. Funding remains applied and is not replayed or refunded
by this disposition; reconcile the missing provider evidence separately.
Ordinary timeout decisions compare the observed `updated_at` and quota with status.
A same-status submission update makes the old decision lose without a terminal
transition or settlement intent; the next sweep reevaluates the newer task.
Provider progress writes use the same snapshot predicates and advance the
version even when the status stays the same, so an older poll cannot restore
prior provider data or make a stale timeout eligible again.
The real Relay confirmation writer advances `updated_at` atomically from its
current database value, even when its in-memory task or wall clock is older.
This version update, upstream identity and existing settlement intent commit or
roll back together. Duplicate confirmation retains the original intent key;
overflow or failed version readback cannot release a successful confirmation.
Request funding can project a changed task quota and set its timestamp back to
wall time. The quota predicate still rejects the old timeout in that case;
zero-delta projections leave the confirmation version intact. Funding SQL and
the original operation identities remain unchanged.
Each recovery/timeout class scans at most 2,000 rows per invocation by default;
both share the existing cap of 100 transition/processing slots. This permits up to
4,000 scans when both classes contain many unreadable or otherwise skipped rows.
Applied recovery now uses at most 50 successful updates from that shared budget,
leaving at least 50 processing slots for ordinary timeouts in the same sweep.
Unused ordinary slots are not donated back to recovery; an applied-only backlog
can therefore recover at most 50 tasks per invocation instead of 100. Failed
decoding or CAS contention can reduce committed work further. Both existing
2,000-row scan caps and independent cursors remain; no funding is replayed by
this scheduling change.

User/admin task lists isolate unreadable private metadata per row and retain
healthy neighbors with the same SQL filtering, pagination and user/channel
visibility rules. Unreadable rows retain their public history fields and their
place in the page, with empty private data and no derived result URL. Count
endpoints continue to count stored rows; the list does not rewrite or repair them.
Public task-ID batch fetches use the same tolerant projection and user scope.
Single-task reads used for billing remain strict. Workers also retain whether
quota was SQL NULL or an explicit zero: timeout/manual CAS uses `IS NULL` or an
exact value match accordingly, without normalizing stored data or inferring a
refund. Worker timestamp snapshots similarly retain SQL NULL separately from
numeric zero; timeout CAS uses `IS NULL` only when the read observed NULL. The
manual-settlement guard includes the observed quota so a changed
reservation rolls back its stale manual intent.
After a worker claim writes a numeric timestamp, the returned snapshot clears
its NULL marker so that an immediate manual decision can match its own claim.
Retry placeholder rewrites now advance the database timestamp version atomically
with their metadata, even within one Unix second. Overflow or failed version
readback rolls back the rewrite; missing or terminal tasks are not recreated.
This prevents a timeout/manual decision from overwriting a newer retry's plan.
The reverse order also fails: a retry must claim the version it observed before
rewriting metadata, and cannot rewrite a task with an existing task-finalize
record. A conflict stops submission through the existing local persistence-error
path; reloading alone does not authorize replacing that settlement decision.
Claim-only timestamp changes may be reloaded at most twice when the persisted
placeholder otherwise matches its observed identity, status, quota and private
metadata. Confirmed upstream work, a changed identity, a terminal task or an
existing task-finalize owner prevents another submission.
Definite request-failure cleanup first claims the nonterminal task with no
task-finalize owner and commits the original request-refund intent in the same
transaction. Only that winner invokes the existing refund engine; manual-owned
tasks retain their quota and evidence. Failed reads, mismatched refund identity
or intent writes retain funds/state for reconciliation instead of guessing.
Generic tasks and Midjourney use this same coordinator, including placeholder
preparation failures; a failed database write never authorizes an independent
refund. Accepted or ambiguous provider responses keep their existing handling.

Apply migrations for `tasks.submit_timeout_retained_funding` and
`task_submit_failure_recoveries` before starting nodes that submit or poll
tasks. If the task claim or refund intent cannot be saved after bounded retries,
the coordinator records the confirmed rejection and original refund identity
in the journal independently of writes to the task table. The fallback and
guarded claims use separate deadlines. The settlement worker persists retry
backoff and picks the oldest due entries, including rows recorded by older nodes.
Pending recovery prevents timeout or retry from taking over the same task.
`intent_recorded` indicates a durable intent. Use the applied settlement and its deltas to verify
the refund. A conflicting task assignment is held for manual reconciliation.
When a timeout still has no confirmed upstream ID, it records the unknown
submission outcome and retains funds, including for H3 or expression-priced
tasks. A later definite rejection of the same request can claim a marked
timeout and refund its original reservation, including any retry increase
already reserved by that request. The task keeps its original completion time.
A retry from a free group can commit its first paid reservation before the
placeholder records the new funding source. A confirmed rejection can recover
that reservation only when the canonical pre-consume receipt and source
selection agree with the task, request, token and exact refund amount.
Pre-upgrade timeouts and existing manual settlement records retain their
reconciliation workflow because their refund ownership cannot be inferred.
A timed-out legacy task with both `awaiting_upstream_id` and a non-empty
`upstream_task_id` keeps its quota for manual reconciliation. The contradictory
state has no automatic late-refund marker. Match both private fields and the
positive quota on `FAILURE` rows and on nonterminal rows with a manual
task-finalize settlement. Reconcile the upstream task and the original
reservation using the stored billing request ID.
Definite rejections of free submissions can use a status-only recovery journal.
Replay verifies the original request, task state and absence of a funding
reservation before recording failure. It creates no refund intent. Upgrade all
recovery workers together so status-only rows are handled automatically.
If the database cannot save even the recovery record, use the original task
and reservation records to reconcile the logged task ID after service returns.
Historical private metadata retains unknown fields, including nested typed
objects and arrays, through polling updates. Progress-only polls omit unchanged
private data; edits merge the retained fields and validate their references.
An incompatible typed replacement that would discard retained history fails
closed. Conflicting repeated JSON object members outside canonical fact maps
are repairable invalid data; equal identity aliases remain readable. Numeric
aliases compare at their original precision. Truncated target key or digest
prefixes reach the deletion decoder, while disjoint
corruption still does not block unrelated deletion. The scan remains unindexed.
Channel setting repairs compare the originally malformed value so an automatic
reset cannot erase a concurrent administrator edit. Pricing metadata reload
failures propagate instead of silently removing a source from the comparison.
These guards do not remove the separate real-database, deployment or million-row
lock-time acceptance requirements.
If an upload preflight finishes reading package and receipt sets but its final
head reread is interrupted, those private observations are compared with the
published snapshot. Differing evidence suspends the Host; matching evidence may
preserve it. No incomplete read is published as an authoritative generation.
