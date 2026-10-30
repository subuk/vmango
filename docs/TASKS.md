# Durable infrastructure tasks

Infrastructure mutations use one durable task per command. VM creation is one
task with provisioning stages, not a set of independently queued steps. The
bundled UI provides Tasks in the main navigation, history links on resources,
and a progress page before a newly requested resource exists.

## Submission and observation

All VM and volume mutations require an `Idempotency-Key` header (8–200
characters) and return `202 Accepted`, `Location: /api/v1/tasks/{id}` and:

```json
{"task":{"id":"...","state":"queued"},"status_url":"/api/v1/tasks/..."}
```

The actual `task` object includes identity, timestamps, resource references and
history. A 202 response confirms durable acceptance, not successful execution.
Reads, authentication, SSH key management and console link issuance remain
synchronous. There is no parallel legacy synchronous mutation contract.

Reuse the same key and equivalent command after a lost submission response.
Deduplication is scoped to the stable authenticated principal, independent of
the web session or personal token. Different parameters with that key return
409. Retrying a failed, cancelled or interrupted task's original key returns
that same task; it never restarts it. Submit a new command with a new key only
after explicitly assessing the previous outcome. The UI retains an uncertain
submission's key across reload in session storage, indexed by a SHA-256 command
fingerprint; it stores neither credentials nor command contents in that storage.
Its Recover submission action resends the original command from memory.

`GET /api/v1/tasks` lists newest first. Filters are `state` (comma separated),
`resource_kind`, `resource_scope`, `resource_id` and `blocking=true`. Use
`limit=1..100` and the returned opaque `next` value as `before` for subsequent
pages. Resource references use provider-independent kind, scope and identifier;
the current adapter uses a configured node as scope and VM name or backend
volume path as identifier. They are not restricted to this addressing scheme
in the task contracts. References and diagnostics survive resource deletion.

`GET /api/v1/tasks/{id}` returns the public task including named stages, start
and end times, lifecycle transitions, confirmed effects and sanitized backend
diagnostics. Private command data is never returned. Polling errors mean status
is temporarily unavailable; they do not change the last known task state.

All authenticated web and personal-token users currently have application-wide
task read, submission, cancellation and lock-release permissions. RBAC is not
implemented. Tasks retain the authorization granted at acceptance; logging out,
revoking the submitting token or later changing its owner's eligibility does
not cancel accepted queued or running work.

## Lifecycle and completion

The lifecycle is `queued -> running -> succeeded | failed | interrupted`, or
`queued -> cancelled`. Each transition is committed before it is reported.
`POST /api/v1/tasks/{id}/cancel` cancels only queued work; a concurrent worker
claim makes cancellation return 409. Running libvirt calls cannot be cancelled.
Closing a browser tab, dialog or HTTP connection never cancels accepted work.

Configuration and attachment changes are read back, deletion confirms absence,
and volume changes verify the requested capacity. For VM creation, the domain,
disk attachments and configuration are checked before the event hook and
optional start stage. Guest OS boot, guest-agent readiness and cloud-init
completion are outside the command contract. A successful libvirt Start return
is sufficient for the start stage, including `start=true` creation. The UI
reads observed VM state separately rather than inferring running from success.

