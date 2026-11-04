import type { components } from "../api/schema";
import {
    useCallback,
    useEffect,
    useRef,
    useState,
    forwardRef,
    type ReactNode,
} from "react";
import {
    Alert,
    Box,
    MenuItem,
    Stack,
    TextField,
    Typography,
} from "@mui/material";
import { useQuery } from "@tanstack/react-query";
import {
    Link,
    NavLink,
    Navigate,
    useLocation,
    useNavigate,
    type LinkProps,
    type NavLinkProps,
    type NavigateProps,
    type To,
} from "react-router";
import { useMe } from "./Auth";
import { request as send, onMutationCompleted } from "../api/client";
import {
    namespaceSelection,
    namespaceURL,
    workloadAPI,
    workloadPage,
} from "../api/context";
import { QueryState, useDirty } from "../components/common";

export type Namespace = components["schemas"]["Namespace"];
export function useWorkspace() {
    const route = useLocation();
    const principal = useMe()?.id;
    const selection = namespaceSelection(route.search);
    const namespace = selection.kind === "selected" ? selection.id : undefined;
    return { selection, namespace, principal };
}

// Every request closes over the rendered workspace before its first await.
export function useRequest() {
    const { namespace, principal } = useWorkspace();
    return useCallback(
        <T,>(
            path: string,
            method = "GET",
            body?: unknown,
            signal?: AbortSignal,
        ) => send<T>(path, method, body, signal, { namespace, principal }),
        [namespace, principal],
    );
}
export function useResourceKey(path: string, ...extra: unknown[]) {
    const { namespace, principal } = useWorkspace();
    return [
        path.startsWith("/api/v1/administration/")
            ? "administration"
            : workloadAPI(path)
              ? "workload"
              : "self",
        principal,
        workloadAPI(path) ? namespace : undefined,
        path,
        ...extra,
    ];
}
function contextualTo(to: To, namespace?: string): To {
    if (typeof to === "string") return namespaceURL(to, namespace);
    const full = `${to.pathname || ""}${to.search || ""}${to.hash || ""}`;
    const scoped = namespaceURL(full, namespace);
    return scoped === full ? to : scoped;
}
export const WorkspaceLink = forwardRef<HTMLAnchorElement, LinkProps>(
    function WorkspaceLink({ to, ...props }, ref) {
        const { namespace } = useWorkspace();
        return <Link {...props} ref={ref} to={contextualTo(to, namespace)} />;
    },
);
export const WorkspaceNavLink = forwardRef<HTMLAnchorElement, NavLinkProps>(
    function WorkspaceNavLink({ to, ...props }, ref) {
        const { namespace } = useWorkspace();
        return (
            <NavLink {...props} ref={ref} to={contextualTo(to, namespace)} />
        );
    },
);
export function WorkspaceNavigate({ to, ...props }: NavigateProps) {
    const { namespace } = useWorkspace();
    return <Navigate {...props} to={contextualTo(to, namespace)} />;
}
export function useNamespaces() {
    const principal = useMe()?.id;
    return useQuery({
        queryKey: ["bootstrap", principal, "namespaces"],
        queryFn: ({ signal }) =>
            send<{ items: Namespace[] }>(
                "/api/v1/namespaces",
                "GET",
                undefined,
                signal,
            ),
        refetchInterval: false,
        refetchOnWindowFocus: false,
    });
}
export function NamespacePicker() {
    const route = useLocation();
    const navigate = useNavigate();
    const { namespace } = useWorkspace();
    const query = useNamespaces();
    return (
        <TextField
            select
            label="Namespace"
            value={
                query.data?.items.some((n) => n.id === namespace)
                    ? namespace
                    : ""
            }
            disabled={!query.data}
            sx={{ minWidth: 170 }}
            onChange={(event) => {
                const search = new URLSearchParams(
                    query.data?.items.some((n) => n.id === namespace)
                        ? ""
                        : route.search,
                );
                search.set("namespace", event.target.value);
                // Resource and task details belong to the old context. A switch returns
                // to their list; the router's dirty-form blocker still controls leaving.
                const root = route.pathname.split("/")[1];
                const path = workloadPage(route.pathname)
                    ? query.data?.items.some((n) => n.id === namespace)
                        ? `/${root}/`
                        : route.pathname
                    : "/machines/";
                search.delete("before");
                search.delete("cloud");
                navigate({ pathname: path, search: `?${search}` });
            }}
        >
            <MenuItem value="" disabled>
                Choose namespace
            </MenuItem>
            {query.data?.items.map((n) => (
                <MenuItem key={n.id} value={n.id}>
                    {n.name}
                </MenuItem>
            ))}
        </TextField>
    );
}
export function WorkspaceBoundary({ children }: { children: ReactNode }) {
    const { selection, namespace } = useWorkspace();
    const namespaces = useNamespaces();
    return (
        <QueryState query={namespaces}>
            {(data) => {
                if (selection.kind === "missing")
                    return (
                        <Stack spacing={2}>
                            <Typography variant="h1">
                                Choose a namespace
                            </Typography>
                            <Typography>
                                Select the workspace for machines, volumes, keys
                                and tasks.
                            </Typography>
                            {!data.items.length && (
                                <Alert severity="info">
                                    No namespaces are configured.
                                </Alert>
                            )}
                            <Box>
                                <NamespacePicker />
                            </Box>
                        </Stack>
                    );
                if (
                    selection.kind === "invalid" ||
                    !data.items.some((n) => n.id === namespace)
                )
                    return (
                        <Stack spacing={2}>
                            <Alert severity="error">
                                The namespace in this URL is invalid or
                                unavailable. Select a configured namespace to
                                continue.
                            </Alert>
                            <Box>
                                <NamespacePicker />
                            </Box>
                        </Stack>
                    );
                return (
                    <WorkspaceContent key={namespace}>
                        {children}
                    </WorkspaceContent>
                );
            }}
        </QueryState>
    );
}

function WorkspaceContent({ children }: { children: ReactNode }) {
    const { namespace, principal } = useWorkspace();
    const [dirty, setDirty] = useState(false);
    const form = useRef<HTMLFormElement | null>(null);
    const prompt = useDirty(dirty);
    useEffect(
        () =>
            onMutationCompleted((context) => {
                if (
                    context.namespace === namespace &&
                    context.principal === principal
                )
                    setDirty(false);
            }),
        [namespace, principal],
    );
    useEffect(() => {
        if (!dirty) return;
        const observer = new MutationObserver(() => {
            if (form.current && !form.current.isConnected) setDirty(false);
        });
        observer.observe(document.body, { childList: true, subtree: true });
        return () => observer.disconnect();
    }, [dirty]);
    return (
        <Box
            onChangeCapture={(event) => {
                const target = event.target;
                if (!(target instanceof HTMLElement)) return;
                const edited = target.closest("form");
                if (edited && !edited.hasAttribute("data-workload-filter")) {
                    form.current = edited;
                    setDirty(true);
                }
            }}
        >
            {prompt}
            {children}
        </Box>
    );
}
