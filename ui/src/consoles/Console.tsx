import { useEffect, useRef, useState } from "react";
import {
    Alert,
    Box,
    Button,
    FormControlLabel,
    Checkbox,
    Link,
    MenuItem,
    Stack,
    TextField,
    Typography,
} from "@mui/material";
import { Link as RouterLink, useParams } from "react-router";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import { machineURL, useMachine, vmPath, type VM } from "../api/hooks";
import { request } from "../api/client";
import { Confirm, Copy, ErrorMessage, PageTitle } from "../components/common";
import { useAppearance } from "../app/theme";

type LinkResponse = { url: string; expires_at: string };
export function Console({ standalone = false }: { standalone?: boolean }) {
    const { cloud = "", id = "", kind: pathKind } = useParams();
    const vm = useMachine(cloud, id, !standalone, false);
    const [kind, setKind] = useState(
        pathKind || (location.pathname.endsWith("/vnc/") ? "vnc" : "serial"),
    );
    const [link, setLink] = useState<LinkResponse>();
    const [error, setError] = useState<unknown>();
    const [takeover, setTakeover] = useState(false);
    const [shared, setShared] = useState(false);
    const [busy, setBusy] = useState(false);
    const [fragment] = useState(() =>
        standalone ? location.hash.slice(1) : "",
    );
    const [attempt, setAttempt] = useState(0);
    const currentKind = standalone ? pathKind! : kind;
    const credential = standalone
        ? fragment
        : link
          ? new URL(link.url, location.origin).hash.slice(1)
          : "";
    const issue = async (force = false) => {
        if (busy) return;
        setBusy(true);
        setError(undefined);
        try {
            setLink(
                await request<LinkResponse>(
                    `${vmPath(cloud, id)}/consoles/${currentKind}/links`,
                    "POST",
                    { force },
                ),
            );
            setAttempt((n) => n + 1);
        } catch (e) {
            setError(e);
        } finally {
            setBusy(false);
        }
    };
    return (
        <Box className="console-page">
            <Stack
                direction="row"
                spacing={2}
                sx={{
                    alignItems: "center",
                    justifyContent: "space-between",
                    mb: 2,
                    flexWrap: "wrap",
                }}
            >
                <Box>
                    <Typography className="wordmark" component="span">
                        Vmango
                    </Typography>
                    <Typography component="span" sx={{ ml: 2 }}>
                        {cloud}/{id}
                    </Typography>
                </Box>
                {!standalone && (
                    <Link component={RouterLink} to={machineURL(cloud, id)}>
                        Back to machine
                    </Link>
                )}
            </Stack>
            <Stack
                direction="row"
                spacing={2}
                sx={{ mb: 2, alignItems: "center", flexWrap: "wrap" }}
            >
                {!standalone && (
                    <TextField
                        select
                        label="Console"
                        value={kind}
                        onChange={(e) => {
                            setKind(e.target.value);
                            setLink(undefined);
                        }}
                        sx={{ width: 160 }}
                    >
                        <MenuItem value="serial">Serial</MenuItem>
                        {vm.data?.graphics === "vnc" && (
                            <MenuItem value="vnc">VNC</MenuItem>
                        )}
                    </TextField>
                )}
                <Button
                    variant="contained"
                    disabled={busy || (standalone && !credential)}
                    onClick={() =>
                        standalone ? setAttempt((n) => n + 1) : issue()
                    }
                >
                    {busy
                        ? "Connecting…"
                        : credential
                          ? "Reconnect"
                          : "Connect"}
                </Button>
                {!standalone && currentKind === "serial" && (
                    <Button disabled={busy} onClick={() => setTakeover(true)}>
                        Force takeover…
                    </Button>
                )}
                {!standalone && link && (
                    <FormControlLabel
                        control={
                            <Checkbox
                                checked={shared}
                                onChange={(e) => setShared(e.target.checked)}
                            />
                        }
                        label="Show reusable sharing link"
                    />
                )}
                {link && (
                    <Typography variant="body2" color="text.secondary">
                        Expires {new Date(link.expires_at).toLocaleString()}
                    </Typography>
                )}
            </Stack>
            <ErrorMessage error={error} />
            {standalone && !credential && (
                <Alert severity="error">
                    This console URL is missing its credential. Open the
                    complete sharing link.
                </Alert>
            )}
            {shared && link && (
                <Alert severity="warning" sx={{ mb: 2 }}>
                    Anyone with this link can access this console until expiry.
                    Logging out does not revoke it.
                    <Copy text={link.url} label="Copy console sharing link" />
                </Alert>
            )}
            {credential ? (
                currentKind === "vnc" ? (
                    <VNC
                        key={`${credential}/${attempt}`}
                        cloud={cloud}
                        id={id}
                        token={credential}
                    />
                ) : (
                    <Serial
                        key={`${credential}/${attempt}`}
                        cloud={cloud}
                        id={id}
                        token={credential}
                    />
                )
            ) : (
                <Typography color="text.secondary" sx={{ py: 5 }}>
                    Connect to open the console.
                </Typography>
            )}
            {takeover && (
                <Confirm
                    title={`Take over ${id} serial console?`}
                    action="Take over console"
                    onClose={() => setTakeover(false)}
                    run={() => issue(true)}
                >
                    Disconnect any existing serial client for {cloud}/{id}. The
                    issued link will permit takeover on every connection until
                    it expires.
                </Confirm>
            )}
        </Box>
    );
}

