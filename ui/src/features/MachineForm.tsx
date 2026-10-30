import { useEffect, useState } from "react";
import {
    Accordion,
    AccordionDetails,
    AccordionSummary,
    Alert,
    Box,
    Button,
    Checkbox,
    FormControlLabel,
    Link,
    MenuItem,
    Stack,
    TextField,
    Typography,
} from "@mui/material";
import { Link as RouterLink, useParams, useSearchParams } from "react-router";
import { APIError, request, type TaskReference } from "../api/client";
import {
    esc,
    useResource,
    useMachine,
    vmPath,
    type Key,
    type VM,
    type Volume,
} from "../api/hooks";
import type { components } from "../api/schema";
import {
    bytes,
    ErrorMessage,
    PageTitle,
    QueryState,
} from "../components/common";
import { fieldError, OperationDialog } from "../components/OperationDialog";
import { ResourceTasks } from "./Tasks";
import { useResourceBusy } from "../api/tasks";

export type Cloud = components["schemas"]["ApiCloud"];
export type HostCatalog = components["schemas"]["ApiHostChoices"];
const GiB = 2 ** 30;
type Entry = { id: string; name: string };
function Choice({
    label,
    value,
    entries,
    set,
}: {
    label: string;
    value: string;
    entries: Entry[];
    set: (value: string) => void;
}) {
    if (entries.length === 1)
        return (
            <Typography>
                {label}: {entries[0].name}
            </Typography>
        );
    return (
        <TextField
            select
            required
            label={label}
            value={entries.some((e) => e.id === value) ? value : ""}
            onChange={(e) => set(e.target.value)}
        >
            <MenuItem value="">Choose {label.toLowerCase()}</MenuItem>
            {entries.map((e) => (
                <MenuItem key={e.id} value={e.id}>
                    {e.name}
                </MenuItem>
            ))}
        </TextField>
    );
}
function validOrFirst(value: string, entries: Entry[]) {
    return entries.some((e) => e.id === value) ? value : entries[0]?.id || "";
}

