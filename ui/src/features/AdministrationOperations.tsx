import { useQuery } from "@tanstack/react-query";
import {
    Alert,
    Box,
    Button,
    Chip,
    Link,
    MenuItem,
    Stack,
    TextField,
    Typography,
} from "@mui/material";
import { Link as RouterLink, useSearchParams } from "react-router";
import { useMe } from "../app/Auth";
import { request } from "../api/client";
import { useAdministration, type AdminCloud } from "../api/administration";
import {
    type Task,
    type TaskPage,
    type ResourceRef,
    taskLabel,
} from "../api/tasks";
import { type Namespace } from "../app/Workspace";
import { PageTitle, QueryState } from "../components/common";
import { DetailSection } from "../components/Information";
import { ResourceTable } from "../components/ResourceTable";

const taskPath = (id: string) =>
    `/administration/tasks/${encodeURIComponent(id)}/`;
function useAdministrativeTasks(
    filters: URLSearchParams,
    poll = false,
    enabled = true,
) {
    const principal = useMe()?.id;
    const path = `/api/v1/administration/tasks?${filters}`;
    return useQuery({
        queryKey: ["administration", principal, path],
        queryFn: ({ signal }) =>
            request<TaskPage>(path, "GET", undefined, signal),
        enabled,
        refetchInterval: poll ? 5000 : false,
        refetchOnWindowFocus: false,
    });
}
function TaskState({ task }: { task: Task }) {
    return (
        <Stack direction="row" sx={{ gap: 0.5, flexWrap: "wrap" }}>
            <Chip
                size="small"
                variant="outlined"
                label={task.state}
                color={
                    task.state === "failed" || task.outcome_uncertain
                        ? "warning"
                        : task.state === "running"
                          ? "info"
                          : "default"
                }
            />
            {task.outcome_uncertain && (
                <Chip size="small" color="warning" label="Outcome uncertain" />
            )}
            {task.blocks_resources && (
                <Chip
                    size="small"
                    variant="outlined"
                    label="Resources locked"
                />
            )}
        </Stack>
    );
}
function TaskRows({
    items,
    empty,
    filters,
}: {
    items: Task[];
    empty: string;
    filters: URLSearchParams;
}) {
    return items.length ? (
        <Stack component="ul" sx={{ listStyle: "none", m: 0, p: 0 }}>
            {items.map((task) => (
                <Box
                    component="li"
                    key={task.id}
                    sx={{ py: 1, borderBottom: 1, borderColor: "divider" }}
                >
                    <Stack
                        direction="row"
                        sx={{
                            gap: 1,
                            alignItems: "baseline",
                            justifyContent: "space-between",
                            flexWrap: "wrap",
                        }}
                    >
                        <Link
                            component={RouterLink}
                            to={`${taskPath(task.id)}?${filters}`}
                        >
                            {taskLabel(task.kind)}
                        </Link>
                        <Typography variant="caption" color="text.secondary">
                            {new Date(task.created_at).toLocaleString()}
                        </Typography>
                    </Stack>
                    <Typography
                        variant="body2"
                        color="text.secondary"
                        sx={{ overflowWrap: "anywhere", mb: 0.5 }}
                    >
                        {task.cloud_id || "No cloud attribution"} ·{" "}
                        {task.namespace_id || "Unassigned"} ·{" "}
                        {task.resources
                            ?.filter((r) => r.kind !== "cloud")
                            .map((r) => r.id)
                            .join(", ") || task.id}
                    </Typography>
                    <TaskState task={task} />
                </Box>
            ))}
        </Stack>
    ) : (
        <Typography color="text.secondary">{empty}</Typography>
    );
}

export function AdministrationActivity({ cloud }: { cloud?: string }) {
    const base = new URLSearchParams({ limit: "5" });
    if (cloud) base.set("cloud_id", cloud);
    const active = useAdministrativeTasks(
        new URLSearchParams([...base, ["state", "queued,running"]]),
        true,
    );
    const failed = useAdministrativeTasks(
        new URLSearchParams([...base, ["state", "failed,interrupted"]]),
    );
    return (
        <Box
            sx={{
                display: "grid",
                gridTemplateColumns: { xs: "1fr", xl: "1fr 1fr" },
                gap: 2,
            }}
        >
            <DetailSection
                title="Active operations"
                description="Up to five recent queued or running operations. Updates every five seconds."
            >
                <QueryState query={active}>
                    {(data) => (
                        <TaskRows
                            items={data.items || []}
                            filters={
                                new URLSearchParams([
                                    ...base,
                                    ["state", "queued,running"],
                                ])
                            }
                            empty="No active operations in this scope."
                        />
                    )}
                </QueryState>
                <Button
                    component={RouterLink}
                    to={`/administration/tasks/?${new URLSearchParams([...base, ["state", "queued,running"]])}`}
                    sx={{ mt: 1 }}
                >
                    View active operations
                </Button>
            </DetailSection>
            <DetailSection
                title="Recent failures"
                description="Historical failures and interrupted operations; inspect their outcomes before taking action."
            >
                <QueryState query={failed}>
                    {(data) => (
                        <TaskRows
                            items={data.items || []}
                            filters={
                                new URLSearchParams([
                                    ...base,
                                    ["state", "failed,interrupted"],
                                ])
                            }
                            empty="No failed or interrupted operations in this scope."
                        />
                    )}
                </QueryState>
                <Button
                    component={RouterLink}
                    to={`/administration/tasks/?${new URLSearchParams([...base, ["state", "failed,interrupted"]])}`}
                    sx={{ mt: 1 }}
                >
                    View failed operations
                </Button>
            </DetailSection>
        </Box>
    );
}

