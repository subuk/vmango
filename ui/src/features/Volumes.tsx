import { useEffect, useState } from "react";
import {
    Alert,
    Button,
    Menu,
    MenuItem,
    TextField,
    Typography,
} from "@mui/material";
import { useQueryClient } from "@tanstack/react-query";
import { useSearchParams } from "react-router";
import {
    esc,
    useResource,
    useVolumes,
    volumePath,
    type Volume,
    type VolumeDetail,
} from "../api/hooks";
import { useResourceBusy, volumeResource } from "../api/tasks";
import { ResourceTasks } from "./Tasks";
import { request } from "../api/client";
import {
    bytes,
    Confirm,
    ErrorMessage,
    PageTitle,
    QueryState,
} from "../components/common";
import { Summary } from "../components/Information";
import { ResourceTable } from "../components/ResourceTable";
import type { Cloud } from "./MachineForm";
import { fieldError, OperationDialog } from "../components/OperationDialog";

export function Volumes() {
    const [params, setParams] = useSearchParams();
    const clouds = useResource<Cloud[]>("/api/v1/clouds");
    const cloudID = params.get("cloud") || clouds.data?.[0]?.id || "";
    const q = useVolumes(cloudID);
    const [create, setCreate] = useState(false);
    return (
        <>
            <PageTitle
                title="Volumes"
                description="Review virtual disks, storage classes and machine attachments."
                count={q.data?.items.length}
                action={
                    <Button variant="contained" onClick={() => setCreate(true)}>
                        Create volume
                    </Button>
                }
            />
            {clouds.data && clouds.data.length > 1 && (
                <TextField
                    select
                    label="Cloud"
                    value={cloudID}
                    sx={{ mb: 3, minWidth: 220 }}
                    onChange={(e) => setParams({ cloud: e.target.value })}
                >
                    {clouds.data.map((c) => (
                        <MenuItem key={c.id} value={c.id}>
                            {c.name}
                        </MenuItem>
                    ))}
                </TextField>
            )}
            <ErrorMessage error={clouds.error} />
            {clouds.isSuccess && clouds.data.length === 0 && (
                <Alert severity="info">No clouds are configured.</Alert>
            )}
            {!!q.data?.unavailable_hosts.length && (
                <Alert severity="warning">
                    The list is incomplete. Unavailable hosts:{" "}
                    {q.data.unavailable_hosts.join(", ")}.
                </Alert>
            )}
            {q.data && (
                <Summary
                    label="Volume inventory summary"
                    items={[
                        {
                            label: "Provisioned size",
                            value: bytes(
                                q.data.items.reduce(
                                    (sum, v) => sum + v.size_bytes,
                                    0,
                                ),
                            ),
                            detail: "Logical volume capacity",
                        },
                        {
                            label: "Attached",
                            value: q.data.items.filter((v) => !!v.attached_to)
                                .length,
                            detail: "Connected to machines",
                        },
                        {
                            label: "Unattached",
                            value: q.data.items.filter((v) => !v.attached_to)
                                .length,
                            detail: "No machine attachment",
                        },
                        {
                            label: "Protected",
                            value: q.data.items.filter((v) => v.protected)
                                .length,
                            detail: "Protected from deletion",
                        },
                    ]}
                />
            )}
            {cloudID && (
                <QueryState query={q}>
                    {(result) => (
                        <ResourceTable
                            rows={result.items}
                            rowKey={(v) => `${v.cloud}/${v.id}`}
                            search={(v) =>
                                `${v.name} ${v.node} ${v.pool} ${v.attached_to || ""}`
                            }
                            pool={(v) => v.storage_class || "Unmanaged"}
                            poolLabel="Storage class"
                            columns={[
                                {
                                    id: "name",
                                    label: "Name",
                                    value: (v) => v.name,
                                    render: (v) => (
                                        <>
                                            {v.name}
                                            <ResourceTasks
                                                compact
                                                resource={volumeResource(
                                                    v.node,
                                                    v.id,
                                                )}
                                            />
                                        </>
                                    ),
                                },
                                {
                                    id: "pool",
                                    label: "Storage class",
                                    value: (v) =>
                                        v.storage_class || "Unmanaged",
                                },
                                {
                                    id: "format",
                                    label: "Format",
                                    value: (v) => v.format,
                                },
                                {
                                    id: "size",
                                    label: "Size",
                                    value: (v) => v.size_bytes,
                                    render: (v) => bytes(v.size_bytes),
                                    numeric: true,
                                },
                                {
                                    id: "attachment",
                                    label: "Attached to",
                                    value: (v) => v.attached_to || "—",
                                },
                                {
                                    id: "protected",
                                    label: "Protection",
                                    value: (v) =>
                                        v.protected ? "Protected" : "—",
                                },
                            ]}
                            actions={(v) => <VolumeActions volume={v} />}
                            empty="No visible volumes are available."
                        />
                    )}
                </QueryState>
            )}
            {create && (
                <VolumeDialog
                    cloudContext={cloudID}
                    mode="create"
                    onClose={() => setCreate(false)}
                />
            )}
        </>
    );
}

