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
    description,
    action,
}: {
    title: string;
    count?: number;
    description?: string;
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
            <Box sx={{ minWidth: 0 }}>
                <Typography variant="h1" sx={{ overflowWrap: "anywhere" }}>
                    {title}{" "}
                    {count !== undefined && (
                        <Typography
                            component="span"
                            color="text.secondary"
                            sx={{
                                ml: 1.5,
                                fontSize: 14,
                                fontWeight: 600,
                                bgcolor: "action.hover",
                                border: 1,
                                borderColor: "divider",
                                borderRadius: 1,
                                px: 1,
                                py: 0.25,
                                verticalAlign: "middle",
                            }}
                        >
                            {count}
                        </Typography>
                    )}
                </Typography>
                {description && (
                    <Typography
                        color="text.secondary"
                        sx={{ mt: 0.75, maxWidth: 720 }}
                    >
                        {description}
                    </Typography>
                )}
            </Box>
            {action && <Box className="page-actions">{action}</Box>}
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
    if (
        query.data === undefined ||
        (query.error instanceof APIError &&
            [403, 404].includes(query.error.status))
    )
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
    const tone =
        value === "running" || value === "active"
            ? "success"
            : value === "stopped"
              ? "neutral"
              : "warning";
    return (
        <Box component="span" className="state-badge" data-tone={tone}>
            <Box component="span" className="state-dot" aria-hidden />
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
