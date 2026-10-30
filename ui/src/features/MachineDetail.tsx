import { useState } from "react";
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
    vmPath,
    volumePath,
    type VM,
    type Volume,
    type Network,
    type Capabilities,
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
import { MachineActions } from "./Lists";

export function MachineDetail() {
    const { node = "", id = "" } = useParams();
    const q = useResource<VM>(vmPath(node, id));
    const [params, setParams] = useSearchParams();
    const tab = params.get("tab") || "overview";
    return (
        <>
            <Breadcrumbs sx={{ mb: 2 }}>
                <Link
                    component={RouterLink}
                    to={`/machines/?node=${esc(node)}`}
                >
                    Machines
                </Link>
                <Typography>{node}</Typography>
                <Typography>{id}</Typography>
            </Breadcrumbs>
            <ResourceTasks resource={{ kind: "machine", scope: node, id }} />
            <QueryState query={q}>
                {(vm) => (
                    <>
                        <PageTitle
                            title={id}
                            action={<MachineActions vm={vm} full />}
                        />
                        <Stack
                            direction="row"
                            spacing={3}
                            sx={{ flexWrap: "wrap", mb: 3 }}
                        >
                            <Typography>Node: {node}</Typography>
                            <State value={vm.state} />
                            <Typography>
                                {vm.vcpus} vCPU · {bytes(vm.memory_bytes)}
                            </Typography>
                            {vm.ip_addresses.map((ip) => (
                                <Box key={ip}>
                                    <span className="mono">{ip}</span>
                                    <Copy label={`Copy IP ${ip}`} text={ip} />
                                </Box>
                            ))}
                        </Stack>
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
                <dt>Graphics</dt>
                <dd>{vm.graphics}</dd>
                <dt>Graphics listen</dt>
                <dd className="mono">{vm.graphics_listen || "Default"}</dd>
                <dt>Video model</dt>
                <dd>{vm.video_model}</dd>
                <dt>Emulator CPUs</dt>
                <dd>{vm.cpu_pinning?.emulator.join(", ") || "Not pinned"}</dd>
                <dt>vCPU pinning</dt>
                <dd>
                    {vm.cpu_pinning?.vcpus.map((p) => (
                        <div key={p.vcpu}>
                            vCPU {p.vcpu}: host CPUs {p.host_cpus.join(", ")}
                        </div>
                    )) || "Not pinned"}
                </dd>
            </Box>
            <Box>
                <Typography variant="h2" sx={{ mb: 2 }}>
                    Configured SSH keys
                </Typography>
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
                        <Typography component="div" key={k.fingerprint}>
                            <span className="mono">{k.fingerprint}</span>{" "}
                            {k.comment}
                            <Copy
                                label={`Copy fingerprint ${k.fingerprint}`}
                                text={k.fingerprint}
                            />
                        </Typography>
                    ))
                )}
            </Box>
        </Stack>
    );
}
function Disks({ vm }: { vm: VM }) {
    const blocked = useResourceBusy({
        kind: "machine",
        scope: vm.node,
        id: vm.id,
    });
    const conflict = !!blocked.task || blocked.unavailable;
    const volumes = useResource<Volume[]>(
        `/api/v1/nodes/${esc(vm.node)}/volumes`,
    );
    const [add, setAdd] = useState(false);
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
                            `${vmPath(vm.node, vm.id)}/disks/${esc(remove)}`,
                            "DELETE",
                        );
                        await queries.invalidateQueries();
                    }}
                >
                    Detach this disk from {vm.node}/{vm.id}. The volume and its
                    data will be kept.
                </Confirm>
            )}
        </>
    );
}
function DiskLabel({
    node,
    id,
    known,
}: {
    node: string;
    id: string;
    known?: Volume;
}) {
    const q = useResource<Volume>(volumePath(node, id), !known, false);
    return (
        <>
            {known?.name ||
                q.data?.name ||
                (q.isError ? "Unavailable volume" : "Loading volume…")}
        </>
    );
}
function AttachDisk({ vm, onClose }: { vm: VM; onClose: () => void }) {
    const blocked = useResourceBusy({
        kind: "machine",
        scope: vm.node,
        id: vm.id,
    });
    const q = useResource<Volume[]>(
        `/api/v1/nodes/${esc(vm.node)}/volumes?usage=attachable`,
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
            title={`Attach disk to ${vm.id}`}
            action="Attach disk"
            onClose={onClose}
            run={() =>
                request(`${vmPath(vm.node, vm.id)}/disks`, "POST", {
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
                                        {v.pool}/{v.name} ·{" "}
                                        {bytes(v.size_bytes)}
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
    const conflict = !!blocked.task || blocked.unavailable;
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
                            `${vmPath(vm.node, vm.id)}/interfaces/${esc(remove)}`,
                            "DELETE",
                        );
                        await queries.invalidateQueries();
                    }}
                >
                    Detach {remove} from {vm.node}/{vm.id}. The guest will lose
                    access through this interface.
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
    const networks = useResource<Network[]>(
        `/api/v1/nodes/${esc(vm.node)}/networks`,
        true,
        false,
    );
    const capabilities = useResource<Capabilities>(
        `/api/v1/nodes/${esc(vm.node)}/capabilities?arch=${esc(vm.arch)}`,
        true,
        false,
    );
    const [network, setNetwork] = useState("");
    const [model, setModel] = useState("virtio");
    const [mac, setMAC] = useState("");
    const [vlan, setVLAN] = useState("0");
    const options = [
        ...new Set([
            ...(networks.data || []).map((n) => n.name),
            ...Object.keys(capabilities.data?.network_models || {}),
        ]),
    ];
    return (
        <OperationDialog
            disabled={!!blocked.task || blocked.unavailable}
            title={`Attach interface to ${vm.id}`}
            action="Attach interface"
            onClose={onClose}
            run={() =>
                request(`${vmPath(vm.node, vm.id)}/interfaces`, "POST", {
                    network,
                    model,
                    mac: mac || undefined,
                    vlan: Number(vlan),
                })
            }
        >
            {(error) => (
                <>
                    <TextField
                        select
                        label="Network"
                        name="network"
                        required
                        value={network}
                        onChange={(e) => setNetwork(e.target.value)}
                        {...fieldError(error, "network")}
                    >
                        <MenuItem value="">Choose a network</MenuItem>
                        {options.map((n) => (
                            <MenuItem key={n} value={n}>
                                {n}
                            </MenuItem>
                        ))}
                    </TextField>
                    {(networks.isError || capabilities.isError) && (
                        <Alert severity="error">
                            Network choices could not be loaded.
                        </Alert>
                    )}
                    <TextField
                        select
                        label="Model"
                        value={model}
                        onChange={(e) => setModel(e.target.value)}
                    >
                        {(
                            capabilities.data?.network_models[network] ||
                            capabilities.data?.nic_models || ["virtio"]
                        ).map((m) => (
                            <MenuItem key={m} value={m}>
                                {m}
                            </MenuItem>
                        ))}
                    </TextField>
                    <TextField
                        label="MAC address (optional)"
                        value={mac}
                        onChange={(e) => setMAC(e.target.value)}
                    />
                    <TextField
                        label="VLAN"
                        type="number"
                        value={vlan}
                        onChange={(e) => setVLAN(e.target.value)}
                        slotProps={{ htmlInput: { min: 0, max: 4094 } }}
                    />
                </>
            )}
        </OperationDialog>
    );
}