function VolumeActions({ volume: v }: { volume: Volume }) {
    const blocked = useResourceBusy(volumeResource(v.node, v.id));
    const conflict = !!blocked.task || blocked.unavailable;
    const [anchor, setAnchor] = useState<HTMLElement | null>(null);
    const [mode, setMode] = useState<string>();
    const queries = useQueryClient();
    return (
        <>
            <Button
                disabled={conflict}
                aria-label={`Actions for ${v.name}`}
                aria-haspopup="menu"
                onClick={(e) => setAnchor(e.currentTarget)}
            >
                Actions
            </Button>
            <Menu
                anchorEl={anchor}
                open={!!anchor}
                onClose={() => setAnchor(null)}
            >
                {["clone", "resize", "delete"].map((action) => (
                    <MenuItem
                        key={action}
                        disabled={
                            conflict ||
                            (action !== "clone" && v.owned === false) ||
                            (action === "delete" &&
                                (v.protected || !!v.attached_to))
                        }
                        onClick={() => {
                            setMode(action);
                            setAnchor(null);
                        }}
                        sx={{
                            color:
                                action === "delete" ? "error.main" : undefined,
                        }}
                    >
                        {action === "clone"
                            ? "Clone"
                            : action === "resize"
                              ? "Resize"
                              : "Delete"}
                        …
                    </MenuItem>
                ))}
            </Menu>
            {(v.protected || v.attached_to) && (
                <Typography
                    variant="caption"
                    component="div"
                    color="text.secondary"
                >
                    {v.protected
                        ? "Protected from deletion"
                        : "Detach before deletion"}
                </Typography>
            )}
            {mode === "delete" && (
                <Confirm
                    title={`Delete ${v.name}?`}
                    action="Delete volume"
                    onClose={() => setMode(undefined)}
                    run={async () => {
                        await request(volumePath(v.cloud, v.id), "DELETE");
                        await queries.invalidateQueries({
                            predicate: (q) =>
                                q.queryKey[0] !== "administration",
                        });
                    }}
                >
                    Permanently delete {v.node}/{v.pool}/{v.name} (
                    {bytes(v.size_bytes)}). This cannot be undone.
                </Confirm>
            )}
            {(mode === "clone" || mode === "resize") && (
                <VolumeDialog
                    mode={mode}
                    volume={v}
                    onClose={() => setMode(undefined)}
                />
            )}
        </>
    );
}