export function AdministrationResourceTasks({
    cloud,
    resource,
    host,
    limit = 5,
}: {
    cloud?: string;
    resource?: ResourceRef;
    host?: string;
    limit?: number;
}) {
    const filters = new URLSearchParams({ limit: String(limit) });
    if (cloud) filters.set("cloud_id", cloud);
    if (host) filters.set("host", host);
    if (resource) {
        filters.set("resource_kind", resource.kind);
        filters.set("resource_scope", resource.scope);
        filters.set("resource_id", resource.id);
    }
    const q = useAdministrativeTasks(filters, true, !!(resource || host));
    if (!resource && !host) return null;
    return (
        <DetailSection
            title="Recent operations"
            description="Read-only task history for this resource. Updates every five seconds."
        >
            <QueryState query={q}>
                {(data) => (
                    <TaskRows
                        items={data.items || []}
                        filters={filters}
                        empty="No recorded operations for this resource."
                    />
                )}
            </QueryState>
            <Button
                component={RouterLink}
                to={`/administration/tasks/?${filters}`}
                sx={{ mt: 1 }}
            >
                Open task history
            </Button>
        </DetailSection>
    );
}

export function AdministrationTasks() {
    const principal = useMe()?.id;
    const [params, setParams] = useSearchParams();
    const filters = new URLSearchParams({ limit: "25" });
    for (const key of [
        "before",
        "state",
        "cloud_id",
        "namespace_id",
        "unassigned",
        "host",
        "resource_kind",
        "resource_scope",
        "resource_id",
        "blocking",
    ]) {
        const value = params.get(key);
        if (value) filters.set(key, value);
    }
    const q = useAdministrativeTasks(filters, !params.has("before"));
    const clouds = useAdministration<AdminCloud[]>(
        "/api/v1/administration/clouds",
    );
    const namespaces = useAdministration<{ items: Namespace[] }>(
        "/api/v1/administration/configuration/namespaces",
    );
    const update = (key: string, value: string) =>
        setParams((old) => {
            const next = new URLSearchParams(old);
            value ? next.set(key, value) : next.delete(key);
            next.delete("before");
            if (key === "namespace_id") next.delete("unassigned");
            return next;
        });
    const selectedNamespace =
        params.get("unassigned") === "true"
            ? "@unassigned"
            : params.get("namespace_id") || "";
    const namespaceIDs = [
        ...new Set(
            [
                ...(namespaces.data?.items || []).map((n) => n.id),
                ...(q.data?.items || [])
                    .map((t) => t.namespace_id || "")
                    .filter(Boolean),
                params.get("namespace_id") || "",
            ].filter(Boolean),
        ),
    ].sort();
    const cloudIDs = [
        ...new Set(
            [
                ...(clouds.data || []).map((c) => c.id),
                ...(q.data?.items || [])
                    .map((t) => t.cloud_id || "")
                    .filter(Boolean),
                params.get("cloud_id") || "",
            ].filter(Boolean),
        ),
    ].sort();
    return (
        <>
            <PageTitle
                title="Task history across namespaces"
                description="Read-only administrative operations. Workspace task controls remain in Workspace tasks."
                action={
                    <Button onClick={() => void q.refetch()}>Refresh</Button>
                }
            />
            <Stack direction="row" sx={{ gap: 1.5, flexWrap: "wrap", mb: 2 }}>
                <TextField
                    select
                    label="Cloud"
                    value={params.get("cloud_id") || ""}
                    sx={{ minWidth: 170 }}
                    slotProps={{
                        inputLabel: { shrink: true },
                        select: { displayEmpty: true },
                    }}
                    onChange={(e) => update("cloud_id", e.target.value)}
                >
                    <MenuItem value="">All clouds</MenuItem>
                    {cloudIDs.map((id) => (
                        <MenuItem key={id} value={id}>
                            {clouds.data?.find((c) => c.id === id)?.name || id}
                        </MenuItem>
                    ))}
                </TextField>
                <TextField
                    select
                    label="Namespace"
                    value={selectedNamespace}
                    sx={{ minWidth: 170 }}
                    slotProps={{
                        inputLabel: { shrink: true },
                        select: { displayEmpty: true },
                    }}
                    onChange={(e) => {
                        if (e.target.value !== "@unassigned")
                            update("namespace_id", e.target.value);
                        else
                            setParams((old) => {
                                const next = new URLSearchParams(old);
                                next.delete("namespace_id");
                                next.delete("before");
                                next.set("unassigned", "true");
                                return next;
                            });
                    }}
                >
                    <MenuItem value="">All namespaces</MenuItem>
                    <MenuItem value="@unassigned">Unassigned</MenuItem>
                    {namespaceIDs.map((id) => (
                        <MenuItem key={id} value={id}>
                            {id}
                        </MenuItem>
                    ))}
                </TextField>
                <TextField
                    select
                    label="Operation state"
                    value={params.get("state") || ""}
                    sx={{ minWidth: 180 }}
                    slotProps={{
                        inputLabel: { shrink: true },
                        select: { displayEmpty: true },
                    }}
                    onChange={(e) => update("state", e.target.value)}
                >
                    <MenuItem value="">All states</MenuItem>
                    {[
                        ...["queued,running", "failed,interrupted"],
                        ...[
                            "queued",
                            "running",
                            "succeeded",
                            "failed",
                            "cancelled",
                            "interrupted",
                        ],
                        ...(params.get("state") ? [params.get("state")!] : []),
                    ]
                        .filter((v, i, a) => a.indexOf(v) === i)
                        .map((state) => (
                            <MenuItem key={state} value={state}>
                                {state === "queued,running"
                                    ? "Active"
                                    : state === "failed,interrupted"
                                      ? "Failed / interrupted"
                                      : state}
                            </MenuItem>
                        ))}
                </TextField>
                <Button
                    onClick={() =>
                        setParams((old) => {
                            const next = new URLSearchParams();
                            const workspace = old.get("namespace");
                            if (workspace) next.set("namespace", workspace);
                            return next;
                        })
                    }
                >
                    Reset filters
                </Button>
            </Stack>
            {(params.has("host") || params.has("resource_id")) && (
                <Alert severity="info" sx={{ mb: 2 }}>
                    Filtered to{" "}
                    {params.get("host")
                        ? `host ${params.get("host")}`
                        : `${params.get("resource_kind")}: ${params.get("resource_id")}`}
                    .{" "}
                    <Button
                        onClick={() =>
                            setParams((old) => {
                                const next = new URLSearchParams(old);
                                [
                                    "host",
                                    "resource_kind",
                                    "resource_scope",
                                    "resource_id",
                                    "before",
                                ].forEach((k) => next.delete(k));
                                return next;
                            })
                        }
                    >
                        Clear resource filter
                    </Button>
                </Alert>
            )}
            <QueryState query={q}>
                {(data) => (
                    <>
                        <ResourceTable
                            rows={data.items || []}
                            rowKey={(t) => t.id}
                            search={(t) =>
                                `${t.id} ${t.kind} ${t.namespace_id || ""} ${t.cloud_id || ""} ${t.resources?.map((r) => r.id).join(" ") || ""}`
                            }
                            paramPrefix="page_"
                            preferenceKey={`administration:${principal}:tasks`}
                            columns={[
                                {
                                    id: "kind",
                                    label: "Operation",
                                    value: (t) => t.kind,
                                    render: (t) => (
                                        <Link
                                            component={RouterLink}
                                            to={`${taskPath(t.id)}?${params}`}
                                        >
                                            {taskLabel(t.kind)}
                                        </Link>
                                    ),
                                },
                                {
                                    id: "state",
                                    label: "State",
                                    value: (t) => t.state,
                                    render: (t) => <TaskState task={t} />,
                                },
                                {
                                    id: "namespace",
                                    label: "Namespace",
                                    value: (t) =>
                                        t.namespace_id || "Unassigned",
                                },
                                {
                                    id: "cloud",
                                    label: "Cloud",
                                    value: (t) => t.cloud_id || "—",
                                },
                                {
                                    id: "resources",
                                    label: "Resources",
                                    value: (t) =>
                                        t.resources
                                            ?.filter((r) => r.kind !== "cloud")
                                            .map((r) => r.id)
                                            .join(", ") || "—",
                                },
                                {
                                    id: "created",
                                    label: "Accepted",
                                    value: (t) => t.created_at,
                                    render: (t) =>
                                        new Date(t.created_at).toLocaleString(),
                                },
                                {
                                    id: "id",
                                    label: "ID",
                                    value: (t) => t.id,
                                    defaultHidden: true,
                                },
                            ]}
                            empty="No recorded tasks match these filters."
                        />
                        <Typography variant="caption" color="text.secondary">
                            Search and table sorting apply to this page of
                            history.
                        </Typography>
                        <Stack direction="row" sx={{ gap: 1, mt: 1 }}>
                            {params.has("before") && (
                                <Button onClick={() => update("before", "")}>
                                    Newest tasks
                                </Button>
                            )}
                            {data.next && (
                                <Button
                                    onClick={() =>
                                        setParams((old) => {
                                            const next = new URLSearchParams(
                                                old,
                                            );
                                            next.set("before", data.next!);
                                            return next;
                                        })
                                    }
                                >
                                    Older tasks
                                </Button>
                            )}
                        </Stack>
                    </>
                )}
            </QueryState>
        </>
    );
}