export function CreateMachine() {
    const clouds = useResource<Cloud[]>("/api/v1/clouds");
    const [params] = useSearchParams();
    const [cloudID, setCloud] = useState(params.get("cloud") || "");
    const cloud =
        clouds.data?.find((c) => c.id === cloudID) || clouds.data?.[0];
    const keys = useResource<Key[]>("/api/v1/keys");
    const volumes = useResource<Volume[]>(
        `/api/v1/clouds/${esc(cloud?.id || "")}/volumes?usage=attachable`,
        !!cloud,
    );
    const [name, setName] = useState("");
    const [zone, setZone] = useState("");
    const [image, setImage] = useState("");
    const [instanceType, setType] = useState("");
    const [network, setNetwork] = useState("");
    const [storageClass, setStorage] = useState("");
    const [keyIDs, setKeys] = useState<string[]>([]);
    const [userData, setUserData] = useState("");
    const [start, setStart] = useState(true);
    const [required, setRequired] = useState<Record<string, string>>({});
    const [preferred, setPreferred] = useState<Record<string, string>>({});
    const [disks, setDisks] = useState<
        { name: string; storage_class: string; size: string }[]
    >([]);
    const [existing, setExisting] = useState<string[]>([]);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState<unknown>();
    const [accepted, setAccepted] = useState<TaskReference>();
    const zoneOffers = cloud?.offers.filter((o) => o.zone === zone) || [];
    const imageChoices =
        cloud?.images.filter((i) => zoneOffers.some((o) => o.image === i.id)) ||
        [];
    const typeChoices =
        cloud?.instance_types.filter((t) =>
            zoneOffers.some(
                (o) => o.image === image && o.instance_type === t.id,
            ),
        ) || [];
    const offers = zoneOffers.filter(
        (o) => o.image === image && o.instance_type === instanceType,
    );
    const networks =
        cloud?.networks.filter((n) =>
            offers.some((o) => (o.networks || []).includes(n.id)),
        ) || [];
    const storageClasses =
        cloud?.storage_classes.filter((s) =>
            offers.some(
                (o) =>
                    (o.networks || []).includes(network) &&
                    (o.storage_classes || []).includes(s.id),
            ),
        ) || [];
    useEffect(() => {
        setDisks((old) =>
            old.map((d) => ({
                ...d,
                storage_class: storageClasses.some(
                    (c) => c.id === d.storage_class,
                )
                    ? d.storage_class
                    : storageClasses[0]?.id || "",
            })),
        );
    }, [JSON.stringify(storageClasses)]);
    useEffect(() => {
        setExisting([]);
    }, [zone]);
    const selectedType = typeChoices.find((t) => t.id === instanceType);
    const owningHost = volumes.data?.find((v) =>
        existing.includes(`${v.node}/${v.id}`),
    )?.node;
    const attachable =
        volumes.data?.filter(
            (v) =>
                v.cloud === cloud?.id &&
                v.zone === zone &&
                !v.attached_to &&
                (!owningHost || v.node === owningHost),
        ) || [];

    useEffect(() => {
        if (cloud) {
            setZone((v) => validOrFirst(v, cloud.zones));
            setRequired({});
            setPreferred({});
            setExisting([]);
            setDisks([]);
        }
    }, [cloud?.id]);
    useEffect(() => {
        setImage((v) => validOrFirst(v, imageChoices));
    }, [JSON.stringify(imageChoices.map((i) => i.id))]);
    useEffect(() => {
        setType((v) => validOrFirst(v, typeChoices));
    }, [JSON.stringify(typeChoices.map((i) => i.id))]);
    useEffect(() => {
        setNetwork((v) => validOrFirst(v, networks));
    }, [JSON.stringify(networks)]);
    useEffect(() => {
        setStorage((v) => validOrFirst(v, storageClasses));
    }, [JSON.stringify(storageClasses)]);
    const uncertainty = error instanceof APIError && error.uncertain;
    const selectors = (values: Record<string, string>) =>
        Object.fromEntries(
            Object.entries(values).filter(([, v]) => v.trim() !== ""),
        );
    return (
        <Box sx={{ maxWidth: 860 }}>
            <PageTitle title="Create machine" />
            <QueryState query={clouds}>
                {() =>
                    !cloud ? (
                        <Alert severity="info">
                            Configure a cloud catalog before creating machines.
                        </Alert>
                    ) : (
                        <Box
                            component="form"
                            onSubmit={async (e) => {
                                e.preventDefault();
                                if (busy || accepted) return;
                                if (
                                    uncertainty &&
                                    !(
                                        error instanceof APIError &&
                                        error.recover
                                    )
                                )
                                    return;
                                setBusy(true);
                                const recover =
                                    error instanceof APIError
                                        ? error.recover
                                        : undefined;
                                setError(undefined);
                                try {
                                    const result = recover
                                        ? ((await recover()) as TaskReference)
                                        : await request<TaskReference>(
                                              `/api/v1/clouds/${esc(cloud.id)}/machines`,
                                              "POST",
                                              {
                                                  name,
                                                  zone,
                                                  image,
                                                  instance_type: instanceType,
                                                  network,
                                                  storage_class: storageClass,
                                                  start,
                                                  keys: keyIDs,
                                                  user_data: userData,
                                                  placement: {
                                                      required:
                                                          selectors(required),
                                                      preferred:
                                                          selectors(preferred),
                                                  },
                                                  new_disks: disks.map((d) => ({
                                                      name: d.name,
                                                      storage_class:
                                                          d.storage_class,
                                                      size_bytes: Math.round(
                                                          Number(d.size) * GiB,
                                                      ),
                                                  })),
                                                  existing_disks: existing.map(
                                                      (key) => {
                                                          const v =
                                                              attachable.find(
                                                                  (v) =>
                                                                      `${v.node}/${v.id}` ===
                                                                      key,
                                                              )!;
                                                          return {
                                                              host: v.node,
                                                              volume_id: v.id,
                                                          };
                                                      },
                                                  ),
                                              },
                                          );
                                    setAccepted(result);
                                } catch (err) {
                                    setError(err);
                                } finally {
                                    setBusy(false);
                                }
                            }}
                        >
                            <Stack spacing={3}>
                                <Box
                                    component="fieldset"
                                    disabled={busy || !!accepted || uncertainty}
                                    sx={{
                                        border: 0,
                                        p: 0,
                                        m: 0,
                                        display: "flex",
                                        flexDirection: "column",
                                        gap: 3,
                                    }}
                                >
                                    <Choice
                                        label="Cloud"
                                        value={cloud.id}
                                        entries={clouds.data || []}
                                        set={setCloud}
                                    />
                                    <TextField
                                        label="Machine name"
                                        name="id"
                                        required
                                        value={name}
                                        onChange={(e) =>
                                            setName(e.target.value)
                                        }
                                        slotProps={{
                                            htmlInput: {
                                                pattern:
                                                    "[A-Za-z0-9][A-Za-z0-9_.-]{0,127}",
                                            },
                                        }}
                                        {...fieldError(error, "id")}
                                    />
                                    <Choice
                                        label="Availability zone"
                                        value={zone}
                                        entries={cloud.zones}
                                        set={setZone}
                                    />
                                    <Choice
                                        label="Image"
                                        value={image}
                                        entries={imageChoices}
                                        set={setImage}
                                    />
                                    <Choice
                                        label="Instance type"
                                        value={instanceType}
                                        entries={typeChoices}
                                        set={setType}
                                    />
                                    <Choice
                                        label="Network"
                                        value={network}
                                        entries={networks}
                                        set={setNetwork}
                                    />
                                    <Choice
                                        label="Storage class"
                                        value={storageClass}
                                        entries={storageClasses}
                                        set={setStorage}
                                    />
                                    {selectedType ? (
                                        <Alert severity="info">
                                            {selectedType.vcpus} vCPU ·{" "}
                                            {bytes(selectedType.memory_bytes)}{" "}
                                            RAM ·{" "}
                                            {bytes(
                                                selectedType.boot_disk_bytes,
                                            )}{" "}
                                            boot disk. Placement is automatic
                                            within the selected zone.
                                        </Alert>
                                    ) : (
                                        <Alert severity="warning">
                                            No compatible image and instance
                                            type are available in this zone.
                                        </Alert>
                                    )}
                                    <TextField
                                        select
                                        label="SSH keys"
                                        value={keyIDs}
                                        slotProps={{
                                            select: { multiple: true },
                                        }}
                                        onChange={(e) =>
                                            setKeys(
                                                typeof e.target.value ===
                                                    "string"
                                                    ? e.target.value.split(",")
                                                    : e.target.value,
                                            )
                                        }
                                    >
                                        {keys.data?.map((k) => (
                                            <MenuItem
                                                key={k.fingerprint}
                                                value={k.fingerprint}
                                            >
                                                {k.comment || k.fingerprint}
                                            </MenuItem>
                                        ))}
                                    </TextField>
                                    <ErrorMessage error={keys.error} />
                                    <TextField
                                        label="Cloud-init user-data"
                                        multiline
                                        minRows={4}
                                        value={userData}
                                        onChange={(e) =>
                                            setUserData(e.target.value)
                                        }
                                    />
                                    <Accordion>
                                        <AccordionSummary>
                                            Additional disks
                                        </AccordionSummary>
                                        <AccordionDetails>
                                            <Stack spacing={2}>
                                                {disks.map((disk, index) => (
                                                    <Stack
                                                        key={index}
                                                        spacing={2}
                                                    >
                                                        <TextField
                                                            required
                                                            label={`Disk ${index + 1} name`}
                                                            value={disk.name}
                                                            onChange={(e) =>
                                                                setDisks(
                                                                    (old) =>
                                                                        old.map(
                                                                            (
                                                                                d,
                                                                                i,
                                                                            ) =>
                                                                                i ===
                                                                                index
                                                                                    ? {
                                                                                          ...d,
                                                                                          name: e
                                                                                              .target
                                                                                              .value,
                                                                                      }
                                                                                    : d,
                                                                        ),
                                                                )
                                                            }
                                                        />
                                                        <TextField
                                                            required
                                                            label={`Disk ${index + 1} size (GiB)`}
                                                            type="number"
                                                            slotProps={{
                                                                htmlInput: {
                                                                    min: 0.001,
                                                                    step: "any",
                                                                },
                                                            }}
                                                            value={disk.size}
                                                            onChange={(e) =>
                                                                setDisks(
                                                                    (old) =>
                                                                        old.map(
                                                                            (
                                                                                d,
                                                                                i,
                                                                            ) =>
                                                                                i ===
                                                                                index
                                                                                    ? {
                                                                                          ...d,
                                                                                          size: e
                                                                                              .target
                                                                                              .value,
                                                                                      }
                                                                                    : d,
                                                                        ),
                                                                )
                                                            }
                                                        />
                                                        <Choice
                                                            label={`Disk ${index + 1} storage class`}
                                                            value={
                                                                disk.storage_class
                                                            }
                                                            entries={
                                                                storageClasses
                                                            }
                                                            set={(value) =>
                                                                setDisks(
                                                                    (old) =>
                                                                        old.map(
                                                                            (
                                                                                d,
                                                                                i,
                                                                            ) =>
                                                                                i ===
                                                                                index
                                                                                    ? {
                                                                                          ...d,
                                                                                          storage_class:
                                                                                              value,
                                                                                      }
                                                                                    : d,
                                                                        ),
                                                                )
                                                            }
                                                        />
                                                        <Button
                                                            onClick={() =>
                                                                setDisks(
                                                                    (old) =>
                                                                        old.filter(
                                                                            (
                                                                                _,
                                                                                i,
                                                                            ) =>
                                                                                i !==
                                                                                index,
                                                                        ),
                                                                )
                                                            }
                                                        >
                                                            Remove disk{" "}
                                                            {index + 1}
                                                        </Button>
                                                    </Stack>
                                                ))}
                                                <Button
                                                    onClick={() =>
                                                        setDisks((old) => [
                                                            ...old,
                                                            {
                                                                name: `data${old.length + 1}`,
                                                                storage_class:
                                                                    storageClass,
                                                                size: "10",
                                                            },
                                                        ])
                                                    }
                                                >
                                                    Add disk
                                                </Button>
                                                <TextField
                                                    select
                                                    label="Existing local disks"
                                                    value={existing}
                                                    slotProps={{
                                                        select: {
                                                            multiple: true,
                                                        },
                                                    }}
                                                    onChange={(e) =>
                                                        setExisting(
                                                            typeof e.target
                                                                .value ===
                                                                "string"
                                                                ? e.target.value.split(
                                                                      ",",
                                                                  )
                                                                : e.target
                                                                      .value,
                                                        )
                                                    }
                                                >
                                                    {attachable.map((v) => (
                                                        <MenuItem
                                                            key={`${v.node}/${v.id}`}
                                                            value={`${v.node}/${v.id}`}
                                                        >
                                                            {v.name} ·{" "}
                                                            {bytes(
                                                                v.size_bytes,
                                                            )}
                                                        </MenuItem>
                                                    ))}
                                                </TextField>
                                                <Typography color="text.secondary">
                                                    Existing disks constrain
                                                    placement to their owning
                                                    host. Disks from different
                                                    hosts cannot be combined.
                                                </Typography>
                                            </Stack>
                                        </AccordionDetails>
                                    </Accordion>
                                    {cloud.placement.attributes.length > 0 && (
                                        <Accordion>
                                            <AccordionSummary>
                                                Placement (optional)
                                            </AccordionSummary>
                                            <AccordionDetails>
                                                <Typography sx={{ mb: 2 }}>
                                                    Required labels restrict
                                                    placement. Preferred labels
                                                    guide scoring; they do not
                                                    guarantee a match.
                                                </Typography>
                                                <Stack spacing={2}>
                                                    {cloud.placement.attributes.map(
                                                        (attribute) => (
                                                            <Stack
                                                                key={attribute}
                                                                direction="row"
                                                                spacing={2}
                                                            >
                                                                <TextField
                                                                    label={`Required: ${attribute}`}
                                                                    value={
                                                                        required[
                                                                            attribute
                                                                        ] || ""
                                                                    }
                                                                    onChange={(
                                                                        e,
                                                                    ) =>
                                                                        setRequired(
                                                                            (
                                                                                v,
                                                                            ) => ({
                                                                                ...v,
                                                                                [attribute]:
                                                                                    e
                                                                                        .target
                                                                                        .value,
                                                                            }),
                                                                        )
                                                                    }
                                                                />
                                                                <TextField
                                                                    label={`Preferred: ${attribute}`}
                                                                    value={
                                                                        preferred[
                                                                            attribute
                                                                        ] || ""
                                                                    }
                                                                    onChange={(
                                                                        e,
                                                                    ) =>
                                                                        setPreferred(
                                                                            (
                                                                                v,
                                                                            ) => ({
                                                                                ...v,
                                                                                [attribute]:
                                                                                    e
                                                                                        .target
                                                                                        .value,
                                                                            }),
                                                                        )
                                                                    }
                                                                />
                                                            </Stack>
                                                        ),
                                                    )}
                                                </Stack>
                                            </AccordionDetails>
                                        </Accordion>
                                    )}
                                    <FormControlLabel
                                        control={
                                            <Checkbox
                                                checked={start}
                                                onChange={(e) =>
                                                    setStart(e.target.checked)
                                                }
                                            />
                                        }
                                        label="Start after creation"
                                    />
                                </Box>
                                <ErrorMessage error={error} />
                                {accepted ? (
                                    <Alert severity="info">
                                        Machine creation accepted.{" "}
                                        <Link
                                            component={RouterLink}
                                            to={`/tasks/${accepted.task.id}/`}
                                        >
                                            View progress and results
                                        </Link>
                                    </Alert>
                                ) : (
                                    <Button
                                        type="submit"
                                        variant="contained"
                                        disabled={
                                            busy ||
                                            !selectedType ||
                                            !storageClass ||
                                            !network ||
                                            (uncertainty &&
                                                !(
                                                    error instanceof APIError &&
                                                    error.recover
                                                ))
                                        }
                                    >
                                        {busy
                                            ? "Submitting…"
                                            : error instanceof APIError &&
                                                error.recover
                                              ? "Recover submission"
                                              : "Create machine"}
                                    </Button>
                                )}
                                {accepted && (
                                    <ResourceTasks
                                        resource={{
                                            kind: "machine-request",
                                            scope: cloud.id,
                                            id: name,
                                        }}
                                    />
                                )}
                            </Stack>
                        </Box>
                    )
                }
            </QueryState>
        </Box>
    );
}

