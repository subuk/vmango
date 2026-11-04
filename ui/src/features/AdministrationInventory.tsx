import { useState } from "react";
import {
    Alert,
    Box,
    Button,
    Link,
    Stack,
    Typography,
    TextField,
    MenuItem,
} from "@mui/material";
import { useParams, useSearchParams } from "react-router";
import { useQuery } from "@tanstack/react-query";
import {
    WorkspaceLink as RouterLink,
    useWorkspace,
    useNamespaces,
    type Namespace,
} from "../app/Workspace";
import { request } from "../api/client";
import {
    adminCloudPath,
    adminCloudURL,
    useAdministration,
} from "../api/administration";
import {
    type VM,
    type Volume,
    type VMDetail,
    type VolumeDetail,
} from "../api/hooks";
import { type TaskPage, taskLabel } from "../api/tasks";
import { PageTitle, QueryState, bytes, State } from "../components/common";
import { ResourceTable } from "../components/ResourceTable";

type Assigned = { namespace_id?: string };
const namespace = (row: Assigned) => row.namespace_id || "Unassigned";
function useInventory<T>(path: string) {
    const { principal } = useWorkspace();
    return useQuery({
        queryKey: ["administration", principal, path],
        queryFn: ({ signal }) => request<T>(path, "GET", undefined, signal),
        refetchInterval: false,
        refetchOnWindowFocus: false,
    });
}
export function NamespaceConfiguration() {
    const { namespace: id } = useParams();
    const q = useAdministration<{ items: Namespace[] }>(
        "/api/v1/administration/configuration/namespaces",
    );
    return (
        <>
            <PageTitle
                title={id ? "Namespace configuration" : "Namespaces"}
                description="Configured workspaces and the clouds they may use. Changes activate on restart."
            />
            <QueryState query={q}>
                {(data) => {
                    const rows = id
                        ? data.items.filter((n) => n.id === id)
                        : data.items;
                    if (id && !rows.length)
                        return (
                            <Alert severity="error">Namespace not found.</Alert>
                        );
                    return (
                        <ResourceTable
                            rows={rows}
                            rowKey={(n) => n.id}
                            search={(n) => `${n.id} ${n.name} ${n.description}`}
                            columns={[
                                {
                                    id: "name",
                                    label: "Name",
                                    value: (n) => n.name,
                                    render: (n) => (
                                        <Link
                                            component={RouterLink}
                                            to={`/administration/configuration/namespaces/${encodeURIComponent(n.id)}/`}
                                        >
                                            {n.name}
                                        </Link>
                                    ),
                                },
                                { id: "id", label: "ID", value: (n) => n.id },
                                {
                                    id: "description",
                                    label: "Description",
                                    value: (n) => n.description || "—",
                                },
                                {
                                    id: "clouds",
                                    label: "Allowed clouds",
                                    value: (n) => (n.clouds || []).join(", "),
                                    render: (n) => (
                                        <Stack>
                                            {(n.clouds || []).map((c) => (
                                                <Link
                                                    key={c}
                                                    component={RouterLink}
                                                    to={adminCloudURL(c)}
                                                >
                                                    {c}
                                                </Link>
                                            ))}
                                        </Stack>
                                    ),
                                },
                            ]}
                            empty="No namespaces are configured."
                        />
                    );
                }}
            </QueryState>
        </>
    );
}
export function AdministrationInventory({
    kind,
}: {
    kind: "machines" | "volumes";
}) {
    const { cloud = "", id } = useParams();
    return id ? (
        <InventoryDetail kind={kind} cloud={cloud} id={id} />
    ) : (
        <InventoryList kind={kind} cloud={cloud} />
    );
}
function InventoryList({
    kind,
    cloud,
}: {
    kind: "machines" | "volumes";
    cloud: string;
}) {
    const q = useInventory<{
        items: ((VM | Volume) & Assigned)[];
        unavailable_hosts: string[];
    }>(`${adminCloudPath(cloud)}/${kind}`);
    return (
        <>
            <PageTitle
                title={
                    kind === "machines"
                        ? "Machine inventory"
                        : "Volume inventory"
                }
                description="Read-only inventory across all namespaces, including unassigned resources."
                action={
                    <Button onClick={() => void q.refetch()}>Refresh</Button>
                }
            />
            <QueryState query={q}>
                {(data) => (
                    <>
                        {!!data.unavailable_hosts?.length && (
                            <Alert severity="warning" sx={{ mb: 2 }}>
                                Inventory is incomplete. Unavailable hosts:{" "}
                                {data.unavailable_hosts.join(", ")}
                            </Alert>
                        )}
                        <ResourceTable
                            rows={data.items}
                            rowKey={(v) => v.id}
                            search={(v) => `${v.name} ${v.id} ${namespace(v)}`}
                            columns={[
                                {
                                    id: "name",
                                    label: "Name",
                                    value: (v) => v.name,
                                    render: (v) => (
                                        <Link
                                            component={RouterLink}
                                            to={`${adminCloudURL(cloud)}${kind}/${encodeURIComponent(v.id)}/`}
                                        >
                                            {v.name}
                                        </Link>
                                    ),
                                },
                                {
                                    id: "namespace",
                                    label: "Namespace",
                                    value: namespace,
                                },
                                {
                                    id: "state",
                                    label:
                                        kind === "machines"
                                            ? "State"
                                            : "Attachment",
                                    value: (v) =>
                                        "state" in v
                                            ? v.state
                                            : v.attached_to || "Unattached",
                                },
                                { id: "id", label: "ID", value: (v) => v.id },
                            ]}
                        />
                    </>
                )}
            </QueryState>
        </>
    );
}
function InventoryDetail({
    kind,
    cloud,
    id,
}: {
    kind: "machines" | "volumes";
    cloud: string;
    id: string;
}) {
    const namespaces = useNamespaces();
    const [destination, setDestination] = useState("");
    const q = useInventory<(VMDetail | VolumeDetail) & Assigned>(
        `${adminCloudPath(cloud)}/${kind}/${encodeURIComponent(id)}`,
    );
    return (
        <>
            <PageTitle
                title={
                    kind === "machines"
                        ? "Machine inventory details"
                        : "Volume inventory details"
                }
                action={
                    <Button
                        component={RouterLink}
                        to={`${adminCloudURL(cloud)}${kind}/`}
                    >
                        Back to inventory
                    </Button>
                }
            />
            <QueryState query={q}>
                {(resource) => (
                    <Stack spacing={3}>
                        <Typography variant="h2">{resource.name}</Typography>
                        {!resource.namespace_id && (
                            <Stack
                                direction="row"
                                spacing={2}
                                sx={{ flexWrap: "wrap" }}
                            >
                                <TextField
                                    select
                                    label="Destination namespace"
                                    value={destination}
                                    sx={{ minWidth: 220 }}
                                    onChange={(e) =>
                                        setDestination(e.target.value)
                                    }
                                >
                                    <MenuItem value="" disabled>
                                        Choose namespace
                                    </MenuItem>
                                    {(namespaces.data?.items || [])
                                        .filter((n) =>
                                            n.clouds?.includes(cloud),
                                        )
                                        .map((n) => (
                                            <MenuItem key={n.id} value={n.id}>
                                                {n.name}
                                            </MenuItem>
                                        ))}
                                </TextField>
                                <Button
                                    disabled={!destination}
                                    component={RouterLink}
                                    to={`/${kind}/${encodeURIComponent(cloud)}/${encodeURIComponent(id)}/assign/?namespace=${encodeURIComponent(destination)}`}
                                >
                                    Open assignment preview
                                </Button>
                            </Stack>
                        )}
                        <Box component="dl" className="details">
                            <dt>Namespace</dt>
                            <dd>{namespace(resource)}</dd>
                            <dt>ID</dt>
                            <dd className="mono">{resource.id}</dd>
                            {resource.backend_name && (
                                <>
                                    <dt>Backend name</dt>
                                    <dd>{resource.backend_name}</dd>
                                </>
                            )}
                            <dt>Cloud</dt>
                            <dd>{resource.cloud}</dd>
                            <dt>Zone</dt>
                            <dd>{resource.zone || "—"}</dd>
                            {"state" in resource ? (
                                <>
                                    <dt>State</dt>
                                    <dd>
                                        <State value={resource.state} />
                                    </dd>
                                    <dt>Host</dt>
                                    <dd>{resource.node}</dd>
                                    <dt>Managed</dt>
                                    <dd>{resource.managed ? "Yes" : "No"}</dd>
                                    <dt>vCPUs</dt>
                                    <dd>{resource.vcpus}</dd>
                                    <dt>Memory</dt>
                                    <dd>{bytes(resource.memory_bytes)}</dd>
                                    <dt>Architecture</dt>
                                    <dd>{resource.arch}</dd>
                                    <dt>IP addresses</dt>
                                    <dd>
                                        {resource.ip_addresses.join(", ") ||
                                            "—"}
                                    </dd>
                                    <dt>Instance type</dt>
                                    <dd>{resource.instance_type || "—"}</dd>
                                    <dt>Image</dt>
                                    <dd>{resource.image || "—"}</dd>
                                </>
                            ) : (
                                <>
                                    <dt>Size</dt>
                                    <dd>{bytes(resource.size_bytes)}</dd>
                                    <dt>Format</dt>
                                    <dd>{resource.format}</dd>
                                    <dt>Pool</dt>
                                    <dd>{resource.pool}</dd>
                                    <dt>Attached to</dt>
                                    <dd>
                                        {resource.attached_to || "Unattached"}
                                    </dd>
                                    <dt>Protected</dt>
                                    <dd>{resource.protected ? "Yes" : "No"}</dd>
                                </>
                            )}
                        </Box>
                        {"disks" in resource && (
                            <>
                                <Typography variant="h2">Disks</Typography>
                                <ResourceTable
                                    rows={resource.disks}
                                    rowKey={(d) => d.volume_id}
                                    search={(d) => `${d.volume_id} ${d.alias}`}
                                    columns={[
                                        {
                                            id: "id",
                                            label: "Volume",
                                            value: (d) => d.volume_id,
                                            render: (d) => (
                                                <Link
                                                    component={RouterLink}
                                                    to={`${adminCloudURL(cloud)}volumes/${encodeURIComponent(d.volume_id)}/`}
                                                >
                                                    {d.alias || d.volume_id}
                                                </Link>
                                            ),
                                        },
                                        {
                                            id: "device",
                                            label: "Device",
                                            value: (d) => d.device || "disk",
                                        },
                                        {
                                            id: "bus",
                                            label: "Bus",
                                            value: (d) => d.bus || "—",
                                        },
                                    ]}
                                />
                            </>
                        )}
                    </Stack>
                )}
            </QueryState>
        </>
    );
}
export function AdministrationTasks() {
    const [params, setParams] = useSearchParams();
    const filters = new URLSearchParams();
    filters.set("limit", "25");
    const before = params.get("before");
    if (before) filters.set("before", before);
    const q = useInventory<TaskPage>(`/api/v1/administration/tasks?${filters}`);
    return (
        <>
            <PageTitle
                title="Task history across namespaces"
                description="Read-only history, including infrastructure and legacy tasks. Recovery remains available through the operator CLI."
                action={
                    <Button onClick={() => void q.refetch()}>Refresh</Button>
                }
            />
            <QueryState query={q}>
                {(data) => (
                    <>
                        <ResourceTable
                            rows={data.items || []}
                            rowKey={(t) => t.id}
                            search={(t) =>
                                `${t.id} ${t.kind} ${(t as Assigned).namespace_id || ""}`
                            }
                            columns={[
                                {
                                    id: "kind",
                                    label: "Operation",
                                    value: (t) => t.kind,
                                    render: (t) => (
                                        <Link
                                            component={RouterLink}
                                            to={`/administration/tasks/${encodeURIComponent(t.id)}/`}
                                        >
                                            {taskLabel(t.kind)}
                                        </Link>
                                    ),
                                },
                                {
                                    id: "namespace",
                                    label: "Namespace",
                                    value: (t) => namespace(t as Assigned),
                                },
                                {
                                    id: "state",
                                    label: "State",
                                    value: (t) => t.state,
                                },
                                {
                                    id: "created",
                                    label: "Accepted",
                                    value: (t) => t.created_at,
                                    render: (t) =>
                                        new Date(t.created_at).toLocaleString(),
                                },
                            ]}
                        />
                        <Stack direction="row" spacing={2} sx={{ mt: 2 }}>
                            {before && (
                                <Button onClick={() => setParams({})}>
                                    Newest tasks
                                </Button>
                            )}
                            {data.next && (
                                <Button
                                    onClick={() =>
                                        setParams({ before: data.next! })
                                    }
                                >
                                    Older tasks
                                </Button>
                            )}
                        </Stack>
                    </>
                )}
            </QueryState>
        </>
    );
}
