# Namespace configuration and migration

Namespaces assign workload resources to an explicit logical owner. A namespace
can use several clouds, and several namespaces can share a cloud. Until RBAC is
introduced, every authenticated operator can select every configured namespace.
Namespace selection is not a permission grant, network boundary, quota, or proof
that Vmango owns a disk for deletion.

## Configure the workspace

```hcl
namespace_directory = "/var/lib/vmango/namespaces"

namespace "platform" {
    name = "Platform"
    description = "Platform workloads"
    clouds = ["local", "edge"]
}

namespace "analytics" {
    name = "Analytics"
    clouds = ["local"]
}
```

The label is the stable ID. IDs use the catalog identifier syntax, names default
to IDs, descriptions are optional, and clouds must be a nonempty list of unique
configured cloud IDs. Configuration activates on restart. There is no implicit
default namespace, including when only one namespace exists.

Removing a block removes the namespace from selection and prevents new work in
its context. It does not remove resources, assignments, keys, templates or task
history, and does not require any backend to be available. Re-adding the same ID
restores access subject to its current allowed-cloud list. Administration shows
stored namespace IDs normally even when they no longer appear in configuration.

VM membership and display labels reside in a versioned Vmango domain XML
metadata element, independently of scheduler metadata. Volume assignments and
labels reside in `namespace_directory/volumes/volumes.json` on the application
host. Namespace SSH keys and templates reside in
`namespace_directory/catalogs/catalogs.json`. These stores have exclusive
process locks, private permissions, and durable atomic replacement. Keep
`namespace_directory` on a local filesystem with reliable locking and fsync.
Multiple Vmango installations do not synchronize these records.

Display labels may repeat across namespaces. VM labels are unique within a
namespace/cloud; volume labels within a namespace/cloud/placement pool. New
backend names are generated independently; existing names, UUIDs, addresses and
guest hostnames are preserved by migration. New VM creation accepts an optional
`hostname` independently of its display label. When omitted, Vmango derives a
safe ASCII guest hostname from the label, or uses the unique generated VM name
when the label has no usable ASCII characters.

## API and browser context

Authenticated bootstrap discovery uses `GET /api/v1/namespaces`. Workload calls,
including clouds, machines, volumes, keys, templates, tasks and console-link
issuance, require exactly one `X-Vmango-Namespace` header:

```sh
curl -H "Authorization: Bearer $VMANGO_TOKEN" \
  -H 'X-Vmango-Namespace: platform' \
  https://vmango.example/api/v1/clouds/local/machines
```

A body or API query parameter cannot select the namespace. Missing, empty,
combined, duplicate and malformed headers fail. Selecting another namespace
does not permit cross-namespace attachment, cloning or deletion. Configured
images are read-only provisioning inputs shared by admitted namespaces within
the owning cloud.

Workload browser links preserve `?namespace=platform`. Selection belongs to the
current tab's URL and survives reload, copied links and Back/Forward. Without a
selection, the UI asks for one before workload requests; an invalid selection is
an error, never an instruction to choose a replacement automatically.

Administration is read-only and independent of workspace selection. Its cloud
machine/volume inventories and task history include assigned, unassigned and
legacy resources. Workload `/tasks/` pages remain namespace-scoped even though
navigation places them under Administration. There is no context-free workload
mutation route or `cloud-admin` bypass.

Native console WebSockets provide explicit context. Browser consoles use signed
credentials binding namespace, cloud, VM UUID and console kind. Old console links
without namespace binding are invalid after upgrade; issue fresh links. Existing
authentication, expiry, origin and same-origin WebSocket restrictions still apply.

## Upgrade and explicit migration

Upgrade clients and server together: the workload API intentionally changes and
has no legacy missing-header fallback. Existing resources are initially
unassigned and remain inspectable in Administration. Existing keys/templates
remain in their legacy stores until explicitly selected for migration. Nothing
is assigned from a name, creator, host placement or an automatic default.

1. Stop Vmango and retain a normal installation backup. The CLI must run as the
   same OS account as Vmango so it can open the private stores. Do not run a
   second instance against these files.
2. Configure destination namespaces and their clouds. Preserve configured host
   IDs and backend URIs when moving the application; those participate in
   physical resource identity.
3. Write a versioned JSON mapping with actual IDs. Include an attached disk's VM
   in the resource graph. Selecting a VM automatically includes its attached
   disks and VM-owned artifacts. Template selections include the revision that
   the operator reviewed.

```json
{
  "version": 1,
  "resources": [
    {
      "namespace": "platform",
      "cloud": "local",
      "machines": ["11111111-1111-4111-8111-111111111111"],
      "volumes": [{"host": "local", "path": "/var/lib/libvirt/images/data.qcow2"}]
    }
  ],
  "catalogs": [
    {
      "namespace": "platform",
      "keys": ["MD5:replace-with-the-existing-fingerprint"],
      "templates": [
        {
          "id": "22222222-2222-4222-8222-222222222222",
          "revision": "33333333-3333-4333-8333-333333333333"
        }
      ]
    }
  ]
}
```

