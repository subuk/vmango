import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Alert, Button, Link, Menu, MenuItem, TextField } from "@mui/material";
import { Link as RouterLink, useSearchParams } from "react-router";
import {
    machineURL,
    useMachines,
    useNetworks,
    useNodes,
    useOperation,
    useResource,
    vmPath,
    type VM,
} from "../api/hooks";
import { useResourceBusy } from "../api/tasks";
import { ResourceTasks } from "./Tasks";
import { APIError, request } from "../api/client";
import {
    bytes,
    Confirm,
    Copy,
    ErrorMessage,
    PageTitle,
    QueryState,
    State,
} from "../components/common";
import type { Cloud } from "./MachineForm";
import { ResourceTable } from "../components/ResourceTable";

export function Machines() {
    const [params, setParams] = useSearchParams();
    const clouds = useResource<Cloud[]>("/api/v1/clouds");
    const cloudID = params.get("cloud") || clouds.data?.[0]?.id || "";
    const q = useMachines(cloudID);
    if (clouds.isSuccess && clouds.data.length === 0) {
        return (
            <>
                <PageTitle title="Machines" />
                <Alert severity="info">
                    No clouds are configured. Add a cloud catalog and host
                    bindings to the application configuration.
                </Alert>
            </>
        );
    }
    return (
        <>
            <PageTitle
                title="Machines"
                count={q.data?.length}
                action={
                    <Button
                        component={RouterLink}
                        to={`/machines/add/${cloudID ? `?cloud=${encodeURIComponent(cloudID)}` : ""}`}
                        variant="contained"
                    >
                        Create machine
                    </Button>
                }
            />
            {clouds.data && clouds.data.length > 1 && (
                <TextField
                    select
                    label="Cloud"
                    value={cloudID}
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
            <QueryState query={q}>
                {(rows) => (
                    <ResourceTable
                        rows={rows.filter(
                            (v) => !cloudID || v.cloud === cloudID,
                        )}
                        rowKey={(v) => `${v.node}/${v.id}`}
                        search={(v) =>
                            `${v.id} ${v.node} ${v.ip_addresses.join(" ")}`
                        }
                        node={(v) => v.node}
                        state={(v) => v.state}
                        columns={[
                            {
                                id: "name",
                                label: "Name",
                                value: (v) => v.id,
                                render: (v) => (
                                    <Link
                                        component={RouterLink}
                                        to={machineURL(v.node, v.id)}
                                    >
                                        {v.id}
                                    </Link>
                                ),
                            },
                            { id: "node", label: "Host", value: (v) => v.node },
                            {
                                id: "state",
                                label: "State",
                                value: (v) => v.state,
                                render: (v) => (
                                    <>
                                        <State value={v.state} />
                                        <ResourceTasks
                                            compact
                                            resource={{
                                                kind: "machine",
                                                scope: v.node,
                                                id: v.id,
                                            }}
                                        />
                                    </>
                                ),
                            },
                            {
                                id: "cpu",
                                label: "CPU",
                                value: (v) => v.vcpus,
                                numeric: true,
                            },
                            {
                                id: "memory",
                                label: "Memory",
                                value: (v) => v.memory_bytes,
                                render: (v) => bytes(v.memory_bytes),
                                numeric: true,
                            },
                            {
                                id: "ip",
                                label: "IP",
                                value: (v) => v.ip_addresses.join(", "),
                                render: (v) =>
                                    v.ip_addresses.map((ip) => (
                                        <div key={ip}>
                                            <span className="mono">{ip}</span>
                                            <Copy
                                                text={ip}
                                                label={`Copy IP ${ip}`}
                                            />
                                        </div>
                                    )),
                            },
                        ]}
                        actions={(v) => <MachineActions vm={v} />}
                        empty="No machines yet. Create your first machine to get started."
                    />
                )}
            </QueryState>
        </>
    );
}
export function MachineActions({
    vm,
    full = false,
}: {
    vm: VM;
    full?: boolean;
}) {
    const blocked = useResourceBusy({
        kind: "machine",
        scope: vm.node,
        id: vm.id,
    });
    const conflict =
        !!blocked.task || blocked.unavailable || vm.managed === false;
    const queries = useQueryClient();
    const [anchor, setAnchor] = useState<HTMLElement | null>(null);
    const [confirm, setConfirm] = useState<string>();
    const op = useOperation<string>((action) =>
        request(`${vmPath(vm.node, vm.id, vm.cloud)}/actions`, "POST", {
            action,
        }),
    );
    const uncertain = op.error instanceof APIError && op.error.uncertain;
    return (
        <>
            <ErrorMessage error={op.error} />
            {vm.managed === false && (
                <Button
                    disabled={!!blocked.task || blocked.unavailable}
                    onClick={() => setConfirm("adopt")}
                >
                    Adopt machine
                </Button>
            )}
            {uncertain && (
                <Button href={location.pathname + location.search}>
                    Reload resource state
                </Button>
            )}
            {vm.state === "stopped" ? (
                <Button
                    disabled={
                        op.isPending ||
                        ((conflict || uncertain) &&
                            !(op.error instanceof APIError && op.error.recover))
                    }
                    onClick={() => op.mutate("start")}
                >
                    {op.isPending
                        ? "Submitting…"
                        : op.error instanceof APIError && op.error.recover
                          ? "Recover submission"
                          : "Start"}
                </Button>
            ) : (
                vm.state === "running" && (
                    <Button
                        disabled={vm.managed === false}
                        component={RouterLink}
                        to={`${machineURL(vm.node, vm.id)}console/`}
                    >
                        Console
                    </Button>
                )
            )}
            <Button
                aria-label={`More actions for ${vm.id}`}
                aria-haspopup="menu"
                aria-expanded={!!anchor}
                onClick={(e) => setAnchor(e.currentTarget)}
            >
                More
            </Button>
            <Menu
                anchorEl={anchor}
                open={!!anchor}
                onClose={() => setAnchor(null)}
            >
                <MenuItem
                    disabled={conflict}
                    component={RouterLink}
                    to={`${machineURL(vm.node, vm.id)}update/`}
                >
                    Edit configuration
                </MenuItem>
                {vm.state === "running" &&
                    ["reboot", "poweroff"].map((action) => (
                        <MenuItem
                            key={action}
                            disabled={conflict}
                            onClick={() => {
                                setAnchor(null);
                                setConfirm(action);
                            }}
                        >
                            {action === "poweroff"
                                ? "Hard power-off…"
                                : "Reboot…"}
                        </MenuItem>
                    ))}
                <MenuItem
                    disabled={conflict}
                    sx={{ color: "error.main" }}
                    onClick={() => {
                        setAnchor(null);
                        setConfirm("delete");
                    }}
                >
                    Delete…
                </MenuItem>
            </Menu>
            {full && (
                <Button
                    component={RouterLink}
                    to={`${machineURL(vm.node, vm.id)}update/`}
                    disabled={conflict}
                >
                    Edit
                </Button>
            )}
            {confirm && (
                <Confirm
                    title={`${confirm === "adopt" ? "Adopt" : confirm === "delete" ? "Delete" : confirm === "poweroff" ? "Hard power-off" : "Reboot"} ${vm.id}?`}
                    action={
                        confirm === "adopt"
                            ? "Adopt machine"
                            : confirm === "delete"
                              ? "Delete machine"
                              : confirm === "poweroff"
                                ? "Hard power-off"
                                : "Reboot"
                    }
                    extra={
                        confirm === "delete"
                            ? "Also delete attached disks (permanent)"
                            : undefined
                    }
                    onClose={() => setConfirm(undefined)}
                    run={async (deleteVolumes) => {
                        if (confirm === "adopt")
                            await request(
                                `/api/v1/clouds/${encodeURIComponent(vm.cloud)}/machines/${encodeURIComponent(vm.node)}/${encodeURIComponent(vm.id)}/adopt`,
                                "POST",
                                {},
                            );
                        else if (confirm === "delete")
                            await request(
                                `${vmPath(vm.node, vm.id, vm.cloud)}?delete_volumes=${deleteVolumes}`,
                                "DELETE",
                            );
                        else
                            await request(
                                `${vmPath(vm.node, vm.id, vm.cloud)}/actions`,
                                "POST",
                                { action: confirm },
                            );
                        await queries.invalidateQueries();
                    }}
                >
                    {confirm === "adopt"
                        ? "Take this machine under management without changing its configuration or claiming ownership of its existing disks."
                        : confirm === "delete"
                          ? `Permanently remove ${vm.node}/${vm.id}. Attached disks are kept unless you explicitly choose to delete them.`
                          : confirm === "poweroff"
                            ? `Immediately cut power to ${vm.node}/${vm.id}. Unsaved guest data may be lost.`
                            : `Reboot ${vm.node}/${vm.id}.`}
                </Confirm>
            )}
        </>
    );
}
export function Nodes() {
    const q = useNodes();
    return (
        <>
            <PageTitle title="Hosts" count={q.data?.length} />
            <QueryState query={q}>
                {(rows) => (
                    <ResourceTable
                        rows={rows}
                        rowKey={(n) => n.id}
                        search={(n) => `${n.id} ${n.hostname} ${n.arch}`}
                        columns={[
                            {
                                id: "name",
                                label: "Name",
                                value: (n) => n.id,
                                render: (n) => (
                                    <Link
                                        component={RouterLink}
                                        to={`/nodes/${encodeURIComponent(n.id)}/`}
                                    >
                                        {n.id}
                                    </Link>
                                ),
                            },
                            {
                                id: "host",
                                label: "Hostname",
                                value: (n) => n.hostname,
                            },
                            {
                                id: "arch",
                                label: "Architecture",
                                value: (n) => n.arch,
                            },
                            {
                                id: "cpus",
                                label: "Host CPUs",
                                value: (n) => n.cpus.length,
                                numeric: true,
                            },
                            {
                                id: "memory",
                                label: "Memory",
                                value: (n) => n.memory_bytes,
                                render: (n) => bytes(n.memory_bytes),
                                numeric: true,
                            },
                        ]}
                        empty="No nodes are configured."
                    />
                )}
            </QueryState>
        </>
    );
}
export function Networks() {
    const q = useNetworks();
    return (
        <>
            <PageTitle title="Networks" count={q.data?.length} />
            <QueryState query={q}>
                {(rows) => (
                    <ResourceTable
                        rows={rows}
                        rowKey={(n) => `${n.node}/${n.name}`}
                        search={(n) => `${n.name} ${n.node}`}
                        node={(n) => n.node}
                        columns={[
                            { id: "name", label: "Name", value: (n) => n.name },
                            { id: "node", label: "Node", value: (n) => n.node },
                        ]}
                        empty="No networks are available."
                    />
                )}
            </QueryState>
        </>
    );
}
