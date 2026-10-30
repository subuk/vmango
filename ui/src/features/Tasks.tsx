import { useEffect, useRef, useState } from "react";
import {
    Alert,
    Box,
    Button,
    Chip,
    Link,
    LinearProgress,
    MenuItem,
    Stack,
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableRow,
    TextField,
    Typography,
} from "@mui/material";
import { Link as RouterLink, useParams, useSearchParams } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import { onTaskAccepted, request, type TaskReference } from "../api/client";
import {
    taskLabel,
    stageLabel,
    taskURL,
    terminal,
    useTask,
    useActiveTasks,
    useTaskPage,
    useResourceTasks,
    type ResourceRef,
    type Task,
} from "../api/tasks";
import { machineURL } from "../api/hooks";
import {
    Confirm,
    Copy,
    ErrorMessage,
    PageTitle,
    QueryState,
    bytes,
} from "../components/common";

const time = (value?: string) =>
    value ? new Date(value).toLocaleString() : "—";
function TaskLink({ task }: { task: Task }) {
    return (
        <Link component={RouterLink} to={taskURL(task.id)}>
            {taskLabel(task.kind)} · {task.state}
        </Link>
    );
}

// This observer lives above routes so closing a form never loses observation.
export function TaskMonitor() {
    const query = useTaskPage(new URLSearchParams({ limit: "10" }));
    const active = useActiveTasks();
    const known = useRef(new Map<string, Task>());
    const [resolved, setResolved] = useState<Task[]>([]);
    const queries = useQueryClient();
    const previous = useRef(new Map<string, string>());
    const [accepted, setAccepted] = useState<TaskReference>();
    const [dismissed, setDismissed] = useState<string[]>([]);
    useEffect(
        () =>
            onTaskAccepted((task) => {
                setAccepted(task);
                known.current.set(task.task.id, task.task);
                queries.setQueryData([task.status_url], task.task);
                void queries.invalidateQueries({
                    predicate: (q) =>
                        String(q.queryKey[0]).startsWith("/api/v1/tasks"),
                });
            }),
        [queries],
    );
    useEffect(() => {
        for (const task of query.data?.items || []) {
            if (
                terminal(task) &&
                previous.current.get(task.id) !== task.state
            ) {
                void queries.invalidateQueries({
                    predicate: (q) =>
                        !String(q.queryKey[0]).startsWith("/api/v1/tasks"),
                });
            }
            previous.current.set(task.id, task.state);
        }
    }, [query.data, queries]);
    useEffect(() => {
        if (!active.data) return;
        const present = new Set(active.data.map((task) => task.id));
        for (const task of active.data) known.current.set(task.id, task);
        for (const [id, task] of known.current) {
            if (present.has(id) || terminal(task)) continue;
            void request<Task>(`/api/v1/tasks/${encodeURIComponent(id)}`)
                .then((result) => {
                    known.current.set(id, result);
                    if (terminal(result)) {
                        known.current.delete(id);
                        setResolved((old) =>
                            [result, ...old.filter((t) => t.id !== id)].slice(
                                0,
                                20,
                            ),
                        );
                        void queries.invalidateQueries({
                            predicate: (q) =>
                                !String(q.queryKey[0]).startsWith(
                                    "/api/v1/tasks",
                                ),
                        });
                    }
                })
                .catch(() => {
                    /* Keep the last known state and retry observation on the next poll. */
                });
        }
    }, [active.data, active.dataUpdatedAt, queries]);
    const observed = [...(query.data?.items || []), ...resolved];
    const acceptedTask = accepted
        ? observed.find((t) => t.id === accepted.task.id) ||
          active.data?.find((t) => t.id === accepted.task.id) ||
          accepted.task
        : undefined;
    const failures = observed.filter(
        (t, index) =>
            ["failed", "interrupted"].includes(t.state) &&
            !dismissed.includes(t.id) &&
            observed.findIndex((other) => other.id === t.id) === index,
    );
    return (
        <Stack spacing={1} sx={{ mb: 2 }}>
            <Box>
                <Link component={RouterLink} to="/tasks/">
                    Tasks
                </Link>{" "}
                <Chip
                    size="small"
                    label={
                        query.data
                            ? `${query.data.active} active`
                            : "Loading tasks…"
                    }
                />
            </Box>
            {(query.isError || active.isError) && (
                <Alert severity="warning">
                    Task status is temporarily unavailable. Last known status is
                    retained.{" "}
                    <Button
                        onClick={() => {
                            void query.refetch();
                            void active.refetch();
                        }}
                    >
                        Retry
                    </Button>
                </Alert>
            )}
            {acceptedTask && (
                <Alert severity="info" onClose={() => setAccepted(undefined)}>
                    Request accepted. <TaskLink task={acceptedTask} /> Closing
                    this message does not cancel it.
                </Alert>
            )}
            {(active.data || []).slice(0, 5).map((task) => (
                <Box key={task.id}>
                    <Typography variant="body2">
                        <TaskLink task={task} /> ·{" "}
                        {(task.resources || [])
                            .filter((r) => r.kind === "machine")
                            .map((r) => r.id)
                            .join(", ")}
                    </Typography>
                    {task.stages?.at(-1)?.transfer && (
                        <TransferStatus
                            transfer={task.stages.at(-1)!.transfer!}
                            running={true}
                        />
                    )}
                </Box>
            ))}
            {failures.map((task) => (
                <Alert
                    key={task.id}
                    severity={
                        task.state === "interrupted" ? "warning" : "error"
                    }
                    onClose={() => setDismissed((old) => [...old, task.id])}
                >
                    <TaskLink task={task} />
                    {task.outcome_uncertain
                        ? ". Backend outcome is uncertain; inspect the task."
                        : ". See failure details."}
                </Alert>
            ))}
        </Stack>
    );
}

