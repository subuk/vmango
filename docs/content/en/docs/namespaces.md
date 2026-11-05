---
title: "Namespaces"
description: "Organize machines, disks, keys and templates into workspaces, and choose where to work."
weight: 2
---

A **namespace** is a named workspace for a group of virtual machines and their
resources. For example, use `platform` for shared services and `analytics` for
data-processing workloads. Each workspace has its own machines, volumes, SSH
key catalog, cloud-init templates and workload tasks.

A **cloud** describes where machines can run: its hosts, images, instance types,
networks and storage classes. A namespace selects which configured clouds its
workloads may use. Two namespaces can share the same cloud and physical hosts;
one namespace can use several clouds.

For example, Platform and Analytics can both run machines in the `local` cloud,
while Platform also uses `edge`. Selecting Analytics shows its workloads, even
when Platform machines are running on the same host. Both workspaces may have a
machine called `web`; Vmango keeps their backend identities distinct.

Namespaces organize resources and prevent operations from accidentally combining
resources from different workspaces. **Every authenticated user can currently
select every configured namespace.** Namespaces do not provide per-user access
permissions, separate guest networks or CPU/storage quotas. Hosts and their
available capacity remain shared.

## Configure a namespace

The installation administrator adds namespace blocks to `vmango.conf`, alongside
the existing cloud and API blocks. For a single workspace using an existing
`cloud "local"`, start with:

```hcl
namespace "platform" {
    name = "Platform"
    description = "Shared services"
    clouds = ["local"]
}
```

The shipped quick-start configuration already contains this namespace; edit it
instead of adding another block with the same ID. Even a single-workspace
installation needs an explicit namespace.

| Setting | Meaning |
| --- | --- |
| `"platform"` | Stable namespace ID used in links, API requests and saved resource assignments. Keep it when changing the display name. |
| `name` | Name shown in the selector. Optional; defaults to the ID. |
| `description` | Optional explanation for operators. |
| `clouds` | One or more existing cloud IDs this workspace may use. IDs must be unique in the list. |

To add Analytics on the same cloud, append:

```hcl
namespace "analytics" {
    name = "Analytics"
    clouds = ["local"]
}
```

If `cloud "edge"` is also configured, Platform can use both with
`clouds = ["local", "edge"]`. This list refers to cloud IDs, not host names.
Restart Vmango after editing the configuration. Namespace creation and changes
are performed in the configuration file; the web interface displays them under
**Administration → Namespaces**.

Changing the `name` changes the displayed name. Changing the block's ID creates
a different workspace; it does not rename or move existing resource assignments.

## Work in a namespace

1. Sign in and choose **Platform** in the **Namespace** selector. If the URL has
   no namespace, Vmango asks you to choose one, including on single-workspace
   installations.
2. On **Machines** or **Volumes**, choose a **Cloud** from those allowed for the
   workspace. If there is only one available choice, it may already be selected.
3. Add provisioning keys under **Keys** and reusable user data under
   **Cloud-init templates**. These catalogs belong to the namespace and can be
   used in any of its allowed clouds. Add a key or template separately in another
   namespace when it should also be available there.
4. Select **Create machine** and follow the [provisioning guide](/docs/images/#launch-a-machine).
   The new machine and its disks belong to the selected namespace. Its display
   name may match a name in another namespace. The optional guest hostname is
   configured separately from that display name.
5. Use **Tasks** to follow work accepted in this namespace. Switching namespace
   changes the task list; already accepted operations retain their original
   namespace and continue there.

Switch workspaces using the **Namespace** selector. Machine and disk lists,
keys, templates and tasks then use the new workspace. Save or discard unfinished
form edits when prompted; changing workspaces also disconnects an embedded
console. To attach a disk to a machine, both must belong to the same namespace
and satisfy the selected cloud's placement rules. Selecting another namespace
does not transfer a machine or disk between workspaces.

## Links and separate tabs

The current tab's URL records its workspace, for example:

```text
https://vmango.example/machines/?namespace=platform
```

Copy the full URL when sharing a workload page. Reload and Back/Forward restore
the namespace from that URL. You can keep Platform open in one tab and Analytics
in another; their selections are independent. A missing namespace prompts for a
choice. An unknown or malformed namespace shows an error instead of silently
opening a different workspace.

## Find and assign existing resources

After upgrading an older installation, existing VMs and disks have no namespace
until explicitly assigned. They are visible in **Administration → Clouds →
your cloud → Machine inventory / Volume inventory**, even if workload lists are
empty. Administration also shows resources belonging to other namespaces and
provides **Task history** across namespaces. Its inventories do not depend on
the current workspace selection.

For an individual unassigned resource:

1. Open its details in the Administration cloud inventory.
2. Choose a **Destination namespace**, then **Open assignment preview**.
3. Select **Preview assignment** and review the machines and disks together.
   Assigning a VM includes its attached disks and VM-owned artifacts. A graph
   containing resources already assigned to another namespace is rejected.
4. Select **Assign to platform** (the button uses your destination's ID), then
   **Track task**. Wait for successful
   completion before using the resource in the destination workspace.

Assignment does not move disk data or grant Vmango permission to delete a disk
that it did not create or adopt under its existing deletion-safety rules.
Resources already assigned to a namespace cannot be transferred to another
namespace through this workflow.

For an installation upgrade or bulk assignment, use the
[migration and recovery guide](https://github.com/subuk/vmango/blob/master/docs/NAMESPACES.md#upgrade-and-explicit-migration).
It covers stopping Vmango, backing up state, previewing an explicit mapping,
applying it and recovering interrupted work. Existing SSH keys and templates
must be explicitly selected for migration; they are not automatically copied
into every workspace. API clients must also send the new namespace header.

## Common questions

**Why is my cloud missing?** Check the selected namespace's `clouds` list and
restart Vmango after changing it. Adding a namespace does not configure hosts,
networks or storage pools; see [cloud configuration](/docs/clouds/).

**Why is my machine missing?** Check the Namespace and Cloud selectors, then use
Administration inventory to inspect its assignment. Machines created outside
Vmango or before the upgrade may still be unassigned. A warning about unavailable
hosts means the inventory may be incomplete.

**What happens if I remove a namespace block?** It disappears from the selector
after restart and cannot accept new work. Its machines, disks, keys, templates
and history remain stored. Administration still shows the saved namespace ID.
Restore the same ID with the required clouds to use it again. Removing a block
is not a resource-deletion operation.

**What must I back up?** VM assignments are stored in libvirt domain XML;
volume assignments, keys and templates are stored on the Vmango application
host beneath `namespace_directory` (default `~/.vmango/namespaces`). Preserve
both along with the rest of the installation state. Follow the
[backup and restore procedure](https://github.com/subuk/vmango/blob/master/docs/NAMESPACES.md#backup-restore-and-moving-the-application)
before moving the server or restoring an installation.

**How do scripts choose a workspace?** Discover configured choices with
`GET /api/v1/namespaces`, then send `X-Vmango-Namespace: platform` on workload
requests. The browser's `?namespace=` convention is not an API query parameter.
See the [API guide](/docs/api/).
