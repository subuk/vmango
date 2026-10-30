import { useEffect, useState } from "react";
import {
    Alert,
    Box,
    Breadcrumbs,
    Button,
    Link,
    MenuItem,
    Stack,
    Tab,
    Tabs,
    TextField,
    Typography,
} from "@mui/material";
import { Link as RouterLink, useParams, useSearchParams } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import {
    esc,
    machineURL,
    useResource,
    useMachine,
    vmPath,
    volumePath,
    type VM,
    type Volume,
} from "../api/hooks";
import { useResourceBusy } from "../api/tasks";
import { ResourceTasks } from "./Tasks";
import { request } from "../api/client";
import {
    bytes,
    Confirm,
    Copy,
    PageTitle,
    QueryState,
    State,
} from "../components/common";
import { OperationDialog, fieldError } from "../components/OperationDialog";
import { ResourceTable } from "../components/ResourceTable";
import { DetailSection, Summary } from "../components/Information";
import { MachineActions } from "./Lists";
import type { HostCatalog } from "./MachineForm";

export function MachineDetail() {
    const { cloud = "", id = "" } = useParams();
    const q = useMachine(cloud, id);
    const [params, setParams] = useSearchParams();
    const tab = params.get("tab") || "overview";
    return (
        <>
            <Breadcrumbs sx={{ mb: 2 }}>
                <Link
                    component={RouterLink}
                    to={`/machines/?cloud=${esc(cloud)}`}
                >
                    Machines
                </Link>
                <Typography>{cloud}</Typography>
                <Typography>{id}</Typography>
            </Breadcrumbs>
            {q.data && (
                <ResourceTasks
                    resource={{ kind: "machine", scope: q.data.node, id }}
                />
            )}
            <QueryState query={q}>
                {(vm) => (
                    <>
                        <PageTitle
                            title={vm.name}
                            description={`Virtual machine in ${vm.cloud} · ${vm.zone || "No availability zone"}`}
                            action={<MachineActions vm={vm} full />}
                        />
                        <Summary
                            label="Machine summary"
                            items={[
                                {
                                    label: "Status",
                                    value: <State value={vm.state} />,
                                    detail:
                                        vm.managed === false
                                            ? "Read-only · adoption required"
                                            : "Managed machine",
                                },
                                {
                                    label: "Compute",
                                    value: `${vm.vcpus} vCPU`,
                                    detail:
                                        vm.instance_type ||
                                        "Unassigned instance type",
                                },
                                {
                                    label: "Memory",
                                    value: bytes(vm.memory_bytes),
                                    detail: "Configured guest memory",
                                },
                                {
                                    label: "Connectivity",
                                    value: `${vm.ip_addresses.length} IP${vm.ip_addresses.length === 1 ? "" : "s"}`,
                                    detail: `${vm.interfaces.length} network interface${vm.interfaces.length === 1 ? "" : "s"}`,
                                },
                            ]}
                        />
                        <Tabs
                            value={
                                ["overview", "disks", "network"].includes(tab)
                                    ? tab
                                    : "overview"
                            }
                            onChange={(_, value) =>
                                setParams((old) => {
                                    const p = new URLSearchParams(old);
                                    p.set("tab", value);
                                    return p;
                                })
                            }
                            aria-label="Machine details"
                            sx={{
                                borderBottom: 1,
                                borderColor: "divider",
                                mb: 3,
                            }}
                        >
                            {[
                                ["overview", "Overview"],
                                ["disks", "Disks"],
                                ["network", "Network"],
                            ].map(([value, label]) => (
                                <Tab
                                    key={value}
                                    value={value}
                                    label={label}
                                    id={`tab-${value}`}
                                    aria-controls={`panel-${value}`}
                                />
                            ))}
                        </Tabs>
                        {tab === "disks" ? (
                            <Box
                                role="tabpanel"
                                id="panel-disks"
                                aria-labelledby="tab-disks"
                            >
                                <Disks vm={vm} />
                            </Box>
                        ) : tab === "network" ? (
                            <Box
                                role="tabpanel"
                                id="panel-network"
                                aria-labelledby="tab-network"
                            >
                                <Interfaces vm={vm} />
                            </Box>
                        ) : (
                            <Box
                                role="tabpanel"
                                id="panel-overview"
                                aria-labelledby="tab-overview"
                            >
                                {vm.managed === false && (
                                    <Alert severity="info">
                                        This machine is read-only until
                                        explicitly adopted. Its resources are
                                        included in capacity accounting.
                                    </Alert>
                                )}
                                <Overview vm={vm} />
                            </Box>
                        )}
                    </>
                )}
            </QueryState>
        </>
    );
}
function Overview({ vm }: { vm: VM }) {
    return (
        <Stack spacing={3}>
            <Box className="detail-grid">
                <DetailSection
                    title="Identity and placement"
                    description="Where this machine runs and how it was provisioned."
                >
                    <Box component="dl" className="details">
                        <dt>Machine ID</dt>
                        <dd>
                            <span className="mono">{vm.id}</span>
                            <Copy label="Copy machine ID" text={vm.id} />
                        </dd>
                        <dt>Host</dt>
                        <dd>
                            <Link
                                component={RouterLink}
                                to={`/nodes/${esc(vm.node)}/`}
                            >
                                {vm.node}
                            </Link>
                        </dd>
                        <dt>Cloud</dt>
                        <dd>{vm.cloud}</dd>
                        <dt>Availability zone</dt>
                        <dd>{vm.zone || "Unassigned"}</dd>
                        <dt>Instance type</dt>
                        <dd>{vm.instance_type || "Unassigned"}</dd>
                        <dt>Source image</dt>
                        <dd>{vm.image || "Not recorded"}</dd>
                    </Box>
                </DetailSection>
                <DetailSection
                    title="Network access"
                    description="Reported IP addresses and connection details."
                >
                    {vm.ip_addresses.length ? (
                        <Stack spacing={1}>
                            {vm.ip_addresses.map((ip) => (
                                <Box className="address-row" key={ip}>
                                    <span className="mono">{ip}</span>
                                    <Copy label={`Copy IP ${ip}`} text={ip} />
                                </Box>
                            ))}
                        </Stack>
                    ) : (
                        <Typography color="text.secondary">
                            No IP addresses reported.
                        </Typography>
                    )}
                    <Typography
                        variant="body2"
                        color="text.secondary"
                        sx={{ mt: 2 }}
                    >
                        Open the Network tab for interfaces, MAC addresses and
                        network assignments.
                    </Typography>
                </DetailSection>
                <DetailSection
                    title="Hardware and boot"
                    description="Guest hardware, startup and memory options."
                >
                    <Box component="dl" className="details">
                        <dt>Architecture</dt>
                        <dd>{vm.arch}</dd>
                        <dt>Firmware</dt>
                        <dd>{vm.firmware || "Default"}</dd>
                        <dt>Disk controller</dt>
                        <dd>{vm.disk_controller || "Default"}</dd>
                        <dt>Autostart</dt>
                        <dd>{vm.autostart ? "Enabled" : "Disabled"}</dd>
                        <dt>Guest agent</dt>
                        <dd>{vm.guest_agent ? "Enabled" : "Disabled"}</dd>
                        <dt>Hugepages</dt>
                        <dd>{vm.hugepages ? "Enabled" : "Disabled"}</dd>
                    </Box>
                </DetailSection>
                <DetailSection
                    title="Console and CPU placement"
                    description="Display configuration and host CPU affinity."
                >
                    <Box component="dl" className="details">
                        <dt>Graphics</dt>
                        <dd>{vm.graphics || "None"}</dd>
                        <dt>Graphics listen</dt>
                        <dd className="mono">
                            {vm.graphics_listen || "Default"}
                        </dd>
                        <dt>Video model</dt>
                        <dd>{vm.video_model || "Default"}</dd>
                        <dt>Emulator CPUs</dt>
                        <dd>
                            {vm.cpu_pinning?.emulator.join(", ") ||
                                "Not pinned"}
                        </dd>
                        <dt>vCPU pinning</dt>
                        <dd>
                            {vm.cpu_pinning?.vcpus.length
                                ? vm.cpu_pinning.vcpus.map((p) => (
                                      <div key={p.vcpu}>
                                          vCPU {p.vcpu}: host CPUs{" "}
                                          {p.host_cpus.join(", ")}
                                      </div>
                                  ))
                                : "Not pinned"}
                        </dd>
                    </Box>
                </DetailSection>
            </Box>
            <DetailSection
                title="Configured SSH keys"
                description="Public keys configured during machine provisioning."
            >
                {vm.configured_keys === null ? (
                    <Typography color="text.secondary">
                        Key configuration is unavailable.
                    </Typography>
                ) : !vm.configured_keys?.length ? (
                    <Typography color="text.secondary">
                        No configured keys.
                    </Typography>
                ) : (
                    vm.configured_keys.map((k) => (
                        <Box className="key-row" key={k.fingerprint}>
                            <Box sx={{ minWidth: 0 }}>
                                <Typography sx={{ fontWeight: 600 }}>
                                    {k.comment || "SSH key"}
                                </Typography>
                                <Typography
                                    variant="body2"
                                    className="mono"
                                    sx={{ overflowWrap: "anywhere" }}
                                    color="text.secondary"
                                >
                                    {k.fingerprint}
                                </Typography>
                            </Box>
                            <Copy
                                label={`Copy fingerprint ${k.fingerprint}`}
                                text={k.fingerprint}
                            />
                        </Box>
                    ))
                )}
            </DetailSection>
        </Stack>
    );
}

