import { useEffect, useRef, useState } from "react";
import {
    Accordion,
    AccordionDetails,
    AccordionSummary,
    Alert,
    Autocomplete,
    Box,
    Breadcrumbs,
    Button,
    Checkbox,
    Divider,
    FormControlLabel,
    Link,
    MenuItem,
    Stack,
    TextField,
    Typography,
} from "@mui/material";
import {
    Link as RouterLink,
    useNavigate,
    useParams,
    useSearchParams,
} from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import { APIError, request } from "../api/client";
import {
    esc,
    machineURL,
    useNodes,
    useResource,
    vmPath,
    type Capabilities,
    type CreateVM,
    type Image,
    type Key,
    type Network,
    type PatchVM,
    type Pool,
    type VM,
    type Volume,
} from "../api/hooks";
import {
    bytes,
    Confirm,
    ErrorMessage,
    PageTitle,
    QueryState,
    useDirty,
} from "../components/common";
import { fieldError } from "../components/OperationDialog";

export type DiskDraft = {
    key: string;
    kind: "image" | "new" | "existing";
    image: string;
    volume: string;
    name: string;
    pool: string;
    format: "" | "qcow2" | "raw";
    size: string;
    bus: string;
    device: "disk" | "cdrom";
    alias: string;
};
type NICDraft = {
    key: string;
    network: string;
    model: string;
    mac: string;
    vlan: string;
};
const newDisk = (): DiskDraft => ({
    key: crypto.randomUUID(),
    kind: "image",
    image: "",
    volume: "",
    name: "",
    pool: "",
    format: "",
    size: "",
    bus: "",
    device: "disk",
    alias: "",
});
const newNIC = (): NICDraft => ({
    key: crypto.randomUUID(),
    network: "",
    model: "",
    mac: "",
    vlan: "0",
});
const GiB = 2 ** 30;
const MiB = 2 ** 20;

function defaultDiskName(d: DiskDraft, index: number, id: string) {
    return d.kind === "image" && index === 0
        ? `${id || "machine"}-root`
        : `${id || "machine"}-disk${index + 1}`;
}