export function ResourceTasks({
    resource,
    compact = false,
}: {
    resource: ResourceRef;
    compact?: boolean;
}) {
    const query = useResourceTasks(resource);
    const tasks = query.data?.items || [];
    const current = tasks.filter((t) => t.blocks_resources);
    const params = new URLSearchParams({
        resource_kind: resource.kind,
        resource_scope: resource.scope,
        resource_id: resource.id,
    });
    return (
        <Box sx={{ my: compact ? 0 : 2 }}>
            {current.map((task) => (
                <Typography key={task.id} variant="body2">
                    <TaskLink task={task} />
                    {task.outcome_uncertain
                        ? " · Manual inspection required"
                        : " · Conflicting changes are disabled"}
                </Typography>
            ))}
            {query.isError && (
                <Typography variant="body2" color="warning.main">
                    Task status temporarily unavailable.
                </Typography>
            )}
            <Link
                component={RouterLink}
                to={`/tasks/?${params}`}
                variant="body2"
            >
                Task history
            </Link>
        </Box>
    );
}

export function Tasks() {
    const [params, setParams] = useSearchParams();
    const scopeParam = params.get("resource_scope") || "";
    const resourceParam = params.get("resource_id") || "";
    const [scope, setScope] = useState(scopeParam);
    const [resourceID, setResourceID] = useState(resourceParam);
    useEffect(() => setScope(scopeParam), [scopeParam]);
    useEffect(() => setResourceID(resourceParam), [resourceParam]);
    const applyResources = () =>
        setParams((old) => {
            const next = new URLSearchParams(old);
            next.delete("before");
            for (const [name, value] of [
                ["resource_scope", scope],
                ["resource_id", resourceID],
            ]) {
                if (value) next.set(name, value);
                else next.delete(name);
            }
            return next;
        });
    const requestParams = new URLSearchParams(params);
    requestParams.set("limit", "25");
    const query = useTaskPage(requestParams);
    const change = (name: string, value: string) =>
        setParams((old) => {
            const next = new URLSearchParams(old);
            next.delete("before");
            if (value) next.set(name, value);
            else next.delete(name);
            return next;
        });
    return (
        <>
            <PageTitle title="Tasks" />
            <Typography sx={{ mb: 2 }} color="text.secondary">
                Accepted operations and their saved execution history. Leaving a
                page does not cancel work.
            </Typography>
            <Stack
                component="form"
                onSubmit={(e) => {
                    e.preventDefault();
                    applyResources();
                }}
                direction={{ xs: "column", sm: "row" }}
                spacing={2}
                sx={{ mb: 3 }}
            >
                <TextField
                    select
                    label="Task state"
                    value={params.get("state") || ""}
                    onChange={(e) => change("state", e.target.value)}
                    sx={{ minWidth: 160 }}
                >
                    <MenuItem value="">All states</MenuItem>
                    {[
                        "queued",
                        "running",
                        "succeeded",
                        "failed",
                        "cancelled",
                        "interrupted",
                    ].map((state) => (
                        <MenuItem key={state} value={state}>
                            {state}
                        </MenuItem>
                    ))}
                </TextField>
                <TextField
                    select
                    label="Resource type"
                    value={params.get("resource_kind") || ""}
                    onChange={(e) => change("resource_kind", e.target.value)}
                    sx={{ minWidth: 160 }}
                >
                    <MenuItem value="">All resources</MenuItem>
                    <MenuItem value="machine">Machine</MenuItem>
                    <MenuItem value="volume">Volume</MenuItem>
                </TextField>
                <TextField
                    label="Resource scope"
                    value={scope}
                    onChange={(e) => setScope(e.target.value)}
                />
                <TextField
                    label="Resource identifier"
                    value={resourceID}
                    onChange={(e) => setResourceID(e.target.value)}
                />
                <Button type="submit" variant="outlined">
                    Apply resource filters
                </Button>
            </Stack>
            <QueryState query={query}>
                {(page) => (
                    <>
                        <Box sx={{ overflowX: "auto" }}>
                            <Table aria-label="Task history">
                                <TableHead>
                                    <TableRow>
                                        {[
                                            "Operation",
                                            "Resources",
                                            "State",
                                            "Last stage",
                                            "Accepted",
                                            "Finished",
                                        ].map((label) => (
                                            <TableCell key={label}>
                                                {label}
                                            </TableCell>
                                        ))}
                                    </TableRow>
                                </TableHead>
                                <TableBody>
                                    {(page.items || []).map((task) => (
                                        <TableRow key={task.id}>
                                            <TableCell>
                                                <Link
                                                    component={RouterLink}
                                                    to={taskURL(task.id)}
                                                >
                                                    {taskLabel(task.kind)}
                                                </Link>
                                            </TableCell>
                                            <TableCell>
                                                {(task.resources || [])
                                                    .map(
                                                        (r) =>
                                                            `${r.scope}/${r.id}`,
                                                    )
                                                    .join(", ")}
                                            </TableCell>
                                            <TableCell>
                                                <Chip
                                                    size="small"
                                                    label={task.state}
                                                />
                                                {task.outcome_uncertain && (
                                                    <Typography
                                                        variant="caption"
                                                        component="div"
                                                    >
                                                        Outcome uncertain
                                                    </Typography>
                                                )}
                                            </TableCell>
                                            <TableCell>
                                                {task.stages?.length
                                                    ? stageLabel(
                                                          task.stages[
                                                              task.stages
                                                                  .length - 1
                                                          ].name,
                                                      )
                                                    : task.state === "queued"
                                                      ? "Waiting to execute"
                                                      : "Not started"}
                                            </TableCell>
                                            <TableCell>
                                                {time(task.created_at)}
                                            </TableCell>
                                            <TableCell>
                                                {time(task.finished_at)}
                                            </TableCell>
                                        </TableRow>
                                    ))}
                                </TableBody>
                            </Table>
                        </Box>
                        {!page.items?.length && (
                            <Typography sx={{ my: 3 }}>
                                No tasks match these filters.
                            </Typography>
                        )}
                        <Stack direction="row" spacing={2} sx={{ mt: 2 }}>
                            {params.has("before") && (
                                <Button onClick={() => change("before", "")}>
                                    Newest tasks
                                </Button>
                            )}
                            {page.next && (
                                <Button
                                    onClick={() => change("before", page.next!)}
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

function ResourceLink({
    resource,
    deleted,
    available = true,
}: {
    resource: ResourceRef;
    deleted: boolean;
    available?: boolean;
}) {
    const label = `${resource.scope}/${resource.id}`;
    if (resource.kind === "machine-request")
        return <span>Requested machine: {resource.id}</span>;
    if (resource.kind === "cloud")
        return (
            <Link
                component={RouterLink}
                to={`/machines/?cloud=${encodeURIComponent(resource.scope)}`}
            >
                Cloud {resource.scope}
            </Link>
        );
    if (resource.kind !== "machine" && resource.kind !== "volume")
        return <span>{label}</span>;
    if (!available && !deleted)
        return <span>{label} (existence not confirmed)</span>;
    if (deleted) return <span>{label} (deleted)</span>;
    return (
        <Link
            component={RouterLink}
            to={
                resource.kind === "machine"
                    ? machineURL(resource.scope, resource.id)
                    : `/volumes/?node=${encodeURIComponent(resource.scope)}`
            }
        >
            {label}
        </Link>
    );
}
export function TransferStatus({
    transfer,
    running,
}: {
    transfer: NonNullable<NonNullable<Task["stages"]>[number]["transfer"]>;
    running: boolean;
}) {
    const stale = Date.now() - new Date(transfer.updated_at).getTime() > 15000;
    const total = transfer.total_bytes || 0;
    const percent = total
        ? Math.min(100, (100 * transfer.bytes) / total)
        : undefined;
    const eta = transfer.remaining_seconds;
    return (
        <Box sx={{ my: 1, minWidth: 220 }}>
            <LinearProgress
                aria-label="Image transfer"
                variant={
                    percent === undefined ? "indeterminate" : "determinate"
                }
                value={percent}
            />
            <Typography variant="body2">
                {bytes(transfer.bytes)}
                {total ? ` / ${bytes(total)} · ${percent!.toFixed(1)}%` : ""}
            </Typography>
            <Typography variant="caption">
                {running && !stale
                    ? `${bytes(transfer.bytes_per_second)}/s${eta === undefined ? "" : ` · About ${eta < 60 ? `${eta}s` : `${Math.ceil(eta / 60)} min`} remaining in transfer`}`
                    : running
                      ? "Waiting for a progress update…"
                      : "Last recorded transfer sample"}
            </Typography>
        </Box>
    );
}

export function TaskDetail() {
    const { id = "" } = useParams();
    const query = useTask(id);
    const queries = useQueryClient();
    const [confirm, setConfirm] = useState<"cancel" | "release">();
    const [reason, setReason] = useState("");
    const [error, setError] = useState<unknown>();
    const previous = useRef<string>("");
    useEffect(() => {
        const task = query.data;
        if (task && terminal(task) && previous.current !== task.state) {
            void queries.invalidateQueries({
                predicate: (q) =>
                    !String(q.queryKey[0]).startsWith("/api/v1/tasks"),
            });
        }
        previous.current = task?.state || "";
    }, [query.data, queries]);
    return (
        <>
            <PageTitle
                title="Task details"
                action={
                    <Button component={RouterLink} to="/tasks/">
                        All tasks
                    </Button>
                }
            />
            <QueryState query={query}>
                {(task) => (
                    <Stack spacing={3}>
                        <Box>
                            <Typography variant="h2">
                                {taskLabel(task.kind)}
                            </Typography>
                            <Chip sx={{ mt: 1 }} label={task.state} />
                            <Typography sx={{ mt: 1 }}>
                                {task.state === "queued"
                                    ? "Accepted and waiting to execute. You can leave this page."
                                    : task.state === "running"
                                      ? "Operation in progress. You can leave this page and return later."
                                      : task.state === "succeeded"
                                        ? "Command completed. Resource state is refreshed from the backend."
                                        : task.state === "cancelled"
                                          ? "Cancelled before execution. No backend action was started."
                                          : task.state === "interrupted"
                                            ? "Execution was interrupted. The last backend action may have completed or may still be running. No automatic retry was made."
                                            : "Operation failed. Review the diagnostic and any confirmed changes below."}
                            </Typography>
                        </Box>
                        {task.outcome_uncertain && (
                            <Alert severity="warning">
                                Backend outcome is uncertain. Inspect resources
                                manually before attempting recovery. Closing
                                this page does not stop backend activity.
                            </Alert>
                        )}
                        {task.blocks_resources && terminal(task) && (
                            <Alert severity="warning">
                                Affected resources remain blocked until an
                                operator confirms manual inspection.
                            </Alert>
                        )}
                        {task.error && (
                            <Alert severity="error">
                                <Typography>
                                    {task.state === "interrupted"
                                        ? "Interruption details"
                                        : "Failure diagnostics"}
                                </Typography>
                                <Box
                                    component="pre"
                                    sx={{
                                        whiteSpace: "pre-wrap",
                                        overflowWrap: "anywhere",
                                        fontSize: 13,
                                    }}
                                >
                                    {task.error.message}
                                </Box>
                                <Typography variant="body2">
                                    {task.error.backend} ·{" "}
                                    {task.error.operation}{" "}
                                    {task.error.code &&
                                        `· Code ${task.error.code}`}{" "}
                                    {task.error.domain &&
                                        `· Domain ${task.error.domain}`}
                                </Typography>
                                <Copy
                                    label="Copy sanitized diagnostics"
                                    text={JSON.stringify(
                                        { task_id: task.id, error: task.error },
                                        null,
                                        2,
                                    )}
                                />
                            </Alert>
                        )}
                        <Box component="dl" className="details">
                            <dt>Task ID</dt>
                            <dd className="mono">
                                {task.id}
                                <Copy label="Copy task ID" text={task.id} />
                            </dd>
                            <dt>Initiated by</dt>
                            <dd>{task.initiator.name || task.initiator.id}</dd>
                            <dt>Accepted</dt>
                            <dd>{time(task.created_at)}</dd>
                            <dt>Started</dt>
                            <dd>{time(task.started_at)}</dd>
                            <dt>Finished / interruption detected</dt>
                            <dd>{time(task.finished_at)}</dd>
                            <dt>Last successful stage</dt>
                            <dd>
                                {stageLabel(
                                    [...(task.stages || [])]
                                        .reverse()
                                        .find((s) => s.state === "succeeded")
                                        ?.name || "None",
                                )}
                            </dd>
                        </Box>
                        <Box>
                            <Typography variant="h2" sx={{ mb: 2 }}>
                                Execution timeline
                            </Typography>
                            {(task.transitions || []).map(
                                (transition, index) => (
                                    <Typography key={index} sx={{ mb: 1 }}>
                                        {time(transition.at)} ·{" "}
                                        {transition.state}
                                        {transition.reason &&
                                            ` · ${transition.reason}`}
                                    </Typography>
                                ),
                            )}
                            <Box sx={{ overflowX: "auto" }}>
                                <Table aria-label="Execution stages">
                                    <TableHead>
                                        <TableRow>
                                            <TableCell>Stage</TableCell>
                                            <TableCell>State</TableCell>
                                            <TableCell>Started</TableCell>
                                            <TableCell>Finished</TableCell>
                                        </TableRow>
                                    </TableHead>
                                    <TableBody>
                                        {(task.stages || []).map(
                                            (stage, index) => (
                                                <TableRow key={index}>
                                                    <TableCell>
                                                        {stageLabel(stage.name)}
                                                        {stage.transfer && (
                                                            <TransferStatus
                                                                transfer={
                                                                    stage.transfer
                                                                }
                                                                running={
                                                                    task.state ===
                                                                        "running" &&
                                                                    stage.state ===
                                                                        "running"
                                                                }
                                                            />
                                                        )}
                                                        {stage.resource && (
                                                            <Typography
                                                                variant="caption"
                                                                component="div"
                                                                sx={{
                                                                    overflowWrap:
                                                                        "anywhere",
                                                                    maxWidth: 440,
                                                                }}
                                                            >
                                                                {
                                                                    stage
                                                                        .resource
                                                                        .scope
                                                                }
                                                                /
                                                                {
                                                                    stage
                                                                        .resource
                                                                        .id
                                                                }
                                                            </Typography>
                                                        )}
                                                        {stage.error && (
                                                            <Typography
                                                                color="error.main"
                                                                sx={{
                                                                    whiteSpace:
                                                                        "pre-wrap",
                                                                }}
                                                            >
                                                                {
                                                                    stage.error
                                                                        .message
                                                                }
                                                            </Typography>
                                                        )}
                                                    </TableCell>
                                                    <TableCell>
                                                        {stage.state ===
                                                            "running" &&
                                                        task.state ===
                                                            "interrupted"
                                                            ? "In progress when interrupted; outcome unknown"
                                                            : stage.state}
                                                    </TableCell>
                                                    <TableCell>
                                                        {time(stage.started_at)}
                                                    </TableCell>
                                                    <TableCell>
                                                        {time(
                                                            stage.finished_at,
                                                        )}
                                                    </TableCell>
                                                </TableRow>
                                            ),
                                        )}
                                    </TableBody>
                                </Table>
                            </Box>
                            {!task.stages?.length && (
                                <Typography sx={{ mt: 2 }}>
                                    No execution stage has started.
                                </Typography>
                            )}
                        </Box>
                        <Box>
                            <Typography variant="h2" sx={{ mb: 2 }}>
                                Confirmed changes
                            </Typography>
                            {(task.effects || []).map((effect, index) => (
                                <Typography key={index}>
                                    {time(effect.at)} ·{" "}
                                    {stageLabel(effect.action)} ·{" "}
                                    <ResourceLink
                                        resource={effect.resource}
                                        deleted={effect.action === "deleted"}
                                    />
                                </Typography>
                            ))}
                            {!task.effects?.length && (
                                <Typography>
                                    No resource changes have been confirmed.
                                </Typography>
                            )}
                        </Box>
                        <Box>
                            <Typography variant="h2" sx={{ mb: 2 }}>
                                Affected resources
                            </Typography>
                            {(task.resources || []).map((resource, index) => (
                                <Typography key={index}>
                                    <ResourceLink
                                        resource={resource}
                                        available={
                                            task.state === "succeeded" ||
                                            !!task.effects?.some(
                                                (e) =>
                                                    [
                                                        "created",
                                                        "configuration-defined",
                                                        "configuration-written",
                                                    ].includes(e.action) &&
                                                    e.resource.kind ===
                                                        resource.kind &&
                                                    e.resource.scope ===
                                                        resource.scope &&
                                                    e.resource.id ===
                                                        resource.id,
                                            )
                                        }
                                        deleted={
                                            !!task.effects?.some(
                                                (e) =>
                                                    e.action === "deleted" &&
                                                    e.resource.kind ===
                                                        resource.kind &&
                                                    e.resource.scope ===
                                                        resource.scope &&
                                                    e.resource.id ===
                                                        resource.id,
                                            )
                                        }
                                    />
                                </Typography>
                            ))}
                        </Box>
                        {task.lock_release && (
                            <Alert severity="info">
                                Resource locks released by{" "}
                                {task.lock_release.by.name ||
                                    task.lock_release.by.id}{" "}
                                at {time(task.lock_release.at)}:{" "}
                                {task.lock_release.reason}. The original task
                                outcome remains unchanged.
                            </Alert>
                        )}
                        <ErrorMessage error={error} />
                        <Stack direction="row" spacing={2}>
                            {task.state === "queued" && (
                                <Button onClick={() => setConfirm("cancel")}>
                                    Cancel queued task
                                </Button>
                            )}
                            {task.blocks_resources && terminal(task) && (
                                <Button
                                    color="warning"
                                    onClick={() => setConfirm("release")}
                                >
                                    Release resource locks…
                                </Button>
                            )}
                        </Stack>
                        {confirm && (
                            <Confirm
                                title={
                                    confirm === "cancel"
                                        ? "Cancel queued task?"
                                        : "Release resource locks?"
                                }
                                action={
                                    confirm === "cancel"
                                        ? "Cancel task"
                                        : "Confirm inspection and release"
                                }
                                onClose={() => setConfirm(undefined)}
                                run={async () => {
                                    try {
                                        await request(
                                            `/api/v1/tasks/${encodeURIComponent(id)}/${confirm}`,
                                            "POST",
                                            confirm === "release"
                                                ? { reason }
                                                : undefined,
                                        );
                                        await queries.invalidateQueries();
                                    } catch (e) {
                                        setError(e);
                                        throw e;
                                    }
                                }}
                                extra={undefined}
                            >
                                {confirm === "cancel" ? (
                                    "Cancellation is possible only before execution starts. Running operations cannot be cancelled."
                                ) : (
                                    <>
                                        <span>
                                            Confirm that you inspected the
                                            affected backend resources and that
                                            further mutations are safe. This
                                            does not stop, retry or roll back
                                            the original operation.
                                        </span>
                                        <TextField
                                            sx={{ mt: 2 }}
                                            fullWidth
                                            required
                                            label="Inspection reason"
                                            value={reason}
                                            onChange={(e) =>
                                                setReason(e.target.value)
                                            }
                                            slotProps={{
                                                htmlInput: {
                                                    minLength: 3,
                                                    maxLength: 500,
                                                },
                                            }}
                                        />
                                    </>
                                )}
                            </Confirm>
                        )}
                    </Stack>
                )}
            </QueryState>
        </>
    );
}