4. Preview, then apply to an absolute private backup directory:

```sh
vmango -c /etc/vmango.conf namespace-migrate --mapping migration.json
vmango -c /etc/vmango.conf namespace-migrate --mapping migration.json \
  --apply --backup /var/lib/vmango-backups/namespace-migration-01
```

Dry-run validates the complete selected graphs and catalog conflicts without
assigning resources. Opening the stores acquires process locks and can create
empty private store directories. Apply backs up secure persistent VM XML,
configuration, mapping, namespace stores, task history, deletion-ownership
records, and legacy key/template stores before the first assignment. Backups
contain private data: directories use mode 0700 and files use mode 0600. A backup
must reside outside the source stores.

Migration preserves template IDs/revisions, VM UUIDs/backend names, volume
addresses and deletion-safety records. Exact repeated imports are idempotent;
conflicting target content is rejected rather than overwritten. Selected legacy
entries are copied only into explicitly named destinations; their originals are
retained. Read operations never perform assignment.

Queued or uncertain tasks holding selected resources block migration. Inspect
and recover them before proceeding. Legacy queued commands without reliable
namespace context must not execute after upgrade.

## Partial-write recovery and operator tasks

Libvirt metadata and application files are not one transaction. If assignment
fails after some writes, keep the backup directory and `effects.json` journal,
inspect the resource state, correct the underlying storage/backend error, and
rerun the same apply command with the same mapping, configuration and backup.
The saved manifest supplies the original graph and catalog snapshots; observable
identity/graph changes prevent unsafe resume. An incomplete backup without a
manifest is never applied; use a new empty backup directory after resolving its
failure. Do not remove assignment or ownership records to silence an error.

Operator recovery is available without a configured namespace or live backend.
It uses the existing task service and safety rules, acquires the task-store
process lock, and never starts queued execution:

```sh
vmango -c /etc/vmango.conf task-recover --action inspect
vmango -c /etc/vmango.conf task-recover --action inspect --id TASK_UUID
vmango -c /etc/vmango.conf task-recover --action cancel --id TASK_UUID \
  --operator maintainer --reason 'Cancel obsolete queued work after upgrade'
vmango -c /etc/vmango.conf task-recover --action release --id TASK_UUID \
  --operator maintainer --reason 'Backend inspected; partial effects reconciled'
```

Cancellation applies only to queued work. Lock release retains the existing
uncertainty safeguards and requires an operator identity and reason. Inspect
actual backend state before releasing uncertain locks. Namespace removal does
not interrupt an operation midway through changes or release its locks.

## Backup, restore and moving the application

With Vmango stopped, copy the complete namespace directory, task directory,
volume-ownership directory (`tasks.directory` plus `-volumes`), legacy catalogs,
configuration, and the existing credential/signing stores according to the
installation backup policy. Preserve each VM's persistent domain XML; VM
namespace membership is not mirrored in a filesystem registry. Preserve owner
and private permissions, and do not transfer `.lock` files as evidence that a
store is unlocked: locks belong to processes.

Restore a consistent set while Vmango is stopped. Restore persistent VM metadata
from the matching XML backups using libvirt operator tooling, retaining unrelated
domain configuration and checking current resources before redefining a domain.
Restore corresponding application stores and ensure no queued command can run
against an obsolete snapshot. Restart only after inspection and a dry-run of any
remaining migration. Restoring assignment data does not roll back guest writes,
resource creation/deletion, or other backend operations.

On another application host, transfer the stores as well as configuration and
retain configured host IDs/backend identity. Copying only libvirt XML loses
volume/key/template assignments; copying only files loses VM membership. Reads
through read-only libvirt connections need no mutation or auxiliary backend pool.

Volume records check available backend identity, pool identity and backend key.
Observable identity changes fail assignment reads instead of looking unassigned.
An external replacement preserving every checked identifier may retain the old
assignment. After such external changes, correctness is the operator's
responsibility; there is no replacement-detection or reconciliation subsystem.

## Verification scope

Regression coverage must include storage corruption and write failures, namespace
removal/re-addition, graph rejection before effects, guessed key/template IDs,
revision conflicts, task restart/idempotency/locks, URL switching and stale UI
responses. Run isolated acceptance lifecycle and migration tests only against
resources created by the test itself. Report local Go tests, browser checks,
macOS/HVF, Linux/KVM, remote libvirt and individual storage combinations
separately; a simulated repository or local pass does not establish remote or
production hypervisor behavior.
