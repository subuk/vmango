import { useEffect, useRef } from "react";
import {
    Alert,
    Box,
    Button,
    Chip,
    Link,
    Stack,
    Tab,
    Tabs,
    Typography,
} from "@mui/material";
import { useParams, useSearchParams } from "react-router";
import { WorkspaceLink as RouterLink } from "../app/Workspace";
import { useMe } from "../app/Auth";
import {
    adminCloudPath,
    adminCloudURL,
    useAdministration,
    type AdminCloud,
    type AdminHost,
    type HostList,
} from "../api/administration";
import {
    esc,
    type Network,
    type Pool,
    type VM,
    type Volume,
} from "../api/hooks";
import type { components } from "../api/schema";
import { Capacity, DetailSection } from "../components/Information";
import { ResourceTable } from "../components/ResourceTable";
import { bytes, PageTitle, QueryState } from "../components/common";
import { NodeDetail } from "./NodeDetail";
import { AdministrationResourceTasks } from "./AdministrationOperations";

type HostReference = components["schemas"]["AdministrationHostReference"];
export type HostTab =
    | "configuration"
    | "diagnostics"
    | "networks"
    | "storage"
    | "resources"
    | "tasks";
const tabs: { id: HostTab; label: string }[] = [
    { id: "configuration", label: "Configuration" },
    { id: "diagnostics", label: "Live diagnostics" },
    { id: "networks", label: "Networks" },
    { id: "storage", label: "Storage" },
    { id: "resources", label: "Machines & volumes" },
    { id: "tasks", label: "Recent tasks" },
];
const hostURL = (cloud: string, host: string, params: URLSearchParams) => {
    const next = new URLSearchParams(params);
    if (next.get("selected") !== host) next.delete("hostTab");
    next.set("selected", host);
    return `${adminCloudURL(cloud)}hosts/${esc(host)}/?${next}`;
};
function Fields({ entries }: { entries: [string, unknown][] }) {
    return (
        <Box component="dl" className="details">
            {entries.map(([key, value]) => (
                <Box component="div" sx={{ display: "contents" }} key={key}>
                    <dt>{key}</dt>
                    <dd>
                        {typeof value === "boolean"
                            ? value
                                ? "Enabled"
                                : "Disabled"
                            : String(value ?? "—") || "—"}
                    </dd>
                </Box>
            ))}
        </Box>
    );
}
export function AdministrationHosts({
    cloud,
    snapshot,
}: {
    cloud: AdminCloud;
    snapshot?: string;
}) {
    return cloud.capabilities.host_inventory ? (
        <HostInventory cloud={cloud} snapshot={snapshot} />
    ) : (
        <Alert severity="info">
            Host inventory is not supported by this cloud.
        </Alert>
    );
}
function HostInventory({
    cloud,
    snapshot,
}: {
    cloud: AdminCloud;
    snapshot?: string;
}) {
    const [params, setParams] = useSearchParams();
    const principal = useMe()?.id;
    const positionKey = `administration.hosts.${principal}.${cloud.id}`;
    const q = useAdministration<HostReference[]>(
        `${adminCloudPath(cloud.id)}/configuration/hosts`,
        true,
        snapshot,
    );
    const observed = useAdministration<HostList>(
        `${adminCloudPath(cloud.id)}/hosts`,
    );
    const selected = params.get("selected") || "";
    const focus = useRef<HTMLDivElement>(null);
    const inspectButtons = useRef(new Map<string, HTMLButtonElement>());
    const returnFocus = useRef("");
    const restored = useRef(false);
    useEffect(() => {
        if (selected) focus.current?.focus({ preventScroll: true });
        else if (returnFocus.current) {
            inspectButtons.current
                .get(returnFocus.current)
                ?.focus({ preventScroll: true });
            returnFocus.current = "";
        }
    }, [selected, !!q.data]);
    useEffect(() => {
        if (!q.data || restored.current) return;
        restored.current = true;
        try {
            const saved = sessionStorage.getItem(
                `vmango.page-scroll.${positionKey}`,
            );
            if (saved) {
                const position = JSON.parse(saved);
                window.scrollTo(position.left, position.top);
                sessionStorage.removeItem(`vmango.page-scroll.${positionKey}`);
            }
        } catch {
            /* Page position is best effort when browser storage is unavailable. */
        }
    }, [q.data, positionKey]);
    const savePosition = () => {
        try {
            sessionStorage.setItem(
                `vmango.page-scroll.${positionKey}`,
                JSON.stringify({ left: window.scrollX, top: window.scrollY }),
            );
        } catch {
            /* Navigation remains available without browser storage. */
        }
    };
    const close = () => {
        returnFocus.current = selected;
        select("");
    };
    const select = (id: string) =>
        setParams((old) => {
            const next = new URLSearchParams(old);
            id ? next.set("selected", id) : next.delete("selected");
            next.delete("hostTab");
            return next;
        });
    return (
        <>
            <PageTitle
                title="Hosts"
                description="Accepted host placement and observed capacity across this cloud."
                action={
                    <Button
                        onClick={() => {
                            void q.refetch();
                            void observed.refetch();
                        }}
                    >
                        Refresh
                    </Button>
                }
            />
            {observed.isError && (
                <Alert severity="warning" sx={{ mb: 2 }}>
                    Live inventory could not be loaded. Configured hosts remain
                    available; their observation state is unknown.
                </Alert>
            )}
            <QueryState query={q}>
                {(hosts) => {
                    const rows = hosts
                        .filter((h) => h.cloud_id === cloud.id)
                        .map((h) => {
                            const live = observed.data?.items.find(
                                (n) => n.id === h.id,
                            );
                            const unavailable =
                                observed.data?.unavailable_hosts.find(
                                    (n) => n.id === h.id,
                                );
                            return {
                                ...h,
                                live,
                                status: live
                                    ? "Observed"
                                    : unavailable
                                      ? "Unavailable"
                                      : "Unknown",
                                reason: unavailable?.code || "",
                            };
                        });
                    return (
                        <>
                            <ResourceTable
                                preferenceKey={positionKey}
                                scrollKey={positionKey}
                                selectedKey={selected}
                                onSelect={(h) => select(h.id)}
                                filters={[
                                    {
                                        id: "zone",
                                        label: "Zone",
                                        value: (h) => h.zone,
                                    },
                                ]}
                                rows={rows}
                                rowKey={(h) => h.id}
                                search={(h) =>
                                    `${h.id} ${h.zone} ${h.live?.hostname || ""} ${h.live?.arch || ""}`
                                }
                                state={(h) => h.status}
                                columns={[
                                    {
                                        id: "id",
                                        label: "Host",
                                        value: (h) => h.id,
                                        render: (h) => (
                                            <Link
                                                component={RouterLink}
                                                to={hostURL(
                                                    cloud.id,
                                                    h.id,
                                                    params,
                                                )}
                                                onClick={savePosition}
                                            >
                                                {h.id}
                                            </Link>
                                        ),
                                    },
                                    {
                                        id: "zone",
                                        label: "Placement zone",
                                        value: (h) => h.zone || "—",
                                    },
                                    {
                                        id: "state",
                                        label: "Observation",
                                        value: (h) => h.status,
                                        render: (h) => (
                                            <Stack spacing={0.5}>
                                                <Chip
                                                    size="small"
                                                    label={h.status}
                                                    color={
                                                        h.status === "Observed"
                                                            ? "success"
                                                            : h.status ===
                                                                "Unavailable"
                                                              ? "warning"
                                                              : "default"
                                                    }
                                                />
                                                {h.reason && (
                                                    <Typography variant="caption">
                                                        {h.reason}
                                                    </Typography>
                                                )}
                                            </Stack>
                                        ),
                                    },
                                    {
                                        id: "cpu",
                                        label: "Physical cores",
                                        value: (h) =>
                                            h.live?.physical_cores ?? -1,
                                        render: (h) =>
                                            h.live?.physical_cores ?? "—",
                                        numeric: true,
                                    },
                                    {
                                        id: "memory",
                                        label: "Installed memory",
                                        value: (h) =>
                                            h.live?.memory_bytes ?? -1,
                                        render: (h) =>
                                            h.live
                                                ? bytes(h.live.memory_bytes)
                                                : "—",
                                        numeric: true,
                                    },
                                    {
                                        id: "arch",
                                        label: "Architecture",
                                        value: (h) => h.live?.arch || "—",
                                    },
                                    {
                                        id: "hostname",
                                        label: "Observed hostname",
                                        value: (h) => h.live?.hostname || "—",
                                    },
                                ]}
                                actions={(h) => (
                                    <Button
                                        ref={(element) => {
                                            if (element)
                                                inspectButtons.current.set(
                                                    h.id,
                                                    element,
                                                );
                                            else
                                                inspectButtons.current.delete(
                                                    h.id,
                                                );
                                        }}
                                        aria-label={`Inspect ${h.id}`}
                                        aria-pressed={selected === h.id}
                                        onClick={() => select(h.id)}
                                    >
                                        Inspect
                                    </Button>
                                )}
                                empty="No hosts are configured in this cloud."
                            />
                            {selected && (
                                <Box
                                    ref={focus}
                                    tabIndex={-1}
                                    role="region"
                                    aria-label={`Selected host ${selected}`}
                                    onKeyDown={(event) => {
                                        if (event.key === "Escape") {
                                            event.stopPropagation();
                                            close();
                                        }
                                    }}
                                    sx={{ mt: 3, outlineOffset: 4 }}
                                >
                                    <Stack
                                        direction="row"
                                        spacing={2}
                                        sx={{
                                            mb: 2,
                                            justifyContent: "space-between",
                                        }}
                                    >
                                        <Link
                                            component={RouterLink}
                                            to={hostURL(
                                                cloud.id,
                                                selected,
                                                params,
                                            )}
                                            onClick={savePosition}
                                        >
                                            Open full host page
                                        </Link>
                                        <Button onClick={close}>
                                            Close host details
                                        </Button>
                                    </Stack>
                                    {rows.some((h) => h.id === selected) ? (
                                        <HostDetail
                                            key={`${cloud.id}/${selected}`}
                                            cloud={cloud}
                                            node={selected}
                                            snapshot={snapshot}
                                            embedded
                                        />
                                    ) : (
                                        <Alert severity="error">
                                            The selected host is not configured
                                            in this cloud.
                                        </Alert>
                                    )}
                                </Box>
                            )}
                        </>
                    );
                }}
            </QueryState>
        </>
    );
}
export function AdministrationHostDetail({
    cloud,
    snapshot,
    initialTab = "configuration",
}: {
    cloud: AdminCloud;
    snapshot?: string;
    initialTab?: HostTab;
}) {
    const { node = "" } = useParams();
    return cloud.capabilities.host_inventory ? (
        <HostDetail
            key={`${cloud.id}/${node}`}
            cloud={cloud}
            node={node}
            snapshot={snapshot}
            initialTab={initialTab}
        />
    ) : (
        <Alert severity="info">
            Host inventory is not supported by this cloud.
        </Alert>
    );
}
function HostDetail({
    cloud,
    node,
    snapshot,
    initialTab = "configuration",
    embedded = false,
}: {
    cloud: AdminCloud;
    node: string;
    snapshot?: string;
    initialTab?: HostTab;
    embedded?: boolean;
}) {
    const [params, setParams] = useSearchParams();
    const back = new URLSearchParams(params);
    back.set("selected", node);
    const chosen = params.get("hostTab");
    const tab = tabs.some((t) => t.id === chosen)
        ? (chosen as HostTab)
        : initialTab;
    const q = useAdministration<AdminHost>(
        `${adminCloudPath(cloud.id)}/configuration/hosts/${esc(node)}`,
        true,
        snapshot,
    );
    return (
        <Stack spacing={2}>
            {!embedded && (
                <Link
                    component={RouterLink}
                    to={`${adminCloudURL(cloud.id)}hosts/?${back}`}
                >
                    All hosts
                </Link>
            )}
            {embedded ? (
                <Typography variant="h2">{node}</Typography>
            ) : (
                <PageTitle
                    title={node}
                    description="Accepted configuration, backend observations and related resources."
                />
            )}
            <QueryState query={q}>
                {(h) =>
                    h.cloud_id !== cloud.id || h.id !== node ? (
                        <Alert severity="error">
                            The configured host does not belong to this cloud
                            and host scope.
                        </Alert>
                    ) : (
                        <>
                            <Typography variant="body2" color="text.secondary">
                                Cloud: {cloud.name} · Placement zone:{" "}
                                {h.zone || "Not set"} · Configuration changes
                                activate on restart.
                            </Typography>
                            <Tabs
                                value={tab}
                                onChange={(_, value: HostTab) =>
                                    setParams((old) => {
                                        const next = new URLSearchParams(old);
                                        next.set("hostTab", value);
                                        return next;
                                    })
                                }
                                variant="scrollable"
                                scrollButtons="auto"
                                aria-label="Host detail sections"
                            >
                                {tabs.map((t) => (
                                    <Tab
                                        id={`host-tab-${t.id}`}
                                        aria-controls={`host-panel-${t.id}`}
                                        key={t.id}
                                        value={t.id}
                                        label={t.label}
                                    />
                                ))}
                            </Tabs>
                            <Box
                                role="tabpanel"
                                id={`host-panel-${tab}`}
                                aria-labelledby={`host-tab-${tab}`}
                                sx={{ minWidth: 0 }}
                            >
                                {tab === "configuration" && (
                                    <DetailSection
                                        title="Accepted host configuration"
                                        description="Effective settings accepted by the running application. Live backend failures do not change this configuration."
                                    >
                                        <Button
                                            onClick={() => void q.refetch()}
                                        >
                                            Refresh configuration
                                        </Button>
                                        <Fields
                                            entries={[
                                                ["Host ID", h.id],
                                                ["Cloud", h.cloud_id],
                                                ["Placement zone", h.zone],
                                                [
                                                    "Placement labels",
                                                    Object.entries(
                                                        h.labels || {},
                                                    )
                                                        .map(
                                                            ([key, value]) =>
                                                                `${key}=${value}`,
                                                        )
                                                        .join(", "),
                                                ],
                                                [
                                                    "CPU overcommit",
                                                    h.cpu_overcommit,
                                                ],
                                                [
                                                    "Physical core reserve",
                                                    h.cpu_reserve,
                                                ],
                                                [
                                                    "Memory reserve",
                                                    bytes(
                                                        h.memory_reserve_bytes,
                                                    ),
                                                ],
                                                [
                                                    "Emulator",
                                                    `${h.emulator.value || h.emulator.state} (${h.emulator.source})`,
                                                ],
                                                ["Image pool", h.image_pool],
                                                [
                                                    "Config drive pool",
                                                    h.config_drive_pool,
                                                ],
                                                [
                                                    "Config drive suffix",
                                                    h.config_drive_suffix,
                                                ],
                                                ["Cache", h.cache],
                                                [
                                                    "QCOW metadata preallocation",
                                                    h.qcow_prealloc_metadata,
                                                ],
                                            ]}
                                        />
                                    </DetailSection>
                                )}
                                {tab === "diagnostics" && (
                                    <NodeDetail cloud={cloud.id} node={node} />
                                )}
                                {tab === "networks" && (
                                    <HostNetworks cloud={cloud.id} host={h} />
                                )}
                                {tab === "storage" && (
                                    <HostStorage cloud={cloud.id} host={h} />
                                )}
                                {tab === "resources" && (
                                    <HostResources
                                        cloud={cloud.id}
                                        node={node}
                                    />
                                )}
                                {tab === "tasks" && (
                                    <AdministrationResourceTasks
                                        cloud={cloud.id}
                                        host={node}
                                        limit={10}
                                    />
                                )}
                            </Box>
                        </>
                    )
                }
            </QueryState>
        </Stack>
    );
}
function HostNetworks({ cloud, host }: { cloud: string; host: AdminHost }) {
    const q = useAdministration<Network[]>(
        `${adminCloudPath(cloud)}/hosts/${esc(host.id)}/networks`,
    );
    return (
        <Stack spacing={3}>
            <DetailSection title="Accepted network bindings">
                <ResourceTable
                    paramPrefix="bindingNetwork."
                    rows={host.networks}
                    rowKey={(n) => n.id}
                    search={(n) => `${n.id} ${n.backend}`}
                    columns={[
                        {
                            id: "id",
                            label: "Logical network",
                            value: (n) => n.id,
                        },
                        { id: "kind", label: "Kind", value: (n) => n.kind },
                        {
                            id: "backend",
                            label: "Backend",
                            value: (n) => n.backend,
                        },
                    ]}
                    empty="No network bindings are configured."
                />
            </DetailSection>
            <DetailSection
                title="Observed backend networks"
                description="Read-only live diagnostics; these names are separate from configured logical network bindings."
            >
                <Button onClick={() => void q.refetch()}>
                    Refresh networks
                </Button>
                <QueryState query={q}>
                    {(rows) => (
                        <ResourceTable
                            paramPrefix="liveNetwork."
                            rows={rows}
                            rowKey={(n) => n.name}
                            search={(n) => n.name}
                            columns={[
                                {
                                    id: "name",
                                    label: "Backend network",
                                    value: (n) => n.name,
                                },
                            ]}
                            empty="No backend networks were observed."
                        />
                    )}
                </QueryState>
            </DetailSection>
        </Stack>
    );
}
function HostStorage({ cloud, host }: { cloud: string; host: AdminHost }) {
    const q = useAdministration<Pool[]>(
        `${adminCloudPath(cloud)}/hosts/${esc(host.id)}/pools`,
    );
    return (
        <Stack spacing={3}>
            <DetailSection title="Accepted storage bindings">
                <ResourceTable
                    paramPrefix="bindingStorage."
                    rows={host.storage}
                    rowKey={(s) => s.id}
                    search={(s) => `${s.id} ${s.pool}`}
                    columns={[
                        {
                            id: "id",
                            label: "Storage class",
                            value: (s) => s.id,
                        },
                        { id: "pool", label: "Pool", value: (s) => s.pool },
                        {
                            id: "format",
                            label: "Format",
                            value: (s) => s.format,
                        },
                    ]}
                    empty="No storage bindings are configured."
                />
            </DetailSection>
            <DetailSection
                title="Observed storage pools"
                description="Live pool capacity is independent of the accepted storage class bindings."
            >
                <Button onClick={() => void q.refetch()}>Refresh pools</Button>
                <QueryState query={q}>
                    {(rows) => (
                        <ResourceTable
                            paramPrefix="livePool."
                            rows={rows}
                            rowKey={(p) => p.name}
                            search={(p) => p.name}
                            columns={[
                                {
                                    id: "name",
                                    label: "Pool",
                                    value: (p) => p.name,
                                },
                                {
                                    id: "size",
                                    label: "Size",
                                    value: (p) => p.size_bytes,
                                    render: (p) => bytes(p.size_bytes),
                                    numeric: true,
                                },
                                {
                                    id: "used",
                                    label: "Used",
                                    value: (p) => p.used_bytes,
                                    render: (p) => bytes(p.used_bytes),
                                    numeric: true,
                                },
                                {
                                    id: "free",
                                    label: "Free",
                                    value: (p) => p.free_bytes,
                                    render: (p) => bytes(p.free_bytes),
                                    numeric: true,
                                },
                                {
                                    id: "capacity",
                                    label: "Capacity",
                                    value: (p) =>
                                        p.size_bytes
                                            ? p.used_bytes / p.size_bytes
                                            : 0,
                                    render: (p) => (
                                        <Capacity
                                            used={p.used_bytes}
                                            total={p.size_bytes}
                                            label={`${p.name} storage usage`}
                                        />
                                    ),
                                },
                            ]}
                            empty="No storage pools were observed."
                        />
                    )}
                </QueryState>
            </DetailSection>
        </Stack>
    );
}
function HostResources({ cloud, node }: { cloud: string; node: string }) {
    const machines = useAdministration<{
        items: VM[];
        unavailable_hosts: string[];
    }>(`${adminCloudPath(cloud)}/machines`);
    const volumes = useAdministration<{
        items: Volume[];
        unavailable_hosts: string[];
    }>(`${adminCloudPath(cloud)}/volumes`);
    return (
        <Stack spacing={3}>
            <Typography variant="body2" color="text.secondary">
                Resources observed on {node}, across namespaces. Each inventory
                is loaded once for the cloud.
            </Typography>
            <DetailSection title="Virtual machines">
                <QueryState query={machines}>
                    {(data) => (
                        <>
                            {data.unavailable_hosts.includes(node) && (
                                <Alert severity="warning">
                                    Machine inventory is unavailable for this
                                    host. The result may be incomplete.
                                </Alert>
                            )}
                            <ResourceTable
                                paramPrefix="hostMachine."
                                rows={data.items.filter(
                                    (m) => m.node === node && m.cloud === cloud,
                                )}
                                rowKey={(m) => m.id}
                                search={(m) =>
                                    `${m.id} ${m.name} ${m.namespace_id}`
                                }
                                columns={[
                                    {
                                        id: "name",
                                        label: "Machine",
                                        value: (m) => m.name,
                                        render: (m) => (
                                            <Link
                                                component={RouterLink}
                                                to={`${adminCloudURL(cloud)}machines/${esc(m.id)}/`}
                                            >
                                                {m.name}
                                            </Link>
                                        ),
                                    },
                                    {
                                        id: "namespace",
                                        label: "Namespace",
                                        value: (m) =>
                                            m.namespace_id || "Unassigned",
                                    },
                                    {
                                        id: "state",
                                        label: "State",
                                        value: (m) => m.state,
                                    },
                                    {
                                        id: "vcpus",
                                        label: "vCPUs",
                                        value: (m) => m.vcpus,
                                        numeric: true,
                                    },
                                    {
                                        id: "memory",
                                        label: "Memory",
                                        value: (m) => m.memory_bytes,
                                        render: (m) => bytes(m.memory_bytes),
                                        numeric: true,
                                    },
                                ]}
                                empty="No machines were observed on this host."
                            />
                        </>
                    )}
                </QueryState>
            </DetailSection>
            <DetailSection title="Volumes">
                <QueryState query={volumes}>
                    {(data) => (
                        <>
                            {data.unavailable_hosts.includes(node) && (
                                <Alert severity="warning">
                                    Volume inventory is unavailable for this
                                    host. The result may be incomplete.
                                </Alert>
                            )}
                            <ResourceTable
                                paramPrefix="hostVolume."
                                rows={data.items.filter(
                                    (v) => v.node === node && v.cloud === cloud,
                                )}
                                rowKey={(v) => v.id}
                                search={(v) =>
                                    `${v.id} ${v.name} ${v.attached_to || ""}`
                                }
                                columns={[
                                    {
                                        id: "name",
                                        label: "Volume",
                                        value: (v) => v.name,
                                        render: (v) => (
                                            <Link
                                                component={RouterLink}
                                                to={`${adminCloudURL(cloud)}volumes/${esc(v.id)}/`}
                                            >
                                                {v.name}
                                            </Link>
                                        ),
                                    },
                                    {
                                        id: "namespace",
                                        label: "Namespace",
                                        value: (v) =>
                                            v.namespace_id || "Unassigned",
                                    },
                                    {
                                        id: "pool",
                                        label: "Pool",
                                        value: (v) => v.pool,
                                    },
                                    {
                                        id: "size",
                                        label: "Size",
                                        value: (v) => v.size_bytes,
                                        render: (v) => bytes(v.size_bytes),
                                        numeric: true,
                                    },
                                    {
                                        id: "machine",
                                        label: "Attached machine",
                                        value: (v) =>
                                            v.attached_to || "Unattached",
                                        render: (v) =>
                                            v.attached_to ? (
                                                <Link
                                                    component={RouterLink}
                                                    to={`${adminCloudURL(cloud)}machines/${esc(v.attached_to)}/`}
                                                >
                                                    {v.attached_to}
                                                </Link>
                                            ) : (
                                                "Unattached"
                                            ),
                                    },
                                ]}
                                empty="No volumes were observed on this host."
                            />
                        </>
                    )}
                </QueryState>
            </DetailSection>
        </Stack>
    );
}
