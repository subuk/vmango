import { useEffect, useRef, useState, type ReactNode } from "react";
import {
    Button,
    Dialog,
    DialogActions,
    DialogContent,
    DialogTitle,
    Stack,
} from "@mui/material";
import { APIError } from "../api/client";
import { ErrorMessage } from "./common";
import { useQueryClient } from "@tanstack/react-query";

export function OperationDialog({
    title,
    action,
    onClose,
    run,
    children,
}: {
    title: string;
    action: string;
    onClose: () => void;
    run: () => Promise<unknown>;
    children: (error: unknown) => ReactNode;
}) {
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState<unknown>();
    const form = useRef<HTMLFormElement>(null);
    const queries = useQueryClient();
    const uncertain = error instanceof APIError && error.uncertain;
    useEffect(() => {
        if (error instanceof APIError) {
            const location = error.problem.errors?.[0]?.location?.replace(
                /^body\.?/,
                "",
            );
            const input = location
                ? form.current?.elements.namedItem(location)
                : null;
            if (input instanceof HTMLElement) input.focus();
        }
    }, [error]);
    return (
        <Dialog
            open
            onClose={busy ? undefined : onClose}
            aria-labelledby="operation-title"
        >
            <form
                ref={form}
                onSubmit={async (e) => {
                    e.preventDefault();
                    if (busy || uncertain) return;
                    setBusy(true);
                    setError(undefined);
                    try {
                        await run();
                        await queries.invalidateQueries();
                        onClose();
                    } catch (e) {
                        setError(e);
                    } finally {
                        setBusy(false);
                    }
                }}
            >
                <DialogTitle id="operation-title">{title}</DialogTitle>
                <DialogContent>
                    <Stack spacing={2.5} sx={{ pt: 1 }}>
                        {children(error)}
                    </Stack>
                    <ErrorMessage error={error} />
                    {uncertain && (
                        <Button href={location.pathname + location.search}>
                            Reload resource state before retrying
                        </Button>
                    )}
                </DialogContent>
                <DialogActions>
                    <Button onClick={onClose} disabled={busy}>
                        Cancel
                    </Button>
                    <Button
                        type="submit"
                        variant="contained"
                        disabled={busy || uncertain}
                    >
                        {busy ? "Working…" : action}
                    </Button>
                </DialogActions>
            </form>
        </Dialog>
    );
}

export function fieldError(error: unknown, name: string) {
    const detail =
        error instanceof APIError
            ? error.problem.errors?.find(
                  (e) =>
                      e.location === `body.${name}` ||
                      e.location === `body[${name}]`,
              )
            : undefined;
    return { error: !!detail, helperText: detail?.message };
}