export function VolumeDialog({
    mode,
    volume,
    cloudContext = "",
    onClose,
}: {
    mode: "create" | "clone" | "resize";
    volume?: Volume;
    cloudContext?: string;
    onClose: () => void;
}) {
    const clouds = useResource<Cloud[]>("/api/v1/clouds", !volume);
    const detail = useResource<VolumeDetail>(
        volumePath(volume?.cloud || "", volume?.id || ""),
        !!volume,
        false,
    );
    const [cloudID, setCloud] = useState(volume?.cloud || cloudContext);
    const cloud =
        clouds.data?.find((c) => c.id === cloudID) || clouds.data?.[0];
    const [zone, setZone] = useState(volume?.zone || "");
    const [storage, setStorage] = useState(volume?.storage_class || "");
    const [name, setName] = useState(
        mode === "clone" ? `${volume!.name}-copy` : "",
    );
    const [size, setSize] = useState(
        volume ? String(volume.size_bytes / 2 ** 30) : "10",
    );
    const classes = volume
        ? detail.data?.catalog?.storage_classes || []
        : cloud?.storage_classes.filter((s) =>
              (cloud.storage_availability || []).some(
                  (o) =>
                      o.zone === zone &&
                      (o.storage_classes || []).includes(s.id),
              ),
          ) || [];
    useEffect(() => {
        if (cloud)
            setZone((old) =>
                cloud.zones.some((z) => z.id === old)
                    ? old
                    : cloud.zones[0]?.id || "",
            );
    }, [cloud?.id]);
    useEffect(() => {
        setStorage((old) =>
            classes.some((s) => s.id === old) ? old : classes[0]?.id || "",
        );
    }, [JSON.stringify(classes)]);
    return (
        <OperationDialog
            title={
                mode === "resize"
                    ? `Resize ${volume!.name}`
                    : mode === "clone"
                      ? `Clone ${volume!.name}`
                      : "Create volume"
            }
            action={
                mode === "resize"
                    ? "Grow volume"
                    : mode === "clone"
                      ? "Clone volume"
                      : "Create volume"
            }
            onClose={onClose}
            disabled={
                mode === "create"
                    ? !cloud || !storage || !zone
                    : mode === "clone"
                      ? !storage || !detail.data?.catalog
                      : false
            }
            run={() =>
                mode === "resize"
                    ? request(
                          `${volumePath(volume!.cloud, volume!.id)}/resize`,
                          "POST",
                          { size_bytes: Math.round(Number(size) * 2 ** 30) },
                      )
                    : request(
                          mode === "clone"
                              ? `${volumePath(volume!.cloud, volume!.id)}/clone`
                              : `/api/v1/clouds/${esc(cloud!.id)}/volumes`,
                          "POST",
                          {
                              name,
                              zone: mode === "create" ? zone : undefined,
                              storage_class: storage,
                              size_bytes: Math.round(Number(size) * 2 ** 30),
                          },
                      )
            }
        >
            {(error) => (
                <>
                    {mode !== "resize" && (
                        <>
                            <TextField
                                required
                                label="Name"
                                value={name}
                                onChange={(e) => setName(e.target.value)}
                                {...fieldError(error, "name")}
                            />
                            {!volume && (
                                <>
                                    <TextField
                                        select
                                        label="Cloud"
                                        value={cloud?.id || ""}
                                        onChange={(e) =>
                                            setCloud(e.target.value)
                                        }
                                    >
                                        {clouds.data?.map((c) => (
                                            <MenuItem key={c.id} value={c.id}>
                                                {c.name}
                                            </MenuItem>
                                        ))}
                                    </TextField>
                                    <TextField
                                        select
                                        label="Availability zone"
                                        value={zone}
                                        onChange={(e) =>
                                            setZone(e.target.value)
                                        }
                                    >
                                        {cloud?.zones.map((z) => (
                                            <MenuItem key={z.id} value={z.id}>
                                                {z.name}
                                            </MenuItem>
                                        ))}
                                    </TextField>
                                </>
                            )}
                            <TextField
                                select
                                required
                                label="Storage class"
                                value={storage}
                                onChange={(e) => setStorage(e.target.value)}
                            >
                                {classes.map((s) => (
                                    <MenuItem key={s.id} value={s.id}>
                                        {s.name}
                                    </MenuItem>
                                ))}
                            </TextField>
                        </>
                    )}
                    <TextField
                        required
                        type="number"
                        label="Size (GiB)"
                        value={size}
                        onChange={(e) => setSize(e.target.value)}
                        slotProps={{
                            htmlInput: {
                                min: volume
                                    ? volume.size_bytes / 2 ** 30
                                    : 0.001,
                                step: "any",
                            },
                        }}
                        {...fieldError(error, "size_bytes")}
                    />
                    <Typography>
                        {volume
                            ? `Host: ${volume.node}. `
                            : "Placement is automatic within the zone. "}
                        Disks remain bound to their host.{" "}
                        {mode === "resize" &&
                            "Extend the guest partition and filesystem separately."}
                    </Typography>
                    {volume && mode === "clone" && (
                        <QueryState query={detail}>
                            {(v) =>
                                v.catalog === null ? (
                                    <Alert severity="warning">
                                        Storage choices are unavailable.{" "}
                                        <Button
                                            onClick={() =>
                                                void detail.refetch()
                                            }
                                        >
                                            Retry
                                        </Button>
                                    </Alert>
                                ) : v.catalog.storage_classes.length === 0 ? (
                                    <Alert severity="info">
                                        No storage classes are available for
                                        cloning.
                                    </Alert>
                                ) : null
                            }
                        </QueryState>
                    )}
                    {!volume && (
                        <QueryState query={clouds}>{() => null}</QueryState>
                    )}
                </>
            )}
        </OperationDialog>
    );
}
