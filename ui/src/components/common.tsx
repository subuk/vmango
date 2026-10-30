import { useEffect, useState, type ReactNode } from "react";
import {
    Alert,
    Link,
    Box,
    Button,
    CircularProgress,
    Dialog,
    DialogActions,
    DialogContent,
    DialogTitle,
    FormControlLabel,
    Checkbox,
    Stack,
    Typography,
} from "@mui/material";
import type { UseQueryResult } from "@tanstack/react-query";
import { APIError } from "../api/client";
import { Link as RouterLink, useBlocker } from "react-router";

export const bytes = (n: number) => {
    const units = ["B", "KiB", "MiB", "GiB", "TiB"];
    let i = 0;
    while (n >= 1024 && i < units.length - 1) {
        n /= 1024;
        i++;
    }
    return `${new Intl.NumberFormat(undefined, { maximumFractionDigits: i ? 1 : 0 }).format(n)} ${units[i]}`;
};
export function PageTitle({
    title,
    count,
    action,
}: {
    title: string;
    count?: number;
    action?: ReactNode;
}) {
    return (
        <Stack
            direction="row"
            sx={{
                alignItems: "center",
                justifyContent: "space-between",
                gap: 2,
                flexWrap: "wrap",
                mb: 3,
            }}
        >
            <Typography variant="h1">
                {title}{" "}
                {count !== undefined && (
                    <Typography
                        component="span"
                        color="text.secondary"
                        sx={{ ml: 1.5, fontSize: 18 }}
                    >
                        {count}
                    </Typography>
                )}
            </Typography>
            {action}
        </Stack>
    );
}
export function ErrorMessage({ error }: { error: unknown }) {
    if (!error) return null;
    const e = error as Error;
    const p = error instanceof APIError ? error.problem : undefined;
    return (
        <Alert severity="error" sx={{ my: 2 }}>
            <Typography>{e.message}</Typography>
            {p?.errors?.map((d, i) => (
                <Typography key={i} variant="body2">
                    {d.location?.replace(/^body\.?/, "")}: {d.message}
                </Typography>
            ))}
            {p?.task_id && (
                <Link
                    component={RouterLink}
                    to={`/tasks/${encodeURIComponent(p.task_id)}/`}
                >
                    Inspect blocking task
                </Link>
            )}
        </Alert>
    );
}
export function QueryState<T>({
    query,
    children,
}: {
    query: UseQueryResult<T, Error>;
    children: (data: T) => ReactNode;
}) {
    if (query.isPending)
        return (
            <Stack
                direction="row"
                spacing={2}
                role="status"
                sx={{ alignItems: "center" }}
            >
                <CircularProgress size={20} />
                <Typography>Loading…</Typography>
            </Stack>
        );
    if (query.data === undefined)
        return (
            <>
                <ErrorMessage error={query.error} />
                <Button onClick={() => query.refetch()}>Retry</Button>
            </>
        );
    return (
        <>
            {query.isError && (
                <Alert severity="warning" sx={{ mb: 2 }}>
                    Refresh failed. Showing last-known data.{" "}
                    <Button size="small" onClick={() => query.refetch()}>
                        Retry
                    </Button>
                </Alert>
            )}
            {children(query.data)}
        </>
    );
}
export function Copy({ text, label }: { text: string; label: string }) {
    const [copied, setCopied] = useState(false);
    const [error, setError] = useState(false);
    useEffect(() => {
        if (copied) {
            const t = setTimeout(() => setCopied(false), 2000);
            return () => clearTimeout(t);
        }
    }, [copied]);
    return (
        <Button
            size="small"
            aria-label={label}
            onClick={async () => {
                try {
                    await navigator.clipboard.writeText(text);
                    setCopied(true);
                    setError(false);
                } catch {
                    setError(true);
                }
            }}
        >
            <span role="status">
                {copied ? "Copied" : error ? "Copy unavailable" : "Copy"}
            </span>
        </Button>
    );
}
export function State({ value }: { value: string }) {
    return (
        <Box
            component="span"
            sx={{ display: "inline-flex", gap: 1, alignItems: "center" }}
        >
            <Box
                component="span"
                aria-hidden
                sx={{
                    width: 7,
                    height: 7,
                    borderRadius: "50%",
                    bgcolor:
                        value === "running" || value === "active"
                            ? "success.main"
                            : value === "stopped"
                              ? "text.secondary"
                              : "warning.main",
                }}
            />
            {value}
        </Box>
    );
}