Reboot waits up to two minutes for a libvirt reboot event registered before the
request. If no event arrives, the task fails with an uncertain outcome and
retains its locks. This does not prove that the guest failed to reboot. See the
[libvirt domain event API](https://libvirt.org/html/libvirt-libvirt-domain.html#virConnectDomainEventRegisterAny).
Hard power-off means libvirt Destroy; it is not a graceful shutdown.

A mandatory `vm_created` hook failure fails the creation task and prevents its
subsequent start stage. Existing optional subscriptions remain best effort.
Hook output and script source are excluded from task diagnostics and logs,
because their content is arbitrary and may contain secrets. The failed event
and process exit error remain available. External hook effects can be uncertain
even when infrastructure changes are confirmed. There is no automatic rollback.

Backend errors retain the original libvirt message, numeric code and domain,
attempted operation and resource context through adapter/service wrappers.
Known private command fragments and credential patterns are redacted while
retaining actionable driver/QEMU text. Structured execution logs include
`task_id` for correlation. Do not log API bodies or authorization headers in a
reverse proxy. A failure after confirmed changes records those effects; a
failed backend call may also have had effects that could not be confirmed.

## Restart and resource locks

Before workers start, every stale running task is durably marked interrupted.
Its stage history and known effects remain intact. The recorded finish time is
the time interruption was detected, not an invented crash time. A stage left
running has unknown outcome. No backend error is invented for process death.
Queued tasks that never started remain eligible to run from their saved command
snapshot, including resolved image defaults, key values and config-drive settings.
Queued VM updates persist the requested patch and apply it to a fresh backend
read at execution. Omitted fields retain their current values; a missing VM
fails the update before any write instead of being recreated from an old snapshot.
Creation rechecks VM absence, existing disk eligibility and destination availability
before its first backend mutation, retaining the accepted creation settings.

The executor does not automatically resume, retry, replay, reconcile or roll
back interrupted work. It makes no exactly-once backend execution guarantee.
A backend action might have completed or still be running independently.

All affected VM and volume identities are locked atomically at acceptance,
including clone sources, destination paths, existing attachments and config
drives. Conflicting submissions return 409 with `task_id` and `task_url`; they
are not deferred for surprising later execution. Unrelated resources remain
usable. These locks coordinate Vmango commands, not external `virsh` users or
other applications; operators must coordinate external mutations themselves.

Queued and running tasks hold their locks. Successful/cancelled tasks and
failures with known outcomes release them. Tasks with uncertain outcomes retain
locks after restart. Inspect the actual backend first, then use Release resource
locks on the task page, or:

```http
POST /api/v1/tasks/{id}/release
Content-Type: application/json

{"reason":"Inspected backend resources; no conflicting operation remains"}
```

This records who released the locks, when and why on the existing task. It
does not add a recovery entity, change the original outcome, stop backend work
or execute cleanup. Recovery/cleanup are explicit operator actions.

## Storage, retention and operations

```hcl
tasks {
    directory = "/var/lib/vmango/tasks"
    workers = 2
    retention_days = 0
}
```

The standalone directory default is `~/.vmango/tasks`. `workers` is 1–32;
`retention_days` is 0–36500. Zero retains all history indefinitely, which is
the default. With a positive value, hourly cleanup removes terminal tasks older
than that many days, except tasks still blocking resources. Their idempotency
keys expire with their history; a key reused after cleanup can create a new
task. Choose retention longer than any client retry window. Active or unresolved
tasks are never silently discarded to satisfy retention.

The initial store is a private local JSON snapshot. Commands, lifecycle/history,
deduplication metadata and resource locks commit together using temporary-file
write, file sync, atomic replacement and directory sync. The directory must be
dedicated, service-owned 0700 with 0600 files on Unix, using the credential
store's protected file primitives. A lifetime exclusive file lock prevents two
executors from sharing it. Corrupt/unsupported history fails startup; it is
never replaced by an empty queue. Storage commit failures fence further
execution/submission until restart, preserving locks for uncertain work.

Private command payloads, including cloud-init, are persisted only while queued
or running and removed in the terminal-state transaction. Diagnostic history
keeps only sanitized public data and command fingerprints. Local storage and
backups still need normal service-account protection; payloads are not encrypted
at rest. Make backups with Vmango stopped and preserve the complete directory.
Restoring a backup can restore old queued work and old deduplication boundaries;
inspect it before starting the service. Do not manually edit live snapshots.

This is a single-process local store. Each update rewrites the snapshot; it is
intended for the current deployment size, not distributed workers or large
multi-tenant histories. Multi-process/network-filesystem storage, HA, automatic
reconciliation and resumable workflow execution are outside this implementation.

## Managed image preparation

A `machine.create` command snapshots each selected image's definition, source
SHA-256 and resolved pool UUID/path before durable acceptance. Submission makes
no upstream HTTP request and performs no image mutation. The worker prepares
missing managed sources and rechecks capacity/format-dependent creation rules
against the confirmed backend volume before making any VM disks.

Image preparation uses the same bounded workers as provisioning. The first
worker requiring an artifact performs its transfer. Commands sharing a managed
artifact stay queued while a member runs, leaving workers available for unrelated
artifacts and nodes. In-process callers also coordinate through the active
preparation and observe its result. Requests accepted before a preparation failed
inherit that failure; retry requires a newly accepted explicit attempt. There is no separately queued
preparation job, workflow graph or background retry. Catalog source references
hold shared read locks; writes and uncertain earlier outcomes exclude readers.
Artifact preparation additionally coordinates by backend connection, pool UUID
and content-addressed destination, including across distinct image IDs.

Stages include `prepare-image`, `wait-image`, `inspect-image-source`,
`create-image-staging`, `stream-image`, `verify-image`, `cleanup-image` and
`validate-image-creation`. `stream-image` means concurrent HTTP download and
libvirt upload, with incremental SHA-256 and bounded backpressure. Its optional
`transfer` object records byte count, total bytes, average bytes per second,
estimated transfer seconds remaining and sample time. Samples are durably
throttled to approximately once per second plus the final sample. Estimates do
not include later verification/provisioning and must not be presented as live
throughput after failure or interruption.

Readiness metadata is persisted separately under `<tasks.directory>-images` and
is not removed with task-history retention. It contains no image payload. Ready
artifacts survive later provisioning failures. Interrupted transfers do not
resume, and staging is never adopted as ready from its name or path. A new
explicit attempt reuses only verified backend identity or fails with instructions
to inspect leftovers. Cleanup is limited to staging created by the current
operation and refuses attached storage. Retained old revisions have no automatic
eviction or deletion policy.