export function CreateMachine() {
    const [params] = useSearchParams();
    const nodes = useNodes();
    const navigate = useNavigate();
    const queries = useQueryClient();
    const form = useRef<HTMLFormElement>(null);
    const [node, setNode] = useState(params.get("node") || "");
    const [arch, setArch] = useState("");
    const [id, setID] = useState("");
    const [cpus, setCPUs] = useState("2");
    const [memory, setMemory] = useState("2048");
    const [disks, setDisks] = useState<DiskDraft[]>([newDisk()]);
    const [interfaces, setInterfaces] = useState<NICDraft[]>([newNIC()]);
    const [keys, setKeys] = useState<string[]>([]);
    const [userdata, setUserdata] = useState("");
    const [hostname, setHostname] = useState("");
    const [start, setStart] = useState(true);
    const [autostart, setAutostart] = useState(false);
    const [hugepages, setHugepages] = useState(false);
    const [graphics, setGraphics] = useState("vnc");
    const [video, setVideo] = useState("none");
    const [firmware, setFirmware] = useState("");
    const [controller, setController] = useState("");
    const [nicModel, setNICModel] = useState("");
    const [guestAgent, setGuestAgent] = useState("");
    const [initialization, setInitialization] = useState("");
    const [dirty, setDirty] = useState(false);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState<unknown>();
    const [invalid, setInvalid] = useState<string[]>([]);
    const [removeDraft, setRemoveDraft] = useState<{
        kind: "disk" | "interface";
        key: string;
        number: number;
    }>();
    const dirtyPrompt = useDirty(dirty);
    const suffix = arch ? `?arch=${esc(arch)}` : "";
    const images = useResource<Image[]>(
        `/api/v1/nodes/${esc(node)}/images${suffix}`,
        !!node,
        false,
    );
    const caps = useResource<Capabilities>(
        `/api/v1/nodes/${esc(node)}/capabilities${suffix}`,
        !!node,
        false,
    );
    const pools = useResource<Pool[]>(
        `/api/v1/nodes/${esc(node)}/pools`,
        !!node,
        false,
    );
    const networks = useResource<Network[]>(
        `/api/v1/nodes/${esc(node)}/networks`,
        !!node,
        false,
    );
    const volumes = useResource<Volume[]>(
        `/api/v1/nodes/${esc(node)}/volumes?usage=attachable`,
        !!node,
        false,
    );
    const keyQuery = useResource<Key[]>("/api/v1/keys", true, false);
    const networkChoices = [
        ...new Set([
            ...(networks.data || []).map((n) => n.name),
            ...Object.keys(caps.data?.network_models || {}),
        ]),
    ];
    const chosenImages = disks
        .filter((d) => d.kind === "image")
        .map((d) => images.data?.find((i) => i.id === d.image))
        .filter((i): i is Image => !!i);
    const minimumMemory = Math.max(
        0,
        ...chosenImages.map((i) => i.minimum_memory_bytes),
    );
    const uncertainty = error instanceof APIError && error.uncertain;
    const patchDisk = (key: string, patch: Partial<DiskDraft>) => {
        setDirty(true);
        setDisks((all) =>
            all.map((d) => (d.key === key ? { ...d, ...patch } : d)),
        );
    };
    const patchNIC = (key: string, patch: Partial<NICDraft>) => {
        setDirty(true);
        setInterfaces((all) =>
            all.map((n) => (n.key === key ? { ...n, ...patch } : n)),
        );
    };
    useEffect(() => {
        if (error instanceof APIError) {
            const field = error.problem.errors?.[0]?.location?.replace(
                /^body\.?/,
                "",
            );
            const input = field
                ? form.current?.elements.namedItem(field)
                : null;
            if (input instanceof HTMLElement) input.focus();
        }
    }, [error]);

    function validate() {
        const errors: string[] = [];
        if (!nodes.data?.some((n) => n.id === node))
            errors.push("Choose an available node.");
        if (
            [images, caps, pools, networks, volumes].some(
                (q) => !q.data || q.isError,
            )
        )
            errors.push(
                "Load the node’s creation choices successfully before creating a machine.",
            );
        if (Number(memory) * MiB < minimumMemory)
            errors.push(
                `Memory must meet the selected images’ minimum of ${bytes(minimumMemory)}.`,
            );
        if (caps.data && Number(cpus) > caps.data.max_vcpus)
            errors.push(
                `This node supports at most ${caps.data.max_vcpus} vCPUs.`,
            );
        disks.forEach((d, i) => {
            if (
                d.kind !== "existing" &&
                !pools.data?.some((p) => p.name === d.pool)
            )
                errors.push(`Disk ${i + 1}: choose a pool on ${node}.`);
            const image = images.data?.find((image) => image.id === d.image);
            if (d.kind === "image" && !image)
                errors.push(
                    `Disk ${i + 1}: the selected image is unavailable for this node/architecture.`,
                );
            if (
                d.kind === "image" &&
                image &&
                d.size &&
                Number(d.size) * GiB < image.minimum_disk_bytes
            )
                errors.push(
                    `Disk ${i + 1}: the image requires at least ${bytes(image.minimum_disk_bytes)}.`,
                );
            if (
                d.kind === "existing" &&
                !volumes.data?.some((v) => v.id === d.volume)
            )
                errors.push(
                    `Disk ${i + 1}: the volume is no longer attachable on this node.`,
                );
            if (d.bus && !caps.data?.disk_buses.includes(d.bus))
                errors.push(`Disk ${i + 1}: bus ${d.bus} is unavailable.`);
        });
        interfaces.forEach((n, i) => {
            if (!networkChoices.includes(n.network))
                errors.push(`Interface ${i + 1}: choose an available network.`);
            const models =
                caps.data?.network_models[n.network] || caps.data?.nic_models;
            if (n.model && !models?.includes(n.model))
                errors.push(
                    `Interface ${i + 1}: model ${n.model} is incompatible.`,
                );
        });
        setInvalid(errors);
        return errors.length === 0;
    }
    return (
        <Box sx={{ maxWidth: 1050 }}>
            {dirtyPrompt}
            {removeDraft && (
                <Confirm
                    title={`Remove ${removeDraft.kind} ${removeDraft.number} from this draft?`}
                    action="Remove from draft"
                    onClose={() => setRemoveDraft(undefined)}
                    run={async () => {
                        if (removeDraft.kind === "disk")
                            setDisks((all) =>
                                all.filter((d) => d.key !== removeDraft.key),
                            );
                        else
                            setInterfaces((all) =>
                                all.filter((n) => n.key !== removeDraft.key),
                            );
                        setDirty(true);
                    }}
                >
                    The settings for this {removeDraft.kind} will be removed
                    from the form. Existing resources are unchanged.
                </Confirm>
            )}
            <Breadcrumbs sx={{ mb: 2 }}>
                <Link
                    component={RouterLink}
                    to={`/machines/${node ? `?node=${esc(node)}` : ""}`}
                >
                    Machines
                </Link>
                <Typography>Create</Typography>
            </Breadcrumbs>
            <PageTitle title="Create machine" />
            <Box
                component="form"
                ref={form}
                onChange={() => setDirty(true)}
                onSubmit={async (e) => {
                    e.preventDefault();
                    if (busy || uncertainty) return;
                    if (!validate()) {
                        form.current
                            ?.querySelector<HTMLElement>(
                                '[aria-invalid="true"]',
                            )
                            ?.focus();
                        return;
                    }
                    setError(undefined);
                    setBusy(true);
                    const body: CreateVM = {
                        id,
                        arch: (arch || undefined) as CreateVM["arch"],
                        vcpus: Number(cpus),
                        memory_bytes: Math.round(Number(memory) * MiB),
                        start,
                        autostart,
                        hugepages,
                        graphics: graphics as CreateVM["graphics"],
                        video_model: video as CreateVM["video_model"],
                        defaults: {
                            firmware: (firmware || undefined) as
                                "auto" | "bios" | "efi" | undefined,
                            disk_controller: (controller || undefined) as
                                "virtio-scsi" | undefined,
                            nic_model: (nicModel || undefined) as
                                "virtio" | "e1000" | "rtl8139" | undefined,
                            guest_agent: guestAgent
                                ? guestAgent === "true"
                                : undefined,
                            initialization: (initialization || undefined) as
                                "none" | "nocloud" | "openstack" | undefined,
                        },
                        cloud_init: {
                            hostname: hostname || id,
                            user_data: userdata || undefined,
                            keys,
                        },
                        clone_disks: disks
                            .filter((d) => d.kind === "image")
                            .map((d) => ({
                                image_id: d.image,
                                name:
                                    d.name ||
                                    defaultDiskName(d, disks.indexOf(d), id),
                                pool: d.pool,
                                format: d.format || undefined,
                                size_bytes: d.size
                                    ? Math.round(Number(d.size) * GiB)
                                    : undefined,
                                alias: d.alias || undefined,
                                bus: (d.bus || undefined) as
                                    "virtio" | "scsi" | "ide" | undefined,
                            })),
                        new_disks: disks
                            .filter((d) => d.kind === "new")
                            .map((d, i) => ({
                                name:
                                    d.name ||
                                    defaultDiskName(d, disks.indexOf(d), id),
                                pool: d.pool,
                                format: d.format || undefined,
                                size_bytes: Math.round(Number(d.size) * GiB),
                                alias: d.alias || undefined,
                                bus: (d.bus || "virtio") as
                                    "virtio" | "scsi" | "ide",
                                device: d.device,
                            })),
                        disks: disks
                            .filter((d) => d.kind === "existing")
                            .map((d) => ({
                                volume_id: d.volume,
                                alias: d.alias || undefined,
                                bus: (d.bus || "virtio") as
                                    "virtio" | "scsi" | "ide",
                                device: d.device,
                            })),
                        interfaces: interfaces.map((n) => ({
                            network: n.network,
                            model: (n.model || undefined) as
                                "virtio" | "e1000" | "rtl8139" | undefined,
                            mac: n.mac || undefined,
                            vlan: Number(n.vlan),
                        })),
                    };
                    try {
                        const vm = await request<VM>(
                            `/api/v1/nodes/${esc(node)}/vms`,
                            "POST",
                            body,
                        );
                        setDirty(false);
                        await queries.invalidateQueries();
                        navigate(machineURL(vm.node, vm.id));
                    } catch (e) {
                        setError(e);
                    } finally {
                        setBusy(false);
                    }
                }}
            >
                <Stack spacing={4}>
                    <Section title="Identity and placement">
                        <Stack
                            direction={{ xs: "column", sm: "row" }}
                            spacing={2}
                        >
                            <TextField
                                label="Machine name"
                                name="id"
                                required
                                value={id}
                                onChange={(e) => setID(e.target.value)}
                                slotProps={{
                                    htmlInput: {
                                        pattern:
                                            "[A-Za-z0-9][A-Za-z0-9_.-]{0,127}",
                                    },
                                }}
                                {...fieldError(error, "id")}
                            />
                            <TextField
                                select
                                label="Node"
                                required
                                value={node}
                                onChange={(e) => {
                                    setNode(e.target.value);
                                    setDirty(true);
                                }}
                            >
                                <MenuItem value="">Choose a node</MenuItem>
                                {nodes.data?.map((n) => (
                                    <MenuItem key={n.id} value={n.id}>
                                        {n.id}
                                    </MenuItem>
                                ))}
                            </TextField>
                            <TextField
                                select
                                label="Architecture"
                                value={arch}
                                onChange={(e) => {
                                    setArch(e.target.value);
                                    setDirty(true);
                                }}
                                helperText="Changing placement preserves your edits."
                            >
                                <MenuItem value="">
                                    Inherit from image / node
                                </MenuItem>
                                <MenuItem value="x86_64">x86_64</MenuItem>
                                <MenuItem value="aarch64">aarch64</MenuItem>
                            </TextField>
                        </Stack>
                    </Section>
                    <Section title="Image and disks">
                        {!node && (
                            <Alert severity="info">
                                Choose a node to load images, storage and
                                network choices.
                            </Alert>
                        )}
                        {node &&
                            [images, caps, pools, volumes].some(
                                (q) => q.isError,
                            ) && (
                                <Alert severity="error">
                                    Some node choices could not be loaded.{" "}
                                    <Button
                                        onClick={() => {
                                            [
                                                images,
                                                caps,
                                                pools,
                                                volumes,
                                            ].forEach((q) => q.refetch());
                                        }}
                                    >
                                        Retry
                                    </Button>
                                </Alert>
                            )}
                        {disks.map((d, index) => (
                            <Box
                                key={d.key}
                                sx={{
                                    pt: index ? 3 : 0,
                                    mt: index ? 3 : 0,
                                    borderTop: index ? 1 : 0,
                                    borderColor: "divider",
                                }}
                            >
                                <Stack
                                    direction="row"
                                    sx={{
                                        alignItems: "center",
                                        justifyContent: "space-between",
                                        mb: 2,
                                    }}
                                >
                                    <Typography sx={{ fontWeight: 600 }}>
                                        Disk {index + 1}
                                    </Typography>
                                    <Button
                                        onClick={() =>
                                            setRemoveDraft({
                                                kind: "disk",
                                                key: d.key,
                                                number: index + 1,
                                            })
                                        }
                                    >
                                        Remove
                                    </Button>
                                </Stack>
                                <Stack spacing={2.5}>
                                    <TextField
                                        select
                                        label="Disk source"
                                        value={d.kind}
                                        onChange={(e) =>
                                            patchDisk(d.key, {
                                                kind: e.target
                                                    .value as DiskDraft["kind"],
                                            })
                                        }
                                    >
                                        <MenuItem value="image">
                                            Clone an image
                                        </MenuItem>
                                        <MenuItem value="new">
                                            New empty disk
                                        </MenuItem>
                                        <MenuItem value="existing">
                                            Existing volume
                                        </MenuItem>
                                    </TextField>
                                    {d.kind === "image" && (
                                        <TextField
                                            select
                                            label="Image"
                                            required
                                            value={d.image}
                                            onChange={(e) =>
                                                patchDisk(d.key, {
                                                    image: e.target.value,
                                                })
                                            }
                                            error={
                                                !!d.image &&
                                                !!images.data &&
                                                !images.data.some(
                                                    (i) => i.id === d.image,
                                                )
                                            }
                                            helperText={
                                                d.image &&
                                                !images.data?.some(
                                                    (i) => i.id === d.image,
                                                )
                                                    ? "This image is unavailable. Choose a compatible image; your other settings are preserved."
                                                    : undefined
                                            }
                                        >
                                            <MenuItem value="">
                                                Choose an image
                                            </MenuItem>
                                            {d.image &&
                                                !images.data?.some(
                                                    (i) => i.id === d.image,
                                                ) && (
                                                    <MenuItem
                                                        value={d.image}
                                                        disabled
                                                    >
                                                        {d.image} (unavailable)
                                                    </MenuItem>
                                                )}
                                            {images.data?.map((i) => (
                                                <MenuItem
                                                    key={i.id}
                                                    value={i.id}
                                                >
                                                    {i.name} · {i.os_name}{" "}
                                                    {i.os_version} · {i.arch}
                                                </MenuItem>
                                            ))}
                                        </TextField>
                                    )}
                                    {d.kind === "existing" ? (
                                        <TextField
                                            select
                                            label="Volume"
                                            required
                                            value={d.volume}
                                            onChange={(e) =>
                                                patchDisk(d.key, {
                                                    volume: e.target.value,
                                                })
                                            }
                                            error={
                                                !!d.volume &&
                                                !!volumes.data &&
                                                !volumes.data.some(
                                                    (v) => v.id === d.volume,
                                                )
                                            }
                                        >
                                            <MenuItem value="">
                                                Choose a volume
                                            </MenuItem>
                                            {d.volume &&
                                                !volumes.data?.some(
                                                    (v) => v.id === d.volume,
                                                ) && (
                                                    <MenuItem
                                                        value={d.volume}
                                                        disabled
                                                    >
                                                        Previously selected
                                                        volume (unavailable)
                                                    </MenuItem>
                                                )}
                                            {volumes.data?.map((v) => (
                                                <MenuItem
                                                    key={v.id}
                                                    value={v.id}
                                                >
                                                    {v.pool}/{v.name} ·{" "}
                                                    {bytes(v.size_bytes)}
                                                </MenuItem>
                                            ))}
                                        </TextField>
                                    ) : (
                                        <>
                                            <TextField
                                                label="Disk name"
                                                value={d.name}
                                                placeholder={defaultDiskName(
                                                    d,
                                                    index,
                                                    id,
                                                )}
                                                onChange={(e) =>
                                                    patchDisk(d.key, {
                                                        name: e.target.value,
                                                    })
                                                }
                                                helperText="Leave empty to use the displayed name."
                                            />
                                            <Stack
                                                direction={{
                                                    xs: "column",
                                                    sm: "row",
                                                }}
                                                spacing={2}
                                            >
                                                <TextField
                                                    select
                                                    label="Storage pool"
                                                    required
                                                    value={d.pool}
                                                    onChange={(e) =>
                                                        patchDisk(d.key, {
                                                            pool: e.target
                                                                .value,
                                                        })
                                                    }
                                                    error={
                                                        !!d.pool &&
                                                        !!pools.data &&
                                                        !pools.data.some(
                                                            (p) =>
                                                                p.name ===
                                                                d.pool,
                                                        )
                                                    }
                                                    helperText={
                                                        pools.data?.find(
                                                            (p) =>
                                                                p.name ===
                                                                d.pool,
                                                        )
                                                            ? `${bytes(pools.data.find((p) => p.name === d.pool)!.free_bytes)} free`
                                                            : "Choose a pool on the selected node."
                                                    }
                                                >
                                                    <MenuItem value="">
                                                        Choose a pool
                                                    </MenuItem>
                                                    {d.pool &&
                                                        !pools.data?.some(
                                                            (p) =>
                                                                p.name ===
                                                                d.pool,
                                                        ) && (
                                                            <MenuItem
                                                                value={d.pool}
                                                                disabled
                                                            >
                                                                {d.pool}{" "}
                                                                (unavailable)
                                                            </MenuItem>
                                                        )}
                                                    {pools.data?.map((p) => (
                                                        <MenuItem
                                                            key={p.name}
                                                            value={p.name}
                                                        >
                                                            {p.name}
                                                        </MenuItem>
                                                    ))}
                                                </TextField>
                                                <TextField
                                                    label="Size (GiB)"
                                                    type="number"
                                                    value={d.size}
                                                    required={d.kind === "new"}
                                                    placeholder={
                                                        d.kind === "image"
                                                            ? "Inherit source / image minimum"
                                                            : undefined
                                                    }
                                                    onChange={(e) =>
                                                        patchDisk(d.key, {
                                                            size: e.target
                                                                .value,
                                                        })
                                                    }
                                                    slotProps={{
                                                        htmlInput: {
                                                            min: 0.001,
                                                            step: "any",
                                                        },
                                                    }}
                                                />
                                                <TextField
                                                    select
                                                    label="Format"
                                                    value={d.format}
                                                    onChange={(e) =>
                                                        patchDisk(d.key, {
                                                            format: e.target
                                                                .value as DiskDraft["format"],
                                                        })
                                                    }
                                                >
                                                    <MenuItem value="">
                                                        Inherit source / backend
                                                        default
                                                    </MenuItem>
                                                    {["qcow2", "raw"].map(
                                                        (f) => (
                                                            <MenuItem
                                                                key={f}
                                                                value={f}
                                                            >
                                                                {f}
                                                            </MenuItem>
                                                        ),
                                                    )}
                                                </TextField>
                                            </Stack>
                                        </>
                                    )}
                                    {d.kind === "image" &&
                                        images.data?.find(
                                            (i) => i.id === d.image,
                                        ) && (
                                            <ImageDefaults
                                                image={images.data.find(
                                                    (i) => i.id === d.image,
                                                )!}
                                            />
                                        )}
                                    <Accordion>
                                        <AccordionSummary>
                                            Advanced disk settings
                                        </AccordionSummary>
                                        <AccordionDetails>
                                            <Stack spacing={2}>
                                                <TextField
                                                    label="Alias (optional)"
                                                    value={d.alias}
                                                    onChange={(e) =>
                                                        patchDisk(d.key, {
                                                            alias: e.target
                                                                .value,
                                                        })
                                                    }
                                                />
                                                <TextField
                                                    select
                                                    label="Bus"
                                                    value={d.bus}
                                                    onChange={(e) =>
                                                        patchDisk(d.key, {
                                                            bus: e.target.value,
                                                        })
                                                    }
                                                >
                                                    <MenuItem value="">
                                                        Inherit image / backend
                                                        default
                                                    </MenuItem>
                                                    {(
                                                        caps.data
                                                            ?.disk_buses || [
                                                            "virtio",
                                                            "scsi",
                                                            "ide",
                                                        ]
                                                    ).map((b) => (
                                                        <MenuItem
                                                            key={b}
                                                            value={b}
                                                        >
                                                            {b}
                                                        </MenuItem>
                                                    ))}
                                                </TextField>
                                                {d.kind !== "image" && (
                                                    <TextField
                                                        select
                                                        label="Device"
                                                        value={d.device}
                                                        onChange={(e) =>
                                                            patchDisk(d.key, {
                                                                device: e.target
                                                                    .value as
                                                                    | "disk"
                                                                    | "cdrom",
                                                            })
                                                        }
                                                    >
                                                        <MenuItem value="disk">
                                                            Disk
                                                        </MenuItem>
                                                        <MenuItem value="cdrom">
                                                            CD-ROM
                                                        </MenuItem>
                                                    </TextField>
                                                )}
                                            </Stack>
                                        </AccordionDetails>
                                    </Accordion>
                                </Stack>
                            </Box>
                        ))}
                        <Button
                            onClick={() => {
                                setDisks((all) => [...all, newDisk()]);
                                setDirty(true);
                            }}
                            sx={{ mt: 2 }}
                        >
                            Add disk
                        </Button>
                    </Section>
                    <Section title="CPU and memory">
                        <Stack
                            direction={{ xs: "column", sm: "row" }}
                            spacing={2}
                        >
                            <TextField
                                label="vCPUs"
                                name="vcpus"
                                type="number"
                                required
                                value={cpus}
                                onChange={(e) => setCPUs(e.target.value)}
                                slotProps={{
                                    htmlInput: {
                                        min: 1,
                                        max: caps.data?.max_vcpus || 65535,
                                    },
                                }}
                                {...fieldError(error, "vcpus")}
                            />
                            <TextField
                                label="Memory (MiB)"
                                name="memory_bytes"
                                type="number"
                                required
                                value={memory}
                                onChange={(e) => setMemory(e.target.value)}
                                slotProps={{
                                    htmlInput: {
                                        min: Math.max(1, minimumMemory / MiB),
                                        step: 1,
                                    },
                                }}
                                helperText={
                                    minimumMemory
                                        ? `Selected image minimum: ${bytes(minimumMemory)}`
                                        : "Explicit memory allocation."
                                }
                                error={Number(memory) * MiB < minimumMemory}
                            />
                        </Stack>
                    </Section>
                    <Section title="Networking">
                        {interfaces.map((n, index) => (
                            <Stack key={n.key} spacing={2} sx={{ mb: 3 }}>
                                <Stack
                                    direction="row"
                                    sx={{ justifyContent: "space-between" }}
                                >
                                    <Typography sx={{ fontWeight: 600 }}>
                                        Interface {index + 1}
                                    </Typography>
                                    <Button
                                        onClick={() =>
                                            setRemoveDraft({
                                                kind: "interface",
                                                key: n.key,
                                                number: index + 1,
                                            })
                                        }
                                    >
                                        Remove
                                    </Button>
                                </Stack>
                                <TextField
                                    select
                                    label="Network"
                                    required
                                    value={n.network}
                                    onChange={(e) =>
                                        patchNIC(n.key, {
                                            network: e.target.value,
                                        })
                                    }
                                    error={
                                        !!n.network &&
                                        !!networks.data &&
                                        !networkChoices.includes(n.network)
                                    }
                                    helperText={
                                        n.network &&
                                        !networkChoices.includes(n.network)
                                            ? "This network is unavailable on the selected node."
                                            : undefined
                                    }
                                >
                                    <MenuItem value="">
                                        Choose a network
                                    </MenuItem>
                                    {n.network &&
                                        !networkChoices.includes(n.network) && (
                                            <MenuItem
                                                value={n.network}
                                                disabled
                                            >
                                                {n.network} (unavailable)
                                            </MenuItem>
                                        )}
                                    {networkChoices.map((name) => (
                                        <MenuItem key={name} value={name}>
                                            {name}
                                        </MenuItem>
                                    ))}
                                </TextField>
                                <Accordion>
                                    <AccordionSummary>
                                        Advanced interface settings
                                    </AccordionSummary>
                                    <AccordionDetails>
                                        <Stack spacing={2}>
                                            <TextField
                                                select
                                                label="Model"
                                                value={n.model}
                                                onChange={(e) =>
                                                    patchNIC(n.key, {
                                                        model: e.target.value,
                                                    })
                                                }
                                            >
                                                <MenuItem value="">
                                                    Inherit image / backend
                                                    default
                                                </MenuItem>
                                                {(
                                                    caps.data?.network_models[
                                                        n.network
                                                    ] ||
                                                    caps.data?.nic_models || [
                                                        "virtio",
                                                    ]
                                                ).map((m) => (
                                                    <MenuItem value={m} key={m}>
                                                        {m}
                                                    </MenuItem>
                                                ))}
                                            </TextField>
                                            <TextField
                                                label="MAC address (optional)"
                                                value={n.mac}
                                                onChange={(e) =>
                                                    patchNIC(n.key, {
                                                        mac: e.target.value,
                                                    })
                                                }
                                            />
                                            <TextField
                                                label="VLAN"
                                                type="number"
                                                value={n.vlan}
                                                onChange={(e) =>
                                                    patchNIC(n.key, {
                                                        vlan: e.target.value,
                                                    })
                                                }
                                                slotProps={{
                                                    htmlInput: {
                                                        min: 0,
                                                        max: 4094,
                                                    },
                                                }}
                                            />
                                        </Stack>
                                    </AccordionDetails>
                                </Accordion>
                            </Stack>
                        ))}
                        <Button
                            onClick={() => {
                                setInterfaces((all) => [...all, newNIC()]);
                                setDirty(true);
                            }}
                        >
                            Add interface
                        </Button>
                    </Section>
                    <Section title="Access and cloud-init">
                        <Stack spacing={2.5}>
                            <TextField
                                label="Hostname (optional)"
                                value={hostname}
                                placeholder={id}
                                onChange={(e) => setHostname(e.target.value)}
                            />
                            <Autocomplete
                                multiple
                                options={keyQuery.data || []}
                                value={(keyQuery.data || []).filter((k) =>
                                    keys.includes(k.fingerprint),
                                )}
                                getOptionLabel={(k) =>
                                    `${k.comment || k.type} · ${k.fingerprint}`
                                }
                                isOptionEqualToValue={(a, b) =>
                                    a.fingerprint === b.fingerprint
                                }
                                onChange={(_, selected) => {
                                    setKeys(selected.map((k) => k.fingerprint));
                                    setDirty(true);
                                }}
                                renderInput={(props) => (
                                    <TextField
                                        {...props}
                                        label="SSH public keys"
                                        helperText={
                                            keyQuery.isError
                                                ? "Could not load SSH keys."
                                                : "Search by comment or fingerprint."
                                        }
                                        error={keyQuery.isError}
                                    />
                                )}
                            />
                            <TextField
                                label="Cloud-init user-data"
                                name="cloud_init.user_data"
                                multiline
                                minRows={9}
                                value={userdata}
                                onChange={(e) => setUserdata(e.target.value)}
                                helperText="Kept only in this page’s memory. Not saved as a browser draft."
                            />
                        </Stack>
                    </Section>
                    <Accordion>
                        <AccordionSummary>
                            Advanced machine settings
                        </AccordionSummary>
                        <AccordionDetails>
                            <Stack spacing={2.5}>
                                <TextField
                                    select
                                    label="Firmware override"
                                    value={firmware}
                                    onChange={(e) =>
                                        setFirmware(e.target.value)
                                    }
                                >
                                    <MenuItem value="">
                                        Inherit image / backend default
                                    </MenuItem>
                                    {(
                                        caps.data?.firmwares || [
                                            "auto",
                                            "bios",
                                            "efi",
                                        ]
                                    ).map((f) => (
                                        <MenuItem key={f} value={f}>
                                            {f}
                                        </MenuItem>
                                    ))}
                                </TextField>
                                <TextField
                                    select
                                    label="Disk controller override"
                                    value={controller}
                                    onChange={(e) =>
                                        setController(e.target.value)
                                    }
                                >
                                    <MenuItem value="">
                                        Inherit image / backend default
                                    </MenuItem>
                                    {(
                                        caps.data?.disk_controllers || [
                                            "virtio-scsi",
                                        ]
                                    ).map((c) => (
                                        <MenuItem key={c} value={c}>
                                            {c}
                                        </MenuItem>
                                    ))}
                                </TextField>
                                <TextField
                                    select
                                    label="NIC model override"
                                    value={nicModel}
                                    onChange={(e) =>
                                        setNICModel(e.target.value)
                                    }
                                >
                                    <MenuItem value="">
                                        Inherit image / backend default
                                    </MenuItem>
                                    {(caps.data?.nic_models || ["virtio"]).map(
                                        (m) => (
                                            <MenuItem key={m} value={m}>
                                                {m}
                                            </MenuItem>
                                        ),
                                    )}
                                </TextField>
                                <TextField
                                    select
                                    label="Guest agent override"
                                    value={guestAgent}
                                    onChange={(e) =>
                                        setGuestAgent(e.target.value)
                                    }
                                >
                                    <MenuItem value="">
                                        Inherit image / backend default
                                    </MenuItem>
                                    <MenuItem value="true">Enabled</MenuItem>
                                    <MenuItem value="false">Disabled</MenuItem>
                                </TextField>
                                <TextField
                                    select
                                    label="Initialization override"
                                    value={initialization}
                                    onChange={(e) =>
                                        setInitialization(e.target.value)
                                    }
                                >
                                    <MenuItem value="">
                                        Inherit image / node default
                                    </MenuItem>
                                    {["none", "nocloud", "openstack"].map(
                                        (v) => (
                                            <MenuItem key={v} value={v}>
                                                {v}
                                            </MenuItem>
                                        ),
                                    )}
                                </TextField>
                                <TextField
                                    select
                                    label="Graphics"
                                    value={graphics}
                                    onChange={(e) =>
                                        setGraphics(e.target.value)
                                    }
                                >
                                    {(
                                        caps.data?.graphics || [
                                            "none",
                                            "vnc",
                                            "spice",
                                        ]
                                    ).map((v) => (
                                        <MenuItem key={v} value={v}>
                                            {v}
                                        </MenuItem>
                                    ))}
                                </TextField>
                                <TextField
                                    select
                                    label="Video model"
                                    value={video}
                                    onChange={(e) => setVideo(e.target.value)}
                                >
                                    {(
                                        caps.data?.video_models || [
                                            "none",
                                            "cirrus",
                                            "qxl",
                                        ]
                                    ).map((v) => (
                                        <MenuItem key={v} value={v}>
                                            {v}
                                        </MenuItem>
                                    ))}
                                </TextField>
                                <FormControlLabel
                                    control={
                                        <Checkbox
                                            checked={autostart}
                                            onChange={(e) =>
                                                setAutostart(e.target.checked)
                                            }
                                        />
                                    }
                                    label="Start automatically with the host"
                                />
                                <FormControlLabel
                                    control={
                                        <Checkbox
                                            checked={hugepages}
                                            onChange={(e) =>
                                                setHugepages(e.target.checked)
                                            }
                                        />
                                    }
                                    label="Use hugepages"
                                />
                            </Stack>
                        </AccordionDetails>
                    </Accordion>
                    <Box>
                        <Divider sx={{ mb: 3 }} />
                        <Typography variant="h2" sx={{ mb: 1 }}>
                            Your selections
                        </Typography>
                        <Typography color="text.secondary">
                            {id || "Unnamed machine"} on{" "}
                            {node || "no node selected"} · {cpus} vCPU ·{" "}
                            {memory} MiB · {disks.length} disk(s) ·{" "}
                            {interfaces.length} interface(s)
                        </Typography>
                        <FormControlLabel
                            control={
                                <Checkbox
                                    checked={start}
                                    onChange={(e) => setStart(e.target.checked)}
                                />
                            }
                            label="Start after creation"
                        />
                        {invalid.length > 0 && (
                            <Alert severity="error" tabIndex={-1}>
                                {invalid.map((m) => (
                                    <div key={m}>{m}</div>
                                ))}
                            </Alert>
                        )}
                        <ErrorMessage error={error} />
                        {uncertainty && (
                            <Alert severity="warning">
                                Do not submit again until you have inspected the
                                machine and volumes.{" "}
                                <Link
                                    component={RouterLink}
                                    to={`/machines/?node=${esc(node)}`}
                                >
                                    Open machines
                                </Link>
                            </Alert>
                        )}
                        <Stack direction="row" spacing={2} sx={{ mt: 2 }}>
                            <Button
                                variant="contained"
                                type="submit"
                                disabled={busy || uncertainty || !node}
                            >
                                {busy ? "Creating…" : "Create machine"}
                            </Button>
                            <Button
                                component={RouterLink}
                                to={`/machines/${node ? `?node=${esc(node)}` : ""}`}
                                disabled={busy}
                            >
                                Cancel
                            </Button>
                        </Stack>
                    </Box>
                </Stack>
            </Box>
        </Box>
    );
}

