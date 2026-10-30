import { useState } from "react";
import { Button, Menu, MenuItem, TextField, Typography } from "@mui/material";
import { useQueryClient } from "@tanstack/react-query";
import { useSearchParams } from "react-router";
import {
    esc,
    useNodes,
    useResource,
    useVolumes,
    volumePath,
    type Pool,
    type Volume,
} from "../api/hooks";
import { request } from "../api/client";
import { bytes, Confirm, PageTitle, QueryState } from "../components/common";
import { ResourceTable } from "../components/ResourceTable";
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
                        pool={(v) => v.pool}
                        columns={[
                            { id: "name", label: "Name", value: (v) => v.name },
                            { id: "node", label: "Node", value: (v) => v.node },
                            { id: "pool", label: "Pool", value: (v) => v.pool },
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
    const [anchor, setAnchor] = useState<HTMLElement | null>(null);
    const [mode, setMode] = useState<string>();
    const queries = useQueryClient();
    return (
        <>
            <Button
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
                            action === "delete" &&
                            (v.protected || !!v.attached_to)
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
                        await request(volumePath(v.node, v.id), "DELETE");
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

function VolumeDialog({
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
    const nodes = useNodes();
    const [node, setNode] = useState(volume?.node || nodeContext);
    const [pool, setPool] = useState(volume?.pool || "");
    const [name, setName] = useState(
        mode === "clone" ? `${volume!.name}-copy` : "",
    );
    const [format, setFormat] = useState(volume?.format || "qcow2");
    const [size, setSize] = useState(
        volume ? String(volume.size_bytes / 2 ** 30) : "10",
    );
    const pools = useResource<Pool[]>(
        `/api/v1/nodes/${esc(node)}/pools`,
        !!node,
        false,
    );
    const selected = pools.data?.find((p) => p.name === pool);
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
            run={() => {
                if (mode === "resize")
                    return request(
                        `${volumePath(volume!.node, volume!.id)}/resize`,
                        "POST",
                        { size_bytes: Math.round(Number(size) * 2 ** 30) },
                    );
                return request(
                    mode === "clone"
                        ? `${volumePath(volume!.node, volume!.id)}/clone`
                        : `/api/v1/nodes/${esc(node)}/volumes`,
                    "POST",
                    {
                        name,
                        pool,
                        format,
                        size_bytes: Math.round(Number(size) * 2 ** 30),
                    },
                );
            }}
        >
            {(error) => (
                <>
                    {mode !== "resize" && (
                        <>
                            <TextField
                                select
                                label="Node"
                                value={node}
                                required
                                disabled={!!volume}
                                onChange={(e) => setNode(e.target.value)}
                            >
                                <MenuItem value="">Choose a node</MenuItem>
                                {nodes.data?.map((n) => (
                                    <MenuItem key={n.id} value={n.id}>
                                        {n.id}
                                    </MenuItem>
                                ))}
                            </TextField>
                            <TextField
                                label="Name"
                                name="name"
                                required
                                value={name}
                                onChange={(e) => setName(e.target.value)}
                                {...fieldError(error, "name")}
                            />
                            <TextField
                                select
                                label="Storage pool"
                                name="pool"
                                required
                                value={pool}
                                onChange={(e) => setPool(e.target.value)}
                                helperText={
                                    pools.isError
                                        ? "Could not load storage pools."
                                        : selected
                                          ? `${bytes(selected.free_bytes)} free`
                                          : "Choose a pool on the selected node."
                                }
                                error={
                                    pools.isError ||
                                    (!!pool && !!pools.data && !selected)
                                }
                            >
                                <MenuItem value="">Choose a pool</MenuItem>
                                {pool && !selected && (
                                    <MenuItem value={pool} disabled>
                                        {pool} (unavailable)
                                    </MenuItem>
                                )}
                                {pools.data?.map((p) => (
                                    <MenuItem key={p.name} value={p.name}>
                                        {p.name}
                                    </MenuItem>
                                ))}
                            </TextField>
                            <TextField
                                select
                                label="Format"
                                value={format}
                                onChange={(e) => setFormat(e.target.value)}
                            >
                                {["qcow2", "raw"].map((f) => (
                                    <MenuItem key={f} value={f}>
                                        {f}
                                    </MenuItem>
                                ))}
                            </TextField>
                        </>
                    )}
                    <TextField
                        label="Size (GiB)"
                        name="size_bytes"
                        type="number"
                        required
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
                    {mode === "resize" && (
                        <Typography variant="body2">
                            Current size: {bytes(volume!.size_bytes)}. Only
                            growing is supported. Extend the guest partition and
                            filesystem separately.
                        </Typography>
                    )}
                </>
            )}
        </OperationDialog>
    );
}
