import {
    createContext,
    useContext,
    useEffect,
    useState,
    type ReactNode,
} from "react";
import {
    Alert,
    Box,
    Button,
    CircularProgress,
    Link,
    Stack,
    TextField,
    Typography,
} from "@mui/material";
import { useQueryClient } from "@tanstack/react-query";
import {
    APIError,
    login,
    onLogout,
    publicRequest,
    refresh,
    request,
} from "../api/client";

export type Me = {
    id: string;
    name: string;
    email: string;
    version: string;
    links_title: string;
    links: { title: string; url: string; active: boolean }[];
};
const Context = createContext<Me | null>(null);
export const useMe = () => useContext(Context)!;
export function Auth({ children }: { children: ReactNode }) {
    const queries = useQueryClient();
    const [me, setMe] = useState<Me | null>(null);
    const [state, setState] = useState<"loading" | "login" | "error" | "ready">(
        "loading",
    );
    const [error, setError] = useState("");
    const bootstrap = async () => {
        setState("loading");
        try {
            await refresh();
            setMe(await request<Me>("/api/v1/me"));
            setState("ready");
        } catch (e) {
            if (
                e instanceof APIError &&
                (e.status === 401 || e.problem.code === "refresh_response_lost")
            ) {
                queries.clear();
                setMe(null);
                setState("login");
            } else {
                setError((e as Error).message);
                setState("error");
            }
        }
    };
    useEffect(() => {
        void bootstrap();
        return onLogout(() => {
            queries.clear();
            setMe(null);
            setState("login");
        });
    }, []);
    if (state === "loading")
        return (
            <Box className="auth-frame" role="status">
                <CircularProgress size={24} />
                <Typography>Restoring session…</Typography>
            </Box>
        );
    if (state === "error")
        return (
            <Box className="auth-frame">
                <Alert severity="error">{error}</Alert>
                <Button onClick={bootstrap}>Retry</Button>
            </Box>
        );
    if (state === "login")
        return (
            <Login
                onLogin={async () => {
                    queries.clear();
                    setMe(await request<Me>("/api/v1/me"));
                    setState("ready");
                }}
            />
        );
    return <Context.Provider value={me}>{children}</Context.Provider>;
}
function Login({ onLogin }: { onLogin: () => Promise<void> }) {
    const [config, setConfig] = useState<{
        password: boolean;
        oidc: boolean;
        oidc_title: string;
        oidc_login_url: string;
    }>();
    const [username, setUsername] = useState("");
    const [password, setPassword] = useState("");
    const [error, setError] = useState(() => {
        const reason = new URLSearchParams(location.search).get("auth_error");
        return reason === "oidc_unavailable"
            ? "Identity provider unavailable. Try signing in again."
            : reason
              ? "OpenID Connect sign-in failed. Try signing in again."
              : "";
    });
    const [busy, setBusy] = useState(false);
    const load = () =>
        publicRequest<typeof config>("/api/v1/auth/config")
            .then(setConfig)
            .catch((e) => setError(e.message));
    useEffect(() => {
        void load();
    }, []);
    return (
        <Box className="login">
            <Typography className="wordmark" component="div">
                Vmango
            </Typography>
            <Typography variant="h1" sx={{ mt: 3, mb: 3 }}>
                Sign in
            </Typography>
            {error && (
                <Alert severity="error" sx={{ mb: 2 }}>
                    {error}
                </Alert>
            )}
            {!config ? (
                <Button onClick={load}>Retry login configuration</Button>
            ) : (
                <Stack spacing={3}>
                    {config.password && (
                        <Box
                            component="form"
                            onSubmit={async (e) => {
                                e.preventDefault();
                                if (busy) return;
                                setBusy(true);
                                setError("");
                                try {
                                    await login(username, password);
                                    setPassword("");
                                    await onLogin();
                                } catch (e) {
                                    setError((e as Error).message);
                                } finally {
                                    setBusy(false);
                                }
                            }}
                        >
                            <Stack spacing={2.5}>
                                <TextField
                                    label="Username"
                                    autoComplete="username"
                                    value={username}
                                    onChange={(e) =>
                                        setUsername(e.target.value)
                                    }
                                    required
                                    autoFocus
                                />
                                <TextField
                                    label="Password"
                                    type="password"
                                    autoComplete="current-password"
                                    value={password}
                                    onChange={(e) =>
                                        setPassword(e.target.value)
                                    }
                                    required
                                />
                                <Button
                                    variant="contained"
                                    type="submit"
                                    disabled={busy}
                                >
                                    {busy ? "Signing in…" : "Sign in"}
                                </Button>
                            </Stack>
                        </Box>
                    )}
                    {config.oidc && (
                        <Button
                            component={Link}
                            href={`${config.oidc_login_url}?next=${encodeURIComponent(location.pathname + location.search)}`}
                            variant="outlined"
                        >
                            Continue with{" "}
                            {config.oidc_title || "OpenID Connect"}
                        </Button>
                    )}
                    {!config.password && !config.oidc && (
                        <Alert severity="warning">
                            No login method is configured. Contact the
                            administrator.
                        </Alert>
                    )}
                </Stack>
            )}
        </Box>
    );
}