function Section({
    title,
    children,
}: {
    title: string;
    children: React.ReactNode;
}) {
    return (
        <Box component="section">
            <Typography variant="h2" sx={{ mb: 2 }}>
                {title}
            </Typography>
            {children}
        </Box>
    );
}
function ImageDefaults({ image }: { image: Image }) {
    return (
        <Alert severity="info" icon={false}>
            <Typography variant="body2">
                Image minimums: {bytes(image.minimum_memory_bytes)} RAM ·{" "}
                {bytes(image.minimum_disk_bytes)} disk.
            </Typography>
            <Typography variant="body2">
                Provided defaults:{" "}
                {Object.entries(image.defaults)
                    .filter(([, v]) => v !== undefined)
                    .map(([k, v]) => `${k.replaceAll("_", " ")}: ${v}`)
                    .join(" · ") || "Backend defaults"}
                . Optional overrides remain inherited until changed.
            </Typography>
        </Alert>
    );
}

export function EditMachine() {
    const { node = "", id = "" } = useParams();
    const q = useResource<VM>(vmPath(node, id), true, false);
    return (
        <QueryState query={q}>
            {(vm) => <EditForm key={`${vm.node}/${vm.id}`} vm={vm} />}
        </QueryState>
    );
}
function EditForm({ vm }: { vm: VM }) {
    const [initial] = useState(vm);
    const form = useRef<HTMLFormElement>(null);
    const [values, set] = useState({
        vcpus: String(vm.vcpus),
        memory_bytes: String(vm.memory_bytes / MiB),
        graphics: vm.graphics,
        graphics_listen: vm.graphics_listen,
        video_model: vm.video_model,
        autostart: vm.autostart,
        guest_agent: vm.guest_agent,
        hugepages: vm.hugepages,
    });
    const [dirty, setDirty] = useState(false);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState<unknown>();
    const navigate = useNavigate();
    const queries = useQueryClient();
    const dirtyPrompt = useDirty(dirty);
    useEffect(() => {
        if (error instanceof APIError) {
            const field = error.problem.errors?.[0]?.location?.replace(
                /^body\.?/,
                "",
            );
            const input = field
                ? form.current?.elements.namedItem(field)
                : null;
            if (input instanceof HTMLElement) input.focus();
        }
    }, [error]);
    const uncertain = error instanceof APIError && error.uncertain;
    const update = (patch: Partial<typeof values>) => {
        set((old) => ({ ...old, ...patch }));
        setDirty(true);
    };
    return (
        <Box sx={{ maxWidth: 760 }}>
            {dirtyPrompt}
            <Breadcrumbs sx={{ mb: 2 }}>
                <Link component={RouterLink} to={machineURL(vm.node, vm.id)}>
                    {vm.id}
                </Link>
                <Typography>Edit</Typography>
            </Breadcrumbs>
            <PageTitle title={`Edit ${vm.id}`} />
            <Typography color="text.secondary" sx={{ mb: 3 }}>
                Persistent configuration on {vm.node}. Some settings take effect
                after stopping and starting the machine.
            </Typography>
            <Box
                component="form"
                ref={form}
                onSubmit={async (e) => {
                    e.preventDefault();
                    if (busy || uncertain) return;
                    setBusy(true);
                    setError(undefined);
                    const patch: PatchVM = {};
                    for (const key of [
                        "autostart",
                        "guest_agent",
                        "hugepages",
                        "graphics",
                        "graphics_listen",
                        "video_model",
                    ] as const) {
                        if (values[key] !== initial[key])
                            Object.assign(patch, { [key]: values[key] });
                    }
                    if (Number(values.vcpus) !== initial.vcpus)
                        patch.vcpus = Number(values.vcpus);
                    if (
                        Math.round(Number(values.memory_bytes) * MiB) !==
                        initial.memory_bytes
                    )
                        patch.memory_bytes = Math.round(
                            Number(values.memory_bytes) * MiB,
                        );
                    try {
                        await request(vmPath(vm.node, vm.id), "PATCH", patch);
                        setDirty(false);
                        await queries.invalidateQueries();
                        navigate(machineURL(vm.node, vm.id));
                    } catch (e) {
                        setError(e);
                    } finally {
                        setBusy(false);
                    }
                }}
            >
                <Stack spacing={3}>
                    <TextField
                        label="vCPUs"
                        name="vcpus"
                        type="number"
                        required
                        value={values.vcpus}
                        onChange={(e) => update({ vcpus: e.target.value })}
                        slotProps={{ htmlInput: { min: 1, max: 65535 } }}
                        {...fieldError(error, "vcpus")}
                    />
                    <TextField
                        label="Memory (MiB)"
                        name="memory_bytes"
                        type="number"
                        required
                        value={values.memory_bytes}
                        onChange={(e) =>
                            update({ memory_bytes: e.target.value })
                        }
                        slotProps={{ htmlInput: { min: 1, step: "any" } }}
                        {...fieldError(error, "memory_bytes")}
                    />
                    <TextField
                        select
                        label="Graphics"
                        value={values.graphics}
                        onChange={(e) => update({ graphics: e.target.value })}
                    >
                        {["none", "vnc", "spice"].map((v) => (
                            <MenuItem value={v} key={v}>
                                {v}
                            </MenuItem>
                        ))}
                    </TextField>
                    <TextField
                        label="Graphics listen address"
                        name="graphics_listen"
                        value={values.graphics_listen}
                        onChange={(e) =>
                            update({ graphics_listen: e.target.value })
                        }
                        {...fieldError(error, "graphics_listen")}
                    />
                    <TextField
                        select
                        label="Video model"
                        value={values.video_model}
                        onChange={(e) =>
                            update({ video_model: e.target.value })
                        }
                    >
                        {["none", "cirrus", "qxl"].map((v) => (
                            <MenuItem value={v} key={v}>
                                {v}
                            </MenuItem>
                        ))}
                    </TextField>
                    {(["autostart", "guest_agent", "hugepages"] as const).map(
                        (key) => (
                            <FormControlLabel
                                key={key}
                                control={
                                    <Checkbox
                                        checked={values[key]}
                                        onChange={(e) =>
                                            update({ [key]: e.target.checked })
                                        }
                                    />
                                }
                                label={
                                    key === "autostart"
                                        ? "Start automatically with the host"
                                        : key === "guest_agent"
                                          ? "Guest agent"
                                          : "Hugepages"
                                }
                            />
                        ),
                    )}
                    <ErrorMessage error={error} />
                    {uncertain && (
                        <Button
                            component={RouterLink}
                            to={machineURL(vm.node, vm.id)}
                        >
                            Refresh machine state before retrying
                        </Button>
                    )}
                    <Stack direction="row" spacing={2}>
                        <Button
                            type="submit"
                            variant="contained"
                            disabled={busy || uncertain}
                        >
                            {busy ? "Saving…" : "Save changes"}
                        </Button>
                        <Button
                            component={RouterLink}
                            to={machineURL(vm.node, vm.id)}
                            disabled={busy}
                        >
                            Cancel
                        </Button>
                    </Stack>
                </Stack>
            </Box>
        </Box>
    );
}
