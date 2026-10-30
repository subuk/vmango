import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
    Alert,
    Box,
    Button,
    Link,
    Menu,
    MenuItem,
    TextField,
    Typography,
} from "@mui/material";
import { Link as RouterLink, useSearchParams } from "react-router";
import {
    machineURL,
    useMachines,
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
import { Summary } from "../components/Information";
import { ResourceTable } from "../components/ResourceTable";

export function Machines() {
    const [params, setParams] = useSearchParams();
    const clouds = useResource<Cloud[]>("/api/v1/clouds");
    const cloudID = params.get("cloud") || clouds.data?.[0]?.id || "";
    const q = useMachines(cloudID);
    const machines = q.data?.items.filter(
        (v) => !cloudID || v.cloud === cloudID,
    );
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
                description="Manage virtual machines, review their resources and connect to a console."
                count={q.data?.items.length}
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
            {machines && (
                <Summary
                    label="Machine inventory summary"
                    items={[
                        {
                            label: "Running",
                            value: machines.filter((v) => v.state === "running")
                                .length,
                            detail: "Powered on",
                            tone: "success",
                        },
                        {
                            label: "Stopped",
                            value: machines.filter((v) => v.state === "stopped")
                                .length,
                            detail: "Powered off",
                        },
                        {
                            label: "Configured vCPUs",
                            value: machines.reduce(
                                (sum, v) => sum + v.vcpus,
                                0,
                            ),
                            detail: "Across listed machines",
                        },
                        {
                            label: "Configured memory",
                            value: bytes(
                                machines.reduce(
                                    (sum, v) => sum + v.memory_bytes,
                                    0,
                                ),
                            ),
                            detail: "Across listed machines",
                        },
                    ]}
                />
            )}
            {clouds.data && clouds.data.length > 1 && (
                <TextField
                    select
                    label="Cloud"
                    sx={{ maxWidth: 300, mb: 3 }}
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
            {!!q.data?.unavailable_hosts.length && (
                <Alert severity="warning">
                    The list is incomplete. Unavailable hosts:{" "}
                    {q.data.unavailable_hosts.join(", ")}.
                </Alert>
            )}
            {!!clouds.data?.find((c) => c.id === cloudID)?.unavailable_hosts
                .length && (
                <Alert severity="warning">
                    Some hosts could not be checked. Available configurations
                    may be incomplete.
                </Alert>
            )}
            <QueryState query={q}>
                {(result) => (
                    <ResourceTable
                        rows={result.items.filter(
                            (v) => !cloudID || v.cloud === cloudID,
                        )}
                        rowKey={(v) => `${v.node}/${v.id}`}
                        search={(v) =>
                            `${v.name} ${v.id} ${v.node} ${v.ip_addresses.join(" ")}`
                        }
                        node={(v) => v.node}
                        state={(v) => v.state}
                        columns={[
                            {
                                id: "name",
                                label: "Name",
                                value: (v) => v.name,
                                render: (v) => (
                                    <Box>
                                        <Link
                                            component={RouterLink}
                                            to={machineURL(v.cloud, v.id)}
                                            sx={{ fontWeight: 600 }}
                                        >
                                            {v.name}
                                        </Link>
                                        <Typography
                                            variant="body2"
                                            color="text.secondary"
                                        >
                                            {v.instance_type ||
                                                "Unassigned type"}
                                            {v.managed === false
                                                ? " · Read-only"
                                                : ""}
                                        </Typography>
                                    </Box>
                                ),
                            },
                            {
                                id: "node",
                                label: "Host",
                                value: (v) => v.node,
                                render: (v) => (
                                    <Box>
                                        <Link
                                            component={RouterLink}
                                            to={`/nodes/${encodeURIComponent(v.node)}/`}
                                        >
                                            {v.node}
                                        </Link>
                                        <Typography
                                            variant="body2"
                                            color="text.secondary"
                                        >
                                            {v.zone || "No zone"}
                                        </Typography>
                                    </Box>
                                ),
                            },
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
                                label: "vCPUs",
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
                                label: "IP addresses",
                                value: (v) => v.ip_addresses.join(", "),
                                render: (v) =>
                                    v.ip_addresses.length ? (
                                        v.ip_addresses.map((ip) => (
                                            <div key={ip}>
                                                <span className="mono">
                                                    {ip}
                                                </span>
                                                <Copy
                                                    text={ip}
                                                    label={`Copy IP ${ip}`}
                                                />
                                            </div>
                                        ))
                                    ) : (
                                        <Typography
                                            variant="body2"
                                            color="text.secondary"
                                        >
                                            No IP reported
                                        </Typography>
                                    ),
                            },
                        ]}
                        actions={(v) => <MachineActions vm={v} />}
                        empty={
                            result.unavailable_hosts.length
                                ? "No machines returned. Some hosts are unavailable."
                                : "No machines yet. Create your first machine to get started."
                        }
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
        request(`${vmPath(vm.cloud, vm.id)}/actions`, "POST", {
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
                        to={`${machineURL(vm.cloud, vm.id)}console/`}
                    >
                        Console
                    </Button>
                )
            )}
            <Button
                aria-label={`More actions for ${vm.name}`}
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
                    to={`${machineURL(vm.cloud, vm.id)}update/`}
                >
                    Change instance type
                </MenuItem>
                {vm.state === "running" &&
                    ["shutdown", "reboot", "poweroff"].map((action) => (
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
                                : action === "shutdown" ? "Shutdown…" : "Reboot…"}
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
                    to={`${machineURL(vm.cloud, vm.id)}update/`}
                    disabled={conflict}
                >
                    Change instance type
                </Button>
            )}
            {confirm && (
                <Confirm
                    title={`${confirm === "adopt" ? "Adopt" : confirm === "delete" ? "Delete" : confirm === "poweroff" ? "Hard power-off" : confirm === "shutdown" ? "Shutdown" : "Reboot"} ${vm.name}?`}
                    action={
                        confirm === "adopt"
                            ? "Adopt machine"
                            : confirm === "delete"
                              ? "Delete machine"
                              : confirm === "poweroff"
                                ? "Hard power-off"
                                : confirm === "shutdown" ? "Shutdown" : "Reboot"
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
                                `/api/v1/clouds/${encodeURIComponent(vm.cloud)}/machines/${encodeURIComponent(vm.id)}/adopt`,
                                "POST",
                                {},
                            );
                        else if (confirm === "delete")
                            await request(
                                `${vmPath(vm.cloud, vm.id)}?delete_volumes=${deleteVolumes}`,
                                "DELETE",
                            );
                        else
                            await request(
                                `${vmPath(vm.cloud, vm.id)}/actions`,
                                "POST",
                                { action: confirm },
                            );
                        await queries.invalidateQueries();
                    }}
                >
                    {confirm === "adopt"
                        ? "Take this machine under management without changing its configuration or claiming ownership of its existing disks."
                        : confirm === "delete"
                          ? `Permanently remove ${vm.node}/${vm.name}. Attached disks are kept unless you explicitly choose to delete them.`
                          : confirm === "poweroff"
                            ? `Immediately cut power to ${vm.node}/${vm.name}. Unsaved guest data may be lost.`
                            : confirm === "shutdown" ? `Request graceful shutdown of ${vm.node}/${vm.name}. The guest operating system must respond to the request.` : `Reboot ${vm.node}/${vm.name}.`}
                </Confirm>
            )}
        </>
    );
}
export function Nodes() {
    const q = useNodes();
    return (
        <>
            <PageTitle
                title="Hosts"
                count={q.data?.length}
                description="Inspect host hardware, CPU topology and storage capacity."
            />
            {q.data && (
                <Summary
                    label="Host inventory summary"
                    items={[
                        {
                            label: "Hosts",
                            value: q.data.length,
                            detail: "Returned by the backend",
                        },
                        {
                            label: "Host CPUs",
                            value: q.data.reduce(
                                (sum, n) => sum + n.cpus.length,
                                0,
                            ),
                            detail: "Logical processors",
                        },
                        {
                            label: "Total memory",
                            value: bytes(
                                q.data.reduce(
                                    (sum, n) => sum + n.memory_bytes,
                                    0,
                                ),
                            ),
                            detail: "Installed host memory",
                        },
                        {
                            label: "Architectures",
                            value: new Set(q.data.map((n) => n.arch)).size,
                            detail:
                                [...new Set(q.data.map((n) => n.arch))].join(
                                    " · ",
                                ) || "No hosts",
                        },
                    ]}
                />
            )}
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
    const [params, setParams] = useSearchParams();
    const clouds = useResource<Cloud[]>("/api/v1/clouds");
    const cloudID = params.get("cloud") || clouds.data?.[0]?.id || "";
    const cloud = clouds.data?.find((c) => c.id === cloudID);
    return (
        <>
            <PageTitle
                title="Networks"
                count={cloud?.networks.length}
                description="Logical networks available to machines in the selected cloud."
            />
            <QueryState query={clouds}>
                {(catalogs) => (
                    <>
                        {catalogs.length === 0 ? (
                            <Alert severity="info">
                                No clouds are configured. Contact your
                                administrator.
                            </Alert>
                        ) : (
                            <>
                                <TextField
                                    select
                                    label="Cloud"
                                    sx={{ mb: 3, maxWidth: 300 }}
                                    value={cloud ? cloudID : ""}
                                    onChange={(e) =>
                                        setParams({ cloud: e.target.value })
                                    }
                                >
                                    {catalogs.map((c) => (
                                        <MenuItem key={c.id} value={c.id}>
                                            {c.name}
                                        </MenuItem>
                                    ))}
                                </TextField>
                                {cloud ? (
                                    <ResourceTable
                                        rows={cloud.networks}
                                        rowKey={(n) => n.id}
                                        search={(n) => `${n.id} ${n.name}`}
                                        columns={[
                                            {
                                                id: "name",
                                                label: "Name",
                                                value: (n) => n.name,
                                            },
                                            {
                                                id: "id",
                                                label: "Network ID",
                                                value: (n) => n.id,
                                            },
                                        ]}
                                        empty="No logical networks are configured in this cloud."
                                    />
                                ) : (
                                    <Alert severity="info">
                                        Select a configured cloud to view its
                                        logical networks.
                                    </Alert>
                                )}
                            </>
                        )}
                    </>
                )}
            </QueryState>
        </>
    );
}