function VNC({
    cloud,
    id,
    token,
}: {
    cloud: string;
    id: string;
    token: string;
}) {
    const frame = useRef<HTMLIFrameElement>(null);
    const { mode } = useAppearance();
    const send = () =>
        frame.current?.contentWindow?.postMessage(
            { type: "vmango-console", cloud, vm: id, token, theme: mode },
            location.origin,
        );
    useEffect(() => {
        send();
        const ready = (e: MessageEvent) => {
            if (
                e.origin === location.origin &&
                e.source === frame.current?.contentWindow &&
                e.data?.type === "vmango-console-ready"
            )
                send();
        };
        window.addEventListener("message", ready);
        return () => window.removeEventListener("message", ready);
    }, [mode, cloud, id, token]);
    return (
        <iframe
            ref={frame}
            src="/vendor/novnc.html"
            title={`VNC console for ${cloud}/${id}`}
            onLoad={send}
            allow="fullscreen; clipboard-read; clipboard-write"
            className="vnc-frame"
        />
    );
}

function Serial({
    cloud,
    id,
    token,
}: {
    cloud: string;
    id: string;
    token: string;
}) {
    const screen = useRef<HTMLDivElement>(null);
    const term = useRef<Terminal>(null);
    const socket = useRef<WebSocket>(null);
    const { mode } = useAppearance();
    const [status, setStatus] = useState("Connecting…");
    const [paste, setPaste] = useState("");
    useEffect(() => {
        const terminal = new Terminal({
            cursorBlink: true,
            fontFamily: "ui-monospace,SFMono-Regular,Consolas,monospace",
            fontSize: 14,
            theme: terminalTheme(mode),
        });
        const fit = new FitAddon();
        terminal.loadAddon(fit);
        terminal.open(screen.current!);
        fit.fit();
        term.current = terminal;
        const endpoint = new URL(
            `/console/${encodeURIComponent(cloud)}/${encodeURIComponent(id)}/serial/ws`,
            location.origin,
        );
        endpoint.protocol = location.protocol === "https:" ? "wss:" : "ws:";
        const ws = new WebSocket(endpoint, ["binary", `vmango.token.${token}`]);
        socket.current = ws;
        ws.binaryType = "arraybuffer";
        ws.onopen = () => {
            setStatus("Connected");
            terminal.focus();
        };
        ws.onmessage = (e) => terminal.write(new Uint8Array(e.data));
        ws.onclose = () =>
            setStatus(
                "Disconnected. The console may be busy, the link may have expired, or the machine may have stopped.",
            );
        ws.onerror = () =>
            setStatus(
                "Connection failed. Check machine state and link expiry.",
            );
        const data = terminal.onData((text) => {
            if (ws.readyState === WebSocket.OPEN)
                ws.send(new TextEncoder().encode(text));
        });
        const resize = new ResizeObserver(() => {
            try {
                fit.fit();
            } catch {
                /* The terminal can be hidden while routing. */
            }
        });
        resize.observe(screen.current!);
        return () => {
            ws.onopen = ws.onmessage = ws.onclose = ws.onerror = null;
            ws.close();
            resize.disconnect();
            data.dispose();
            terminal.dispose();
        };
    }, [cloud, id, token]);
    useEffect(() => {
        if (term.current) term.current.options.theme = terminalTheme(mode);
    }, [mode]);
    return (
        <>
            <Typography role="status" variant="body2" sx={{ mb: 1 }}>
                {status}
            </Typography>
            <Box
                ref={screen}
                className="terminal"
                sx={{
                    border: 1,
                    borderColor: "divider",
                    bgcolor: mode === "dark" ? "#171b20" : "#ffffff",
                }}
            />
            <Stack
                direction={{ xs: "column", sm: "row" }}
                spacing={2}
                sx={{ mt: 2 }}
            >
                <Button
                    onClick={() =>
                        navigator.clipboard
                            .writeText(term.current?.getSelection() || "")
                            .catch(() =>
                                setStatus(
                                    "Clipboard unavailable. Use the browser copy action.",
                                ),
                            )
                    }
                >
                    Copy selection
                </Button>
                <TextField
                    label="Paste into console"
                    value={paste}
                    onChange={(e) => setPaste(e.target.value)}
                />
                <Button
                    disabled={
                        !paste || socket.current?.readyState !== WebSocket.OPEN
                    }
                    onClick={() => {
                        socket.current?.send(new TextEncoder().encode(paste));
                        setPaste("");
                        term.current?.focus();
                    }}
                >
                    Send text
                </Button>
            </Stack>
            <Typography variant="caption" color="text.secondary">
                Terminal resizing changes the local view; guest terminal
                dimensions follow the guest’s serial configuration.
            </Typography>
        </>
    );
}
function terminalTheme(mode: string) {
    return mode === "dark"
        ? {
              background: "#171b20",
              foreground: "#edf0f4",
              cursor: "#8cbcf0",
              selectionBackground: "#3e586f",
              black: "#171b20",
              red: "#ff9292",
              green: "#9ad997",
              yellow: "#eed68b",
              blue: "#8cbcf0",
              magenta: "#d5aff0",
              cyan: "#8ed9d7",
              white: "#edf0f4",
          }
        : {
              background: "#ffffff",
              foreground: "#252b32",
              cursor: "#245a91",
              selectionBackground: "#c7ddf1",
              black: "#252b32",
              red: "#a3272d",
              green: "#28652b",
              yellow: "#795300",
              blue: "#245a91",
              magenta: "#74358f",
              cyan: "#176668",
              white: "#626c77",
          };
}