function Disks({ vm }: { vm: VM }) {
    const blocked = useResourceBusy({
        kind: "machine",
        scope: vm.node,
        id: vm.id,
    });
    const conflict =
        !!blocked.task || blocked.unavailable || vm.managed === false;
    const volumes = useResource<Volume[]>(
        `/api/v1/clouds/${esc(vm.cloud)}/volumes?host=${esc(vm.node)}`,
    );
    const [add, setAdd] = useState(false);
    const [createDisk, setCreateDisk] = useState(false);
    const [remove, setRemove] = useState<string>();
    const queries = useQueryClient();
    // A visible list omits hidden disks; authorized detail reads retain their labels.
    const disks = vm.disks.map((d) => ({
        ...d,
        volume: volumes.data?.find((v) => v.id === d.volume_id),
    }));
    return (
        <>
            <Button
                onClick={() => setAdd(true)}
                disabled={conflict || vm.state !== "stopped"}
                sx={{ mb: 2 }}
            >
                Attach disk
            </Button>
            <Button
                disabled={conflict || vm.state !== "stopped"}
                onClick={() => setCreateDisk(true)}
                sx={{ mb: 2 }}
            >
                Create and attach disk
            </Button>
            {createDisk && (
                <CreateDisk vm={vm} onClose={() => setCreateDisk(false)} />
            )}
            {vm.state !== "stopped" && (
                <Typography color="text.secondary" sx={{ mb: 2 }}>
                    Stop the machine before attaching or detaching disks.
                </Typography>
            )}
            <ResourceTable
                rows={disks}
                rowKey={(d) => d.volume_id}
                search={(d) => `${d.alias} ${d.volume?.name || ""}`}
                columns={[
                    {
                        id: "name",
                        label: "Volume",
                        value: (d) => d.volume?.name || d.alias || d.volume_id,
                        render: (d) => (
                            <DiskLabel
                                cloud={vm.cloud}
                                node={vm.node}
                                id={d.volume_id}
                                known={d.volume}
                            />
                        ),
                    },
                    {
                        id: "alias",
                        label: "Alias",
                        value: (d) => d.alias || "—",
                    },
                    {
                        id: "device",
                        label: "Device",
                        value: (d) => d.device || "disk",
                    },
                    {
                        id: "bus",
                        label: "Bus",
                        value: (d) => d.bus || "virtio",
                    },
                    {
                        id: "size",
                        label: "Size",
                        value: (d) => d.volume?.size_bytes || 0,
                        render: (d) =>
                            d.volume ? bytes(d.volume.size_bytes) : "—",
                        numeric: true,
                    },
                ]}
                actions={(d) => (
                    <Button
                        disabled={conflict || vm.state !== "stopped"}
                        onClick={() => setRemove(d.volume_id)}
                    >
                        Detach
                    </Button>
                )}
                empty="No disks attached."
            />
            {add && <AttachDisk vm={vm} onClose={() => setAdd(false)} />}
            {remove && (
                <Confirm
                    title="Detach disk?"
                    action="Detach disk"
                    onClose={() => setRemove(undefined)}
                    run={async () => {
                        await request(
                            `${vmPath(vm.cloud, vm.id)}/disks/${esc(remove)}`,
                            "DELETE",
                        );
                        await queries.invalidateQueries();
                    }}
                >
                    Detach this disk from {vm.node}/{vm.name}. The volume and
                    its data will be kept.
                </Confirm>
            )}
        </>
    );
}
function DiskLabel({
    cloud,
    node,
    id,
    known,
}: {
    node: string;
    id: string;
    known?: Volume;
    cloud: string;
}) {
    const q = useResource<Volume>(volumePath(node, id, cloud), !known, false);
    return (
        <>
            {known?.name ||
                q.data?.name ||
                (q.isError ? "Unavailable volume" : "Loading volume…")}
        </>
    );
}
function CreateDisk({ vm, onClose }: { vm: VM; onClose: () => void }) {
    const catalog = useResource<HostCatalog>(
        `/api/v1/hosts/${esc(vm.node)}/catalog`,
    );
    const [name, setName] = useState("");
    const [storage, setStorage] = useState("");
    const [size, setSize] = useState("10");
    useEffect(() => {
        setStorage((old) =>
            catalog.data?.storage_classes.some((s) => s.id === old)
                ? old
                : catalog.data?.storage_classes[0]?.id || "",
        );
    }, [catalog.data]);
    return (
        <OperationDialog
            title={`Create disk for ${vm.name}`}
            action="Create and attach disk"
            onClose={onClose}
            disabled={
                !storage || vm.managed === false || vm.state !== "stopped"
            }
            run={() =>
                request(
                    `/api/v1/clouds/${esc(vm.cloud)}/machines/${esc(vm.id)}/disks`,
                    "POST",
                    {
                        name,
                        storage_class: storage,
                        size_bytes: Math.round(Number(size) * 2 ** 30),
                    },
                )
            }
        >
            {(error) => (
                <>
                    <TextField
                        required
                        label="Disk name"
                        value={name}
                        onChange={(e) => setName(e.target.value)}
                    />
                    <TextField
                        required
                        label="Size (GiB)"
                        type="number"
                        slotProps={{ htmlInput: { min: 0.001, step: "any" } }}
                        value={size}
                        onChange={(e) => setSize(e.target.value)}
                        {...fieldError(error, "size_bytes")}
                    />
                    <QueryState query={catalog}>
                        {(c) => (
                            <TextField
                                select
                                required
                                label="Storage class"
                                value={storage}
                                onChange={(e) => setStorage(e.target.value)}
                            >
                                {c.storage_classes.map((s) => (
                                    <MenuItem key={s.id} value={s.id}>
                                        {s.name}
                                    </MenuItem>
                                ))}
                            </TextField>
                        )}
                    </QueryState>
                    <Typography>
                        The disk will be created on the machine's current host.
                    </Typography>
                </>
            )}
        </OperationDialog>
    );
}
function AttachDisk({ vm, onClose }: { vm: VM; onClose: () => void }) {
    const blocked = useResourceBusy({
        kind: "machine",
        scope: vm.node,
        id: vm.id,
    });
    const q = useResource<Volume[]>(
        `/api/v1/clouds/${esc(vm.cloud)}/volumes?host=${esc(vm.node)}&usage=attachable`,
        true,
        false,
    );
    const [id, setID] = useState("");
    const [alias, setAlias] = useState("");
    const [device, setDevice] = useState("disk");
    const [bus, setBus] = useState("virtio");
    return (
        <OperationDialog
            disabled={!!blocked.task || blocked.unavailable}
            title={`Attach disk to ${vm.name}`}
            action="Attach disk"
            onClose={onClose}
            run={() =>
                request(`${vmPath(vm.cloud, vm.id)}/disks/attach`, "POST", {
                    volume_id: id,
                    alias,
                    device,
                    bus,
                })
            }
        >
            {(error) => (
                <>
                    <QueryState query={q}>
                        {(volumes) => (
                            <TextField
                                select
                                label="Volume"
                                name="volume_id"
                                required
                                value={id}
                                onChange={(e) => setID(e.target.value)}
                                {...fieldError(error, "volume_id")}
                            >
                                <MenuItem value="">Choose a volume</MenuItem>
                                {volumes.map((v) => (
                                    <MenuItem key={v.id} value={v.id}>
                                        {v.name} · {bytes(v.size_bytes)}
                                    </MenuItem>
                                ))}
                            </TextField>
                        )}
                    </QueryState>
                    <TextField
                        label="Alias (optional)"
                        value={alias}
                        onChange={(e) => setAlias(e.target.value)}
                    />
                    <TextField
                        select
                        label="Device"
                        value={device}
                        onChange={(e) => setDevice(e.target.value)}
                    >
                        {["disk", "cdrom"].map((d) => (
                            <MenuItem value={d} key={d}>
                                {d}
                            </MenuItem>
                        ))}
                    </TextField>
                    <TextField
                        select
                        label="Bus"
                        value={bus}
                        onChange={(e) => setBus(e.target.value)}
                    >
                        {["virtio", "scsi", "ide"].map((b) => (
                            <MenuItem value={b} key={b}>
                                {b}
                            </MenuItem>
                        ))}
                    </TextField>
                </>
            )}
        </OperationDialog>
    );
}
function Interfaces({ vm }: { vm: VM }) {
    const blocked = useResourceBusy({
        kind: "machine",
        scope: vm.node,
        id: vm.id,
    });
    const conflict =
        !!blocked.task || blocked.unavailable || vm.managed === false;
    const [add, setAdd] = useState(false);
    const [remove, setRemove] = useState<string>();
    const queries = useQueryClient();
    return (
        <>
            <Button
                disabled={conflict || vm.state !== "stopped"}
                onClick={() => setAdd(true)}
                sx={{ mb: 2 }}
            >
                Attach interface
            </Button>
            {vm.state !== "stopped" && (
                <Typography color="text.secondary" sx={{ mb: 2 }}>
                    Stop the machine before changing network interfaces.
                </Typography>
            )}
            <ResourceTable
                rows={vm.interfaces}
                rowKey={(i) => i.mac || i.network}
                search={(i) => `${i.network} ${i.mac}`}
                columns={[
                    {
                        id: "network",
                        label: "Network",
                        value: (i) => i.network,
                    },
                    { id: "mac", label: "MAC", value: (i) => i.mac || "—" },
                    {
                        id: "model",
                        label: "Model",
                        value: (i) => i.model || "Default",
                    },
                    { id: "vlan", label: "VLAN", value: (i) => i.vlan || "—" },
                    {
                        id: "ip",
                        label: "IP addresses",
                        value: (i) => i.ip_addresses.join(", "),
                        render: (i) =>
                            i.ip_addresses.map((ip) => (
                                <div key={ip}>
                                    <span className="mono">{ip}</span>
                                    <Copy label={`Copy IP ${ip}`} text={ip} />
                                </div>
                            )),
                    },
                ]}
                actions={(i) => (
                    <Button
                        disabled={conflict || vm.state !== "stopped" || !i.mac}
                        onClick={() => setRemove(i.mac)}
                    >
                        Detach
                    </Button>
                )}
                empty="No interfaces attached."
            />
            {add && <AttachInterface vm={vm} onClose={() => setAdd(false)} />}
            {remove && (
                <Confirm
                    title="Detach interface?"
                    action="Detach interface"
                    onClose={() => setRemove(undefined)}
                    run={async () => {
                        await request(
                            `${vmPath(vm.cloud, vm.id)}/interfaces/${esc(remove)}`,
                            "DELETE",
                        );
                        await queries.invalidateQueries();
                    }}
                >
                    Detach {remove} from {vm.node}/{vm.name}. The guest will
                    lose access through this interface.
                </Confirm>
            )}
        </>
    );
}
function AttachInterface({ vm, onClose }: { vm: VM; onClose: () => void }) {
    const blocked = useResourceBusy({
        kind: "machine",
        scope: vm.node,
        id: vm.id,
    });
    const choices = useResource<HostCatalog>(
        `/api/v1/hosts/${esc(vm.node)}/catalog`,
    );
    const [network, setNetwork] = useState("");
    useEffect(() => {
        const entries = choices.data?.networks || [];
        setNetwork((old) =>
            entries.some((n) => n.id === old) ? old : entries[0]?.id || "",
        );
    }, [choices.data]);
    return (
        <OperationDialog
            title={`Attach interface to ${vm.name}`}
            action="Attach interface"
            disabled={
                vm.managed === false ||
                !!blocked.task ||
                blocked.unavailable ||
                !network ||
                choices.isError
            }
            onClose={onClose}
            run={() =>
                request(`${vmPath(vm.cloud, vm.id)}/interfaces`, "POST", {
                    network,
                })
            }
        >
            {(error) => (
                <>
                    <QueryState query={choices}>
                        {(catalog) => (
                            <TextField
                                select
                                label="Network"
                                required
                                value={network}
                                onChange={(e) => setNetwork(e.target.value)}
                                {...fieldError(error, "network")}
                            >
                                {catalog.networks.map((n) => (
                                    <MenuItem key={n.id} value={n.id}>
                                        {n.name}
                                    </MenuItem>
                                ))}
                            </TextField>
                        )}
                    </QueryState>
                    <Typography>
                        The logical network is resolved on this machine's host.
                    </Typography>
                </>
            )}
        </OperationDialog>
    );
}