export function Confirm({
    title,
    children,
    action,
    run,
    onClose,
    extra,
}: {
    title: string;
    children: ReactNode;
    action: string;
    run: (checked: boolean) => Promise<unknown>;
    onClose: () => void;
    extra?: string;
}) {
    const [busy, setBusy] = useState(false);
    const [checked, setChecked] = useState(false);
    const [error, setError] = useState<unknown>();
    const uncertain = error instanceof APIError && error.uncertain;
    return (
        <Dialog
            open
            onClose={busy ? undefined : onClose}
            aria-labelledby="confirm-title"
        >
            <DialogTitle id="confirm-title">{title}</DialogTitle>
            <DialogContent>
                <Box>{children}</Box>
                {extra && (
                    <FormControlLabel
                        control={
                            <Checkbox
                                checked={checked}
                                onChange={(e) => setChecked(e.target.checked)}
                            />
                        }
                        label={extra}
                    />
                )}
                <ErrorMessage error={error} />
                {uncertain && (
                    <Alert severity="warning">
                        Submission status is unknown. Recover the original
                        submission using its saved idempotency key.
                    </Alert>
                )}
                {uncertain && (
                    <Button href={location.pathname + location.search}>
                        Reload resource state
                    </Button>
                )}
            </DialogContent>
            <DialogActions>
                <Button disabled={busy} onClick={onClose}>
                    Cancel
                </Button>
                <Button
                    color="error"
                    variant="contained"
                    disabled={
                        busy ||
                        (uncertain &&
                            !(error instanceof APIError && error.recover))
                    }
                    onClick={async () => {
                        if (busy) return;
                        const recovery =
                            error instanceof APIError
                                ? error.recover
                                : undefined;
                        setBusy(true);
                        setError(undefined);
                        try {
                            if (recovery) await recovery();
                            else await run(checked);
                            onClose();
                        } catch (e) {
                            setError(e);
                        } finally {
                            setBusy(false);
                        }
                    }}
                >
                    {busy
                        ? "Submitting…"
                        : error instanceof APIError && error.recover
                          ? "Recover submission"
                          : action}
                </Button>
            </DialogActions>
        </Dialog>
    );
}

export function useDirty(dirty: boolean) {
    const blocker = useBlocker(
        ({ currentLocation, nextLocation }) =>
            dirty &&
            (currentLocation.pathname !== nextLocation.pathname ||
                currentLocation.search !== nextLocation.search),
    );
    useEffect(() => {
        if (!dirty) return;
        const unload = (e: BeforeUnloadEvent) => {
            e.preventDefault();
            e.returnValue = "";
        };
        window.addEventListener("beforeunload", unload);
        return () => {
            window.removeEventListener("beforeunload", unload);
        };
    }, [dirty]);
    return blocker.state === "blocked" ? (
        <Dialog
            open
            onClose={() => blocker.reset()}
            aria-labelledby="leave-title"
        >
            <DialogTitle id="leave-title">Discard unsaved changes?</DialogTitle>
            <DialogContent>
                <Typography>
                    Your changes will be lost if you leave this page.
                </Typography>
            </DialogContent>
            <DialogActions>
                <Button autoFocus onClick={() => blocker.reset()}>
                    Stay on page
                </Button>
                <Button color="error" onClick={() => blocker.proceed()}>
                    Discard and leave
                </Button>
            </DialogActions>
        </Dialog>
    ) : null;
}