export function EditMachine() {
    const { cloud = "", id = "" } = useParams();
    const machine = useMachine(cloud, id);
    return (
        <QueryState query={machine}>
            {(vm) => <EditForm key={`${cloud}/${id}`} vm={vm} />}
        </QueryState>
    );
}
function EditForm({ vm }: { vm: VM }) {
    const clouds = useResource<Cloud[]>("/api/v1/clouds");
    const blocked = useResourceBusy({
        kind: "machine",
        scope: vm.node,
        id: vm.id,
    });
    const [type, setType] = useState(vm.instance_type || "");
    const [open, setOpen] = useState(false);
    const catalog = clouds.data?.find((c) => c.id === vm.cloud);
    const instanceTypes = catalog?.instance_types || [];
    const selectedType = validOrFirst(type, instanceTypes);
    const cannotChange =
        !vm.managed ||
        vm.state !== "stopped" ||
        !selectedType ||
        !!blocked.task ||
        blocked.unavailable;
    useEffect(() => {
        if (clouds.data) setType(selectedType);
    }, [clouds.data, selectedType]);
    return (
        <Stack spacing={3} sx={{ maxWidth: 760 }}>
            <PageTitle title={`Change instance type for ${vm.name}`} />
            <Typography>
                Host: {vm.node} · Cloud: {vm.cloud} · AZ: {vm.zone}
            </Typography>
            <ResourceTasks
                resource={{ kind: "machine", scope: vm.node, id: vm.id }}
            />
            {!vm.managed ? (
                <Alert severity="info">
                    Adopt this machine before changing its configuration.
                </Alert>
            ) : (
                <>
                    <Typography>
                        The machine must be stopped. The change stays on this
                        host, applies its autostart policy and preserves guest devices
                        and all disk sizes. Insufficient host
                        resources cause an error.
                    </Typography>
                    <ErrorMessage error={clouds.error} />
                    <Choice
                        label="Instance type"
                        value={selectedType}
                        entries={instanceTypes}
                        set={setType}
                    />
                    <Button
                        variant="contained"
                        disabled={cannotChange}
                        onClick={() => setOpen(true)}
                    >
                        Change instance type
                    </Button>
                </>
            )}
            {open && (
                <OperationDialog
                    disabled={cannotChange}
                    title="Change instance type?"
                    action="Change instance type"
                    onClose={() => setOpen(false)}
                    run={() =>
                        request(
                            `/api/v1/clouds/${esc(vm.cloud)}/machines/${esc(vm.id)}/instance-type`,
                            "POST",
                            { instance_type: selectedType },
                        )
                    }
                >
                    {() => (
                        <Typography>
                            Apply {selectedType} to {vm.name} on {vm.node}. Disk
                            sizes remain unchanged.
                        </Typography>
                    )}
                </OperationDialog>
            )}
        </Stack>
    );
}
