import { useEffect, useState } from "react";
import { Button, Menu, MenuItem, TextField, Typography } from "@mui/material";
import { useQueryClient } from "@tanstack/react-query";
import { useSearchParams } from "react-router";
import {
    esc,
    useResource,
    useVolumes,
    volumePath,
    type Volume,
} from "../api/hooks";
import { useResourceBusy, volumeResource } from "../api/tasks";
import { ResourceTasks } from "./Tasks";
import { request } from "../api/client";
import { bytes, Confirm, PageTitle, QueryState } from "../components/common";
import { ResourceTable } from "../components/ResourceTable";
import type { Cloud, HostCatalog } from "./MachineForm";
import { fieldError, OperationDialog } from "../components/OperationDialog";

export function Volumes() {
    const q = useVolumes();
    const [create, setCreate] = useState(false);
    const [params] = useSearchParams();
    return (
        <>
            <PageTitle
                title="Volumes"
                count={q.data?.length}
                action={
                    <Button variant="contained" onClick={() => setCreate(true)}>
                        Create volume
                    </Button>
                }
            />
            <QueryState query={q}>
                {(rows) => (
                    <ResourceTable
                        rows={rows}
                        rowKey={(v) => `${v.node}/${v.id}`}
                        search={(v) =>
                            `${v.name} ${v.node} ${v.pool} ${v.attached_to || ""}`
                        }
                        node={(v) => v.node}
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
                            { id: "node", label: "Host", value: (v) => v.node },
                            {
                                id: "pool",
                                label: "Storage class",
                                value: (v) => v.storage_class || "Unmanaged",
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
                                value: (v) => (v.protected ? "Protected" : "—"),
                            },
                        ]}
                        actions={(v) => <VolumeActions volume={v} />}
                        empty="No visible volumes are available."
                    />
                )}
            </QueryState>
            {create && (
                <VolumeDialog
                    nodeContext={params.get("node") || ""}
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
                        await request(
                            volumePath(v.node, v.id, v.cloud),
                            "DELETE",
                        );
                        await queries.invalidateQueries();
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
    nodeContext = "",
    onClose,
}: {
    mode: "create" | "clone" | "resize";
    volume?: Volume;
    nodeContext?: string;
    onClose: () => void;
}) {
    const clouds = useResource<Cloud[]>("/api/v1/clouds");
    const host = useResource<HostCatalog>(
        `/api/v1/hosts/${esc(volume?.node || nodeContext)}/catalog`,
        !!(volume?.node || nodeContext),
    );
    const [cloudID, setCloud] = useState(volume?.cloud || "");
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
        ? host.data?.storage_classes || []
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
            disabled={mode !== "resize" && (!cloud || !storage || !zone)}
            run={() =>
                mode === "resize"
                    ? request(
                          `${volumePath(volume!.node, volume!.id, volume!.cloud)}/resize`,
                          "POST",
                          { size_bytes: Math.round(Number(size) * 2 ** 30) },
                      )
                    : request(
                          mode === "clone"
                              ? `${volumePath(volume!.node, volume!.id, volume!.cloud)}/clone`
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
                    <QueryState query={clouds}>{() => null}</QueryState>
                </>
            )}
        </OperationDialog>
    );
}
