import { useEffect, useRef, useState } from "react";
import {
    Alert,
    Box,
    Button,
    Link,
    MenuItem,
    Stack,
    TextField,
    Typography,
} from "@mui/material";
import {
    Link as RouterLink,
    useLocation,
    useParams,
    useSearchParams,
} from "react-router";
import { useQuery } from "@tanstack/react-query";
import { useNamespaces, useWorkspace } from "../app/Workspace";
import { request } from "../api/client";
import { adminCloudPath, adminCloudURL } from "../api/administration";
import type { VM, Volume, VMDetail, VolumeDetail } from "../api/hooks";
import { volumeResource } from "../api/tasks";
import { PageTitle, QueryState, bytes, State } from "../components/common";
import { ResourceTable, type Column } from "../components/ResourceTable";
import { AdministrationResourceTasks } from "./AdministrationOperations";
import "./administration-inventory.css";

type Kind = "machines" | "volumes";
type Resource = VM | Volume;
const namespace = (row: Resource) => row.namespace_id || "Unassigned";
function useInventory<T>(path: string) {
    const { principal } = useWorkspace();
    return useQuery({
        queryKey: ["administration", principal, path],
        queryFn: ({ signal }) => request<T>(path, "GET", undefined, signal),
        refetchInterval: false,
        refetchOnWindowFocus: false,
    });
}
export function AdministrationInventory({ kind }: { kind: Kind }) {
    const { cloud = "", id } = useParams();
    const { principal } = useWorkspace();
    return id ? (
        <InventoryDetail
            key={`${principal}:${cloud}:${kind}:${id}`}
            kind={kind}
            cloud={cloud}
            id={id}
        />
    ) : (
        <InventoryList
            key={`${principal}:${cloud}:${kind}`}
            kind={kind}
            cloud={cloud}
        />
    );
}
function InventoryList({ kind, cloud }: { kind: Kind; cloud: string }) {
    const { principal } = useWorkspace();
    const [params, setParams] = useSearchParams();
    const selected = params.get("selected") || "";
    const trigger = useRef<HTMLElement | null>(null);
    const list = useRef<HTMLDivElement>(null);
    const q = useInventory<{ items: Resource[]; unavailable_hosts: string[] }>(
        `${adminCloudPath(cloud)}/${kind}`,
    );
    function select(row: Resource) {
        trigger.current =
            document.activeElement instanceof HTMLElement
                ? document.activeElement
                : null;
        setParams(
            (old) => {
                const next = new URLSearchParams(old);
                next.set("selected", row.id);
                return next;
            },
            { replace: true },
        );
    }
    function close() {
        setParams(
            (old) => {
                const next = new URLSearchParams(old);
                next.delete("selected");
                return next;
            },
            { replace: true },
        );
        if (trigger.current?.isConnected && trigger.current !== document.body)
            trigger.current.focus();
        else
            list.current
                ?.querySelector<HTMLElement>(
                    'tr[aria-selected="true"], [aria-label="Scrollable resources"]',
                )
                ?.focus();
    }
    const common: Column<Resource>[] = [
        {
            id: "name",
            label: "Name",
            value: (r) => r.name,
            render: (r) => (
                <Button
                    className="inventory-resource-name"
                    onClick={() => select(r)}
                    aria-pressed={selected === r.id}
                >
                    {r.name}
                </Button>
            ),
        },
        ...(kind === "machines"
            ? [
                  {
                      id: "state",
                      label: "State",
                      value: (r: Resource) => ("state" in r ? r.state : ""),
                      render: (r: Resource) =>
                          "state" in r ? <State value={r.state} /> : null,
                  },
              ]
            : []),
        { id: "namespace", label: "Namespace", value: namespace },
        { id: "host", label: "Host", value: (r) => r.node || "—" },
    ];
    const columns: Column<Resource>[] = [
        ...common,
        ...(kind === "machines"
            ? [
                  {
                      id: "vcpus",
                      label: "vCPU",
                      value: (r: Resource) => ("vcpus" in r ? r.vcpus : 0),
                      numeric: true,
                  },
                  {
                      id: "memory",
                      label: "RAM",
                      value: (r: Resource) =>
                          "memory_bytes" in r ? r.memory_bytes : 0,
                      numeric: true,
                      render: (r: Resource) =>
                          "memory_bytes" in r ? bytes(r.memory_bytes) : "—",
                  },
                  {
                      id: "ip",
                      label: "IP addresses",
                      value: (r: Resource) =>
                          "ip_addresses" in r
                              ? (r.ip_addresses || []).join(", ") || "—"
                              : "—",
                  },
              ]
            : [
                  {
                      id: "size",
                      label: "Size",
                      value: (r: Resource) =>
                          "size_bytes" in r ? r.size_bytes : 0,
                      numeric: true,
                      render: (r: Resource) =>
                          "size_bytes" in r ? bytes(r.size_bytes) : "—",
                  },
                  {
                      id: "class",
                      label: "Storage class / pool",
                      value: (r: Resource) =>
                          "storage_class" in r
                              ? `${r.storage_class || "—"} / ${r.pool || "—"}`
                              : "—",
                  },
                  {
                      id: "attachment",
                      label: "Attachment",
                      value: (r: Resource) =>
                          "state" in r ? "" : r.attached_to || "Unattached",
                  },
                  {
                      id: "clone",
                      label: "Clone mode",
                      value: (r: Resource) =>
                          "clone_mode" in r ? r.clone_mode || "—" : "—",
                  },
                  {
                      id: "protection",
                      label: "Protected",
                      value: (r: Resource) =>
                          "protected" in r && r.protected ? "Yes" : "No",
                  },
              ]),
        { id: "id", label: "ID", value: (r) => r.id, defaultHidden: true },
    ];
    return (
        <>
            <PageTitle
                title={kind === "machines" ? "Machines" : "Volumes"}
                description="Read-only inventory across all namespaces. Select a resource to inspect it."
                action={
                    <Button onClick={() => void q.refetch()}>Refresh</Button>
                }
            />
            <QueryState query={q}>
                {(data) => (
                    <>
                        {!!data.unavailable_hosts?.length && (
                            <Alert severity="warning" sx={{ mb: 2 }}>
                                {data.items.length
                                    ? "Inventory is incomplete."
                                    : "Live inventory is unavailable; an empty inventory cannot be confirmed."}{" "}
                                Unavailable hosts:{" "}
                                {data.unavailable_hosts.join(", ")}
                            </Alert>
                        )}
                        <Box
                            className={`inventory-workspace${selected ? " has-selection" : ""}`}
                        >
                            <Box
                                ref={list}
                                className="inventory-table"
                                sx={{
                                    "& .MuiTableHead-root": {
                                        bgcolor: "background.paper",
                                    },
                                }}
                            >
                                <ResourceTable
                                    rows={data.items}
                                    columns={columns}
                                    rowKey={(r) => r.id}
                                    search={(r) =>
                                        `${r.name} ${r.id} ${namespace(r)} ${r.node} ${"ip_addresses" in r ? (r.ip_addresses || []).join(" ") : `${r.storage_class} ${r.pool} ${r.attached_to || ""}`}`
                                    }
                                    node={(r) => r.node}
                                    filters={[
                                        {
                                            id: "inventory_namespace",
                                            label: "Namespace",
                                            value: namespace,
                                            allLabel: "All namespaces",
                                        },
                                        kind === "machines"
                                            ? {
                                                  id: "state",
                                                  label: "State",
                                                  value: (r) =>
                                                      "state" in r
                                                          ? r.state
                                                          : "",
                                                  allLabel: "All states",
                                              }
                                            : {
                                                  id: "attachment",
                                                  label: "Attachment",
                                                  value: (r) =>
                                                      "state" in r
                                                          ? ""
                                                          : r.attached_to
                                                            ? "attached"
                                                            : "unattached",
                                                  allLabel: "Any attachment",
                                                  options: [
                                                      {
                                                          value: "attached",
                                                          label: "Attached",
                                                      },
                                                      {
                                                          value: "unattached",
                                                          label: "Unattached",
                                                      },
                                                  ],
                                              },
                                    ]}
                                    preferenceKey={`${principal}:administration:${kind}`}
                                    scrollKey={`${principal}:${cloud}:${kind}`}
                                    selectedKey={selected}
                                    onSelect={select}
                                    empty={
                                        data.unavailable_hosts?.length
                                            ? "No resources could be retrieved from the available hosts."
                                            : `No ${kind} in this cloud.`
                                    }
                                />
                            </Box>
                            {selected && (
                                <InventoryInspector
                                    key={selected}
                                    kind={kind}
                                    cloud={cloud}
                                    id={selected}
                                    onClose={close}
                                />
                            )}
                        </Box>
                    </>
                )}
            </QueryState>
        </>
    );
}
function InventoryInspector({
    kind,
    cloud,
    id,
    onClose,
}: {
    kind: Kind;
    cloud: string;
    id: string;
    onClose: () => void;
}) {
    const location = useLocation();
    const pane = useRef<HTMLDivElement>(null);
    useEffect(() => {
        pane.current?.focus({ preventScroll: window.innerWidth >= 1400 });
    }, [id]);
    return (
        <Box
            component="aside"
            className="inventory-inspector"
            sx={{
                bgcolor: "background.paper",
                border: 1,
                borderColor: "divider",
                "&:focus-visible": {
                    outline: "2px solid",
                    outlineColor: "primary.main",
                },
            }}
            ref={pane}
            tabIndex={-1}
            aria-label={
                kind === "machines" ? "Machine inspector" : "Volume inspector"
            }
            onKeyDown={(e) => {
                if (e.key === "Escape") {
                    e.stopPropagation();
                    onClose();
                }
            }}
        >
            <Stack
                direction="row"
                sx={{ justifyContent: "space-between", gap: 1, mb: 2 }}
            >
                <Button
                    component={RouterLink}
                    to={`${adminCloudURL(cloud)}${kind}/${encodeURIComponent(id)}/${location.search}`}
                >
                    Full details
                </Button>
                <Button onClick={onClose}>Close inspector</Button>
            </Stack>
            <ResourceDetails kind={kind} cloud={cloud} id={id} />
        </Box>
    );
}
function InventoryDetail({
    kind,
    cloud,
    id,
}: {
    kind: Kind;
    cloud: string;
    id: string;
}) {
    const location = useLocation();
    const back = new URLSearchParams(location.search);
    back.set("selected", id);
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
                        to={`${adminCloudURL(cloud)}${kind}/?${back}`}
                    >
                        Back to inventory
                    </Button>
                }
            />
            <ResourceDetails kind={kind} cloud={cloud} id={id} />
        </>
    );
}
function AssignmentPreview({
    kind,
    cloud,
    id,
}: {
    kind: Kind;
    cloud: string;
    id: string;
}) {
    const namespaces = useNamespaces();
    const [destination, setDestination] = useState("");
    return (
        <QueryState query={namespaces}>
            {(data) => (
                <Stack spacing={2}>
                    <Typography>
                        Review ownership and dependencies before assigning this
                        resource.
                    </Typography>
                    <TextField
                        select
                        label="Destination namespace"
                        value={destination}
                        onChange={(e) => setDestination(e.target.value)}
                    >
                        <MenuItem value="" disabled>
                            Choose namespace
                        </MenuItem>
                        {data.items
                            .filter((n) => n.clouds?.includes(cloud))
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
        </QueryState>
    );
}
function ResourceDetails({
    kind,
    cloud,
    id,
}: {
    kind: Kind;
    cloud: string;
    id: string;
}) {
    const [assignment, setAssignment] = useState(false);
    const q = useInventory<VMDetail | VolumeDetail>(
        `${adminCloudPath(cloud)}/${kind}/${encodeURIComponent(id)}`,
    );
    return (
        <QueryState query={q}>
            {(r) => (
                <Stack spacing={2}>
                    <Typography variant="h2">{r.name}</Typography>
                    <Box component="dl" className="details inventory-facts">
                        <dt>Namespace</dt>
                        <dd>{namespace(r)}</dd>
                        <dt>Host</dt>
                        <dd>{r.node || "—"}</dd>
                        <dt>Cloud / zone</dt>
                        <dd>
                            {r.cloud} / {r.zone || "—"}
                        </dd>
                        {"state" in r ? (
                            <>
                                <dt>State</dt>
                                <dd>
                                    <State value={r.state} />
                                </dd>
                                <dt>Allocated vCPU / RAM</dt>
                                <dd>
                                    {r.vcpus} / {bytes(r.memory_bytes)}
                                </dd>
                                <dt>IP addresses</dt>
                                <dd>
                                    {(r.ip_addresses || []).join(", ") || "—"}
                                </dd>
                                <dt>Instance type</dt>
                                <dd>{r.instance_type || "—"}</dd>
                                <dt>Image</dt>
                                <dd>{r.image || "—"}</dd>
                                <dt>Architecture</dt>
                                <dd>{r.arch || "—"}</dd>
                                <dt>Managed</dt>
                                <dd>{r.managed ? "Yes" : "No"}</dd>
                            </>
                        ) : (
                            <>
                                <dt>Size</dt>
                                <dd>{bytes(r.size_bytes)}</dd>
                                <dt>Storage class / pool</dt>
                                <dd>
                                    {r.storage_class || "—"} / {r.pool || "—"}
                                </dd>
                                <dt>Format / clone mode</dt>
                                <dd>
                                    {r.format} / {r.clone_mode || "—"}
                                </dd>
                                <dt>Attached to</dt>
                                <dd>
                                    {r.attached_to ? (
                                        <Link
                                            component={RouterLink}
                                            to={`${adminCloudURL(cloud)}machines/${encodeURIComponent(r.attached_to)}/`}
                                        >
                                            {r.attached_to}
                                        </Link>
                                    ) : (
                                        "Unattached"
                                    )}
                                </dd>
                                <dt>Protected</dt>
                                <dd>{r.protected ? "Yes" : "No"}</dd>
                            </>
                        )}
                        <dt>ID</dt>
                        <dd className="mono">{r.id}</dd>
                        {r.backend_name && (
                            <>
                                <dt>Backend name</dt>
                                <dd>{r.backend_name}</dd>
                            </>
                        )}
                    </Box>
                    {!r.namespace_id && (
                        <Box>
                            {assignment ? (
                                <AssignmentPreview
                                    kind={kind}
                                    cloud={cloud}
                                    id={id}
                                />
                            ) : (
                                <Button onClick={() => setAssignment(true)}>
                                    Assign to namespace…
                                </Button>
                            )}
                        </Box>
                    )}
                    {"disks" in r && (
                        <>
                            <Typography variant="h2">Disks</Typography>
                            <ResourceTable
                                paramPrefix="disk_"
                                rows={r.disks || []}
                                rowKey={(d) => d.volume_id}
                                search={(d) => `${d.volume_id} ${d.alias}`}
                                columns={[
                                    {
                                        id: "name",
                                        label: "Volume",
                                        value: (d) => d.alias || d.volume_id,
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
                                empty="No disks attached."
                            />
                            <Typography variant="h2">Interfaces</Typography>
                            <ResourceTable
                                paramPrefix="interface_"
                                rows={r.interfaces || []}
                                rowKey={(i) => `${i.network}:${i.mac}`}
                                search={(i) =>
                                    `${i.network} ${i.mac} ${(i.ip_addresses || []).join(" ")}`
                                }
                                columns={[
                                    {
                                        id: "network",
                                        label: "Network",
                                        value: (i) => i.network,
                                    },
                                    {
                                        id: "mac",
                                        label: "MAC",
                                        value: (i) => i.mac || "—",
                                    },
                                    {
                                        id: "ip",
                                        label: "IP addresses",
                                        value: (i) =>
                                            (i.ip_addresses || []).join(", ") ||
                                            "—",
                                    },
                                ]}
                                empty="No interfaces attached."
                            />
                        </>
                    )}
                    <AdministrationResourceTasks
                        cloud={cloud}
                        resource={
                            "state" in r
                                ? { kind: "machine", scope: r.node, id: r.id }
                                : volumeResource(r.node, r.id)
                        }
                    />
                </Stack>
            )}
        </QueryState>
    );
}
