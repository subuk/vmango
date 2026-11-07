import { useEffect, useRef, useState } from "react";
import {
    Alert,
    Box,
    Button,
    Checkbox,
    Chip,
    CircularProgress,
    Container,
    Divider,
    FormControlLabel,
    LinearProgress,
    MenuItem,
    Paper,
    Stack,
    TextField,
    Typography,
} from "@mui/material";
import { connectSession, InstallError, readState, request } from "./client";
import {
    stageLabels,
    type APISettings,
    type InstallHost,
    type InstallState,
    type Stage,
} from "./types";

type Draft = { hosts: InstallHost[]; api: APISettings };
const grid = {
    display: "grid",
    gridTemplateColumns: { xs: "1fr", sm: "repeat(2, minmax(0, 1fr))" },
    gap: 2,
};
const resourceDefaults = {
    network: "default",
    subnet: "192.168.122.0/24",
    dhcpStart: "192.168.122.2",
    dhcpEnd: "192.168.122.254",
    pool: "default",
    poolPath: "/var/lib/libvirt/images",
};
const stages: Stage[] = ["hosts", "resources", "config"];

function errorText(error: unknown): string {
    if (error instanceof InstallError && error.status === 401) {
        return "This installer session is unavailable. Open the session URL printed by vmango install in your terminal.";
    }
    return error instanceof Error
        ? error.message
        : "The installer could not complete the request.";
}

function AccessFields({
    api,
    change,
    needsAdmin,
}: {
    api: APISettings;
    change: (value: APISettings) => void;
    needsAdmin: boolean;
}) {
    const [mode, setMode] = useState(
        api.tlsCertFile || api.tlsKeyFile
            ? "tls"
            : api.baseURL.startsWith("https://")
              ? "proxy"
              : "local",
    );
    const update = (key: keyof APISettings, value: string) =>
        change({ ...api, [key]: value });
    return (
        <Stack spacing={2}>
            <TextField
                select
                label="Server access"
                value={mode}
                onChange={(event) => {
                    const mode = event.target.value;
                    setMode(mode);
                    change({
                        ...api,
                        listen: mode === "tls" ? ":8443" : "127.0.0.1:8080",
                        baseURL:
                            mode === "local"
                                ? "http://127.0.0.1:8080"
                                : mode === "tls"
                                  ? "https://localhost:8443"
                                  : "https://vmango.example.com",
                        tlsCertFile: "",
                        tlsKeyFile: "",
                    });
                }}
            >
                <MenuItem value="local">Localhost or SSH tunnel</MenuItem>
                <MenuItem value="proxy">Existing HTTPS reverse proxy</MenuItem>
                <MenuItem value="tls">
                    Native HTTPS with your certificates
                </MenuItem>
            </TextField>
            <Box sx={grid}>
                <TextField
                    label="Server listen address"
                    value={api.listen}
                    onChange={(e) => update("listen", e.target.value)}
                    helperText="On the machine running Vmango"
                />
                <TextField
                    label="Public server URL"
                    value={api.baseURL}
                    onChange={(e) => update("baseURL", e.target.value)}
                />
            </Box>
            {mode === "tls" && (
                <Box sx={grid}>
                    <TextField
                        label="TLS certificate path"
                        value={api.tlsCertFile}
                        onChange={(e) => update("tlsCertFile", e.target.value)}
                        helperText="On the machine running Vmango"
                    />
                    <TextField
                        label="TLS private key path"
                        value={api.tlsKeyFile}
                        onChange={(e) => update("tlsKeyFile", e.target.value)}
                        helperText="Existing file; contents are not uploaded"
                    />
                </Box>
            )}
            {mode === "proxy" && (
                <Typography variant="body2" color="text.secondary">
                    Keep the server reachable only by your existing HTTPS proxy.
                    The installer does not configure the proxy.
                </Typography>
            )}
            {needsAdmin && (
                <Box sx={grid}>
                    <TextField
                        label="Initial administrator"
                        value={api.adminUser}
                        onChange={(e) => update("adminUser", e.target.value)}
                        autoComplete="username"
                    />
                    <TextField
                        label="Initial administrator password"
                        type="password"
                        value={api.password || ""}
                        onChange={(e) => update("password", e.target.value)}
                        autoComplete="new-password"
                        helperText="Hashed before configuration is saved"
                    />
                </Box>
            )}
        </Stack>
    );
}

function HostFields({
    host,
    observation,
    state,
    change,
    editableID,
}: {
    host: InstallHost;
    observation?: InstallHost;
    state: InstallState;
    change: (host: InstallHost) => void;
    editableID: boolean;
}) {
    const update = <K extends keyof InstallHost>(
        key: K,
        value: InstallHost[K],
    ) => change({ ...host, [key]: value });
    const cloud = state.clouds.find((entry) => entry.id === host.cloud);
    const zones = cloud?.zones || [];
    return (
        <Paper
            variant="outlined"
            component="section"
            sx={{ p: 2.5 }}
            aria-label={`Host ${host.id}`}
        >
            <Stack
                direction="row"
                sx={{
                    alignItems: "center",
                    justifyContent: "space-between",
                    flexWrap: "wrap",
                    gap: 1,
                }}
            >
                <FormControlLabel
                    control={
                        <Checkbox
                            checked={host.selected}
                            onChange={(e) =>
                                update("selected", e.target.checked)
                            }
                        />
                    }
                    label={
                        <Typography sx={{ fontWeight: 600 }}>
                            {host.id || "New host"}
                        </Typography>
                    }
                />
                <Stack direction="row" sx={{ gap: 1, alignItems: "center" }}>
                    <Chip
                        size="small"
                        variant="outlined"
                        label={host.local ? "Local Linux" : "SSH host"}
                    />
                    {observation?.status && (
                        <Chip
                            size="small"
                            label={observation.status}
                            color={
                                /fail|error|unavailable/i.test(
                                    observation.status,
                                )
                                    ? "error"
                                    : /ready|success|complete/i.test(
                                            observation.status,
                                        )
                                      ? "success"
                                      : "default"
                            }
                        />
                    )}
                </Stack>
            </Stack>
            {observation?.message && (
                <Alert
                    severity={
                        /fail|error|unavailable/i.test(observation.status || "")
                            ? "warning"
                            : "info"
                    }
                    sx={{ mt: 1 }}
                >
                    {observation.message}
                </Alert>
            )}
            {!host.selected ? (
                <Typography
                    variant="body2"
                    color="text.secondary"
                    sx={{ mt: 1 }}
                >
                    Not selected for this run. Any existing configuration entry
                    is preserved.
                </Typography>
            ) : (
                <Stack spacing={2} sx={{ mt: 2 }}>
                    <Box sx={grid}>
                        <TextField
                            label="Host ID"
                            value={host.id}
                            disabled={!editableID}
                            onChange={(e) => update("id", e.target.value)}
                            helperText="Stable name in the Vmango configuration"
                        />
                        {state.clouds.length > 0 ? (
                            <TextField
                                label="Cloud"
                                select
                                value={host.cloud}
                                onChange={(e) => {
                                    const next = state.clouds.find(
                                        (c) => c.id === e.target.value,
                                    );
                                    change({
                                        ...host,
                                        cloud: e.target.value,
                                        zone: next?.zones[0]?.id || "default",
                                    });
                                }}
                            >
                                {state.clouds.map((c) => (
                                    <MenuItem key={c.id} value={c.id}>
                                        {c.name || c.id}
                                    </MenuItem>
                                ))}
                            </TextField>
                        ) : (
                            <TextField
                                label="Cloud"
                                value={host.cloud}
                                onChange={(e) =>
                                    update("cloud", e.target.value)
                                }
                            />
                        )}
                        {zones.length > 0 ? (
                            <TextField
                                label="Zone"
                                select
                                value={host.zone}
                                onChange={(e) => update("zone", e.target.value)}
                            >
                                {zones.map((zone) => (
                                    <MenuItem key={zone.id} value={zone.id}>
                                        {zone.name || zone.id}
                                    </MenuItem>
                                ))}
                            </TextField>
                        ) : (
                            <TextField
                                label="Zone"
                                value={host.zone}
                                onChange={(e) => update("zone", e.target.value)}
                            />
                        )}
                        {!host.local && (
                            <TextField
                                label="SSH hostname or alias"
                                value={host.address}
                                disabled={!editableID}
                                onChange={(e) =>
                                    update("address", e.target.value)
                                }
                                helperText="Uses your controller's OpenSSH settings and keys"
                            />
                        )}
                        {!host.local && (
                            <TextField
                                label="SSH user"
                                value={host.user}
                                disabled={!editableID}
                                onChange={(e) => update("user", e.target.value)}
                                helperText="Also used for the server's ongoing libvirt access"
                            />
                        )}
                        {!host.local && (
                            <TextField
                                label="SSH port"
                                type="number"
                                value={host.port || ""}
                                disabled={!editableID}
                                onChange={(e) =>
                                    update(
                                        "port",
                                        e.target.value === ""
                                            ? 0
                                            : Number(e.target.value),
                                    )
                                }
                                slotProps={{
                                    htmlInput: { min: 1, max: 65535 },
                                }}
                                helperText="Leave empty to use SSH configuration"
                            />
                        )}
                    </Box>
                    <FormControlLabel
                        control={
                            <Checkbox
                                checked={host.sudo}
                                onChange={(e) =>
                                    update("sudo", e.target.checked)
                                }
                            />
                        }
                        label="Use noninteractive sudo for provisioning on this host (sudo -n)"
                    />
                    <Divider />
                    <Typography variant="body2" sx={{ fontWeight: 600 }}>
                        Network and storage on{" "}
                        {host.local
                            ? state.controller.hostname
                            : host.address || "this host"}
                    </Typography>
                    <Typography variant="body2" color="text.secondary">
                        Compatible existing resources are reused. Missing
                        resources are created only after review. Matching names
                        on different hosts do not create shared networking or
                        storage.
                    </Typography>
                    <Box sx={grid}>
                        <TextField
                            label="NAT network name"
                            value={host.network}
                            onChange={(e) => update("network", e.target.value)}
                        />
                        <TextField
                            label="Network subnet"
                            value={host.subnet}
                            onChange={(e) => update("subnet", e.target.value)}
                        />
                        <TextField
                            label="DHCP range start"
                            value={host.dhcpStart}
                            onChange={(e) =>
                                update("dhcpStart", e.target.value)
                            }
                        />
                        <TextField
                            label="DHCP range end"
                            value={host.dhcpEnd}
                            onChange={(e) => update("dhcpEnd", e.target.value)}
                        />
                        <TextField
                            label="Directory pool name"
                            value={host.pool}
                            onChange={(e) => update("pool", e.target.value)}
                        />
                        <TextField
                            label="Pool directory on this host"
                            value={host.poolPath}
                            onChange={(e) => update("poolPath", e.target.value)}
                        />
                    </Box>
                </Stack>
            )}
        </Paper>
    );
}

export function Installer() {
    const [state, setState] = useState<InstallState>();
    const [draft, setDraft] = useState<Draft>();
    const [dirty, setDirty] = useState(false);
    const dirtyRef = useRef(false);
    const [pending, setPending] = useState(false);
    const [error, setError] = useState("");
    const [connectionError, setConnectionError] = useState("");
    const [connected, setConnected] = useState(false);
    const [shared, setShared] = useState(resourceDefaults);
    const [sharedOpen, setSharedOpen] = useState(false);
    const originalIDs = useRef<Set<string> | null>(null);
    const alive = useRef(true);

    const acceptState = (next: InstallState) => {
        if (!alive.current) return;
        setState(next);
        originalIDs.current ??= new Set(next.hosts.map((host) => host.id));
        if (!dirtyRef.current)
            setDraft({ hosts: next.hosts, api: { ...next.api, password: "" } });
        setConnected(true);
    };
    useEffect(() => {
        alive.current = true;
        let loading = false;
        const refresh = async () => {
            if (loading) return;
            loading = true;
            try {
                await connectSession();
                acceptState(await readState());
                if (alive.current) {
                    setConnected(true);
                    setConnectionError("");
                }
            } catch (error) {
                if (alive.current) {
                    setConnectionError(errorText(error));
                    setConnected(false);
                }
            } finally {
                loading = false;
            }
        };
        void refresh();
        const timer = setInterval(() => void refresh(), 2000);
        return () => {
            alive.current = false;
            clearInterval(timer);
        };
    }, []);

    const edit = (next: Draft) => {
        setDraft(next);
        dirtyRef.current = true;
        setDirty(true);
        setError("");
    };
    const save = async (): Promise<InstallState> => {
        if (!state || !draft) throw new Error("The installer has not loaded.");
        if (dirtyRef.current) {
            await request("answers", { revision: state.revision, ...draft });
            dirtyRef.current = false;
            setDirty(false);
        }
        const current = await readState();
        acceptState(current);
        return current;
    };
    const run = async (
        action:
            | "save"
            | "inspect"
            | "discover"
            | "prepare"
            | "apply"
            | "cancel"
            | "resume",
        stage?: Stage,
    ) => {
        if (
            pending ||
            !state ||
            state.phase === "complete" ||
            state.phase === "completed"
        )
            return;
        setPending(true);
        setError("");
        try {
            let current = state;
            if (["save", "inspect", "discover", "prepare"].includes(action))
                current = await save();
            if (action === "apply") {
                if (dirtyRef.current || !current.plan)
                    throw new Error(
                        "Review a new plan after changing the settings.",
                    );
                await request("apply", {
                    revision: current.revision,
                    planID: current.plan.id,
                });
            } else if (action !== "save") {
                await request(
                    action,
                    action === "cancel"
                        ? {}
                        : {
                              revision: current.revision,
                              ...(stage ? { stage } : {}),
                          },
                );
            }
            acceptState(await readState());
        } catch (error) {
            setError(errorText(error));
        } finally {
            setPending(false);
        }
    };
    const addHost = (local: boolean) => {
        if (!draft || !state) return;
        let index = 1;
        while (draft.hosts.some((host) => host.id === `host-${index}`)) index++;
        const cloud = state.clouds[0];
        edit({
            ...draft,
            hosts: [
                ...draft.hosts,
                {
                    id: `host-${index}`,
                    cloud: cloud?.id || "default",
                    zone: cloud?.zones[0]?.id || "default",
                    address: "",
                    user: "",
                    port: 0,
                    local,
                    sudo: false,
                    selected: true,
                    ...shared,
                },
            ],
        });
    };

    if (!state || !draft)
        return (
            <Container maxWidth="sm" sx={{ py: 8 }}>
                <Stack spacing={3}>
                    <Typography variant="h1">Set up Vmango</Typography>
                    {connectionError ? (
                        <Alert severity="error">{connectionError}</Alert>
                    ) : (
                        <Stack
                            direction="row"
                            sx={{ gap: 2, alignItems: "center" }}
                        >
                            <CircularProgress size={24} />
                            <Typography>
                                Connecting to your installer session…
                            </Typography>
                        </Stack>
                    )}
                </Stack>
            </Container>
        );

    const busy = state.busy || pending;
    const selected = draft.hosts.filter((host) => host.selected);
    const complete = state.phase === "complete" || state.phase === "completed";
    return (
        <Container maxWidth="md" component="main" sx={{ py: { xs: 3, sm: 5 } }}>
            <Stack spacing={3}>
                <Box>
                    <Stack
                        direction="row"
                        sx={{
                            alignItems: "center",
                            justifyContent: "space-between",
                            gap: 2,
                        }}
                    >
                        <Typography className="wordmark">vmango</Typography>
                        <Chip label="Setup" size="small" variant="outlined" />
                    </Stack>
                    <Typography variant="h1" sx={{ mt: 3 }}>
                        Set up your virtualization hosts
                    </Typography>
                    <Typography color="text.secondary" sx={{ mt: 1 }}>
                        Prepare Linux hosts, review changes, and save the
                        configuration for your Vmango server.
                    </Typography>
                </Box>

                <Paper variant="outlined" sx={{ p: 2.5 }}>
                    <Stack spacing={1}>
                        <Typography variant="body2" color="text.secondary">
                            {state.exists
                                ? "Updating configuration"
                                : "Creating configuration"}{" "}
                            on {state.controller.hostname}
                        </Typography>
                        <Typography
                            component="code"
                            sx={{ overflowWrap: "anywhere" }}
                        >
                            {state.configPath}
                        </Typography>
                        <Typography variant="body2" color="text.secondary">
                            Controller: {state.controller.os} /{" "}
                            {state.controller.arch}
                            {state.controller.user
                                ? ` · User: ${state.controller.user}`
                                : ""}
                            . This wizard does not start or restart the Vmango
                            server.
                        </Typography>
                    </Stack>
                </Paper>

                {!connected && (
                    <Alert severity="warning">
                        Connection lost. Approved work continues in the
                        installer process. Reconnecting… {connectionError}
                    </Alert>
                )}
                {(error || state.error) && (
                    <Alert
                        severity="error"
                        onClose={error ? () => setError("") : undefined}
                    >
                        {error || state.error}
                    </Alert>
                )}
                {busy && (
                    <Box aria-live="polite">
                        <LinearProgress />
                        <Typography variant="body2" sx={{ mt: 1 }}>
                            Working — {state.phase}. You can close this page and
                            reconnect with the same session.
                        </Typography>
                    </Box>
                )}

                {complete && (
                    <Alert severity="success">
                        <Typography sx={{ fontWeight: 600 }}>
                            Configuration saved
                        </Typography>
                        <Typography variant="body2">
                            Start or restart Vmango using the configuration at{" "}
                            {state.configPath}. A running server has not
                            automatically loaded these changes.
                        </Typography>
                        <Typography variant="body2">
                            To make further changes, start a new installer
                            session.
                        </Typography>
                        {state.backupPath && (
                            <Typography
                                variant="body2"
                                sx={{ overflowWrap: "anywhere" }}
                            >
                                Backup: {state.backupPath}
                            </Typography>
                        )}
                    </Alert>
                )}

                <Box
                    component="fieldset"
                    disabled={busy || !connected || complete}
                    sx={{ border: 0, p: 0, m: 0, minWidth: 0 }}
                >
                    <Stack spacing={3}>
                        <Box>
                            <Typography variant="h2">
                                1. Choose hosts
                            </Typography>
                            <Typography
                                color="text.secondary"
                                variant="body2"
                                sx={{ mt: 0.5 }}
                            >
                                Select the hosts to prepare. Existing entries
                                remain in your configuration when unselected.
                            </Typography>
                        </Box>
                        {draft.hosts.map((host, index) => (
                            <HostFields
                                key={index}
                                host={host}
                                observation={state.hosts.find(
                                    (h) => h.id === host.id,
                                )}
                                state={state}
                                editableID={!originalIDs.current?.has(host.id)}
                                change={(next) =>
                                    edit({
                                        ...draft,
                                        hosts: draft.hosts.map((h, i) =>
                                            i === index ? next : h,
                                        ),
                                    })
                                }
                            />
                        ))}
                        {draft.hosts.length === 0 && (
                            <Paper variant="outlined" sx={{ p: 3 }}>
                                <Typography>
                                    No hosts yet. Add an SSH host
                                    {state.controller.os === "linux"
                                        ? " or this Linux machine"
                                        : " to get started"}
                                    .
                                </Typography>
                            </Paper>
                        )}
                        <Stack
                            direction="row"
                            sx={{ flexWrap: "wrap", gap: 1 }}
                        >
                            <Button
                                variant="outlined"
                                onClick={() => addHost(false)}
                            >
                                Add SSH host
                            </Button>
                            {state.controller.os === "linux" && (
                                <Button
                                    variant="outlined"
                                    onClick={() => addHost(true)}
                                    disabled={draft.hosts.some((h) => h.local)}
                                >
                                    Add this Linux host
                                </Button>
                            )}
                            <Button
                                onClick={() => setSharedOpen(!sharedOpen)}
                                aria-expanded={sharedOpen}
                            >
                                Shared resource defaults
                            </Button>
                        </Stack>
                        {sharedOpen && (
                            <Paper variant="outlined" sx={{ p: 2.5 }}>
                                <Stack spacing={2}>
                                    <Typography variant="h2">
                                        Shared resource defaults
                                    </Typography>
                                    <Typography
                                        variant="body2"
                                        color="text.secondary"
                                    >
                                        Used for new hosts. Apply explicitly to
                                        selected hosts to replace their network
                                        and storage fields; individual hosts can
                                        then be adjusted.
                                    </Typography>
                                    <Box sx={grid}>
                                        {(
                                            [
                                                [
                                                    "network",
                                                    "Default NAT network",
                                                ],
                                                ["subnet", "Default subnet"],
                                                [
                                                    "dhcpStart",
                                                    "Default DHCP start",
                                                ],
                                                ["dhcpEnd", "Default DHCP end"],
                                                ["pool", "Default pool"],
                                                [
                                                    "poolPath",
                                                    "Default pool directory",
                                                ],
                                            ] as const
                                        ).map(([key, label]) => (
                                            <TextField
                                                key={key}
                                                label={label}
                                                value={shared[key]}
                                                onChange={(e) =>
                                                    setShared({
                                                        ...shared,
                                                        [key]: e.target.value,
                                                    })
                                                }
                                            />
                                        ))}
                                    </Box>
                                    <Button
                                        variant="outlined"
                                        disabled={!selected.length}
                                        onClick={() =>
                                            edit({
                                                ...draft,
                                                hosts: draft.hosts.map(
                                                    (host) =>
                                                        host.selected
                                                            ? {
                                                                  ...host,
                                                                  ...shared,
                                                              }
                                                            : host,
                                                ),
                                            })
                                        }
                                    >
                                        Apply defaults to selected hosts
                                    </Button>
                                </Stack>
                            </Paper>
                        )}

                        <Paper variant="outlined" sx={{ p: 2.5 }}>
                            <Stack spacing={2}>
                                <Typography variant="h2">
                                    2. Configure server access
                                </Typography>
                                <Typography
                                    variant="body2"
                                    color="text.secondary"
                                >
                                    These settings apply to the Vmango server on{" "}
                                    {state.controller.hostname}, not to the
                                    selected hypervisors.
                                </Typography>
                                <AccessFields
                                    api={draft.api}
                                    change={(api) => edit({ ...draft, api })}
                                    needsAdmin={
                                        state.needsAdmin ?? !state.exists
                                    }
                                />
                            </Stack>
                        </Paper>
                        <Stack
                            direction="row"
                            sx={{
                                flexWrap: "wrap",
                                gap: 1,
                                alignItems: "center",
                            }}
                        >
                            <Button
                                variant="outlined"
                                onClick={() => void run("save")}
                                disabled={!dirty}
                            >
                                Save answers
                            </Button>
                            <Button
                                variant="contained"
                                onClick={() => void run("inspect")}
                                disabled={!selected.length}
                            >
                                Inspect selected hosts
                            </Button>
                            <Button
                                variant="outlined"
                                onClick={() => void run("discover")}
                                disabled={!selected.length}
                            >
                                Load existing resource settings
                            </Button>
                            {dirty && (
                                <Typography
                                    variant="body2"
                                    color="text.secondary"
                                >
                                    Unsaved answers. Any previous plan must be
                                    prepared again.
                                </Typography>
                            )}
                        </Stack>
                        <Typography variant="body2" color="text.secondary">
                            Loading existing settings replaces the selected
                            hosts' subnet, DHCP range and pool directory with
                            values read from libvirt. Missing resources keep
                            your current desired settings. No host resources are
                            changed.
                        </Typography>
                    </Stack>
                </Box>

                <Divider />
                <Box>
                    <Typography variant="h2">
                        3. Review and apply each stage
                    </Typography>
                    <Typography
                        variant="body2"
                        color="text.secondary"
                        sx={{ mt: 0.5 }}
                    >
                        Each stage prepares its own action list. Review and
                        approve it before anything changes.
                    </Typography>
                </Box>
                <Stack spacing={2}>
                    {stages.map((stage, index) => (
                        <Paper variant="outlined" key={stage} sx={{ p: 2.5 }}>
                            <Stack
                                direction={{ xs: "column", sm: "row" }}
                                sx={{
                                    justifyContent: "space-between",
                                    alignItems: { sm: "center" },
                                    gap: 2,
                                }}
                            >
                                <Box>
                                    <Typography sx={{ fontWeight: 600 }}>
                                        {index + 1}. {stageLabels[stage]}
                                    </Typography>
                                    <Typography
                                        variant="body2"
                                        color="text.secondary"
                                    >
                                        {stage === "hosts"
                                            ? "Packages, libvirt services, and host access through Richt."
                                            : stage === "resources"
                                              ? "Inspect and prepare concrete NAT network and directory pool changes."
                                              : "Review the final host selection and configuration changes."}
                                    </Typography>
                                </Box>
                                <Button
                                    variant="outlined"
                                    sx={{ flexShrink: 0 }}
                                    disabled={
                                        busy ||
                                        !connected ||
                                        !selected.length ||
                                        (state.allowedStages !== undefined &&
                                            !state.allowedStages.includes(
                                                stage,
                                            ))
                                    }
                                    onClick={() => void run("prepare", stage)}
                                >
                                    Review{" "}
                                    {stage === "hosts"
                                        ? "host changes"
                                        : stage === "resources"
                                          ? "resource changes"
                                          : "configuration"}
                                </Button>
                            </Stack>
                        </Paper>
                    ))}
                </Stack>

                {state.plan && (
                    <Paper
                        variant="outlined"
                        component="section"
                        aria-label="Prepared plan"
                        sx={{ p: 3, borderColor: "primary.main" }}
                    >
                        <Stack spacing={2}>
                            <Typography variant="h2">
                                Review: {stageLabels[state.plan.stage]}
                            </Typography>
                            <Typography variant="body2">
                                Selected hosts:{" "}
                                {selected.map((host) => host.id).join(", ") ||
                                    "None"}
                            </Typography>
                            {state.plan.summary?.length > 0 && (
                                <Box component="ul" sx={{ pl: 3, m: 0 }}>
                                    {state.plan.summary.map((line, index) => (
                                        <li key={index}>
                                            <Typography variant="body2">
                                                {line}
                                            </Typography>
                                        </li>
                                    ))}
                                </Box>
                            )}
                            {state.plan.output && (
                                <Box
                                    component="pre"
                                    aria-label="Plan output"
                                    sx={{
                                        m: 0,
                                        p: 2,
                                        bgcolor: "action.hover",
                                        whiteSpace: "pre-wrap",
                                        overflowWrap: "anywhere",
                                        maxHeight: 400,
                                        overflow: "auto",
                                        fontSize: 12,
                                    }}
                                >
                                    {state.plan.output}
                                </Box>
                            )}
                            {dirty && (
                                <Alert severity="warning">
                                    Answers have changed. Prepare and review a
                                    new plan before applying.
                                </Alert>
                            )}
                            <Typography variant="body2" color="text.secondary">
                                Approving applies only this reviewed plan.
                                Changes already completed on hosts are not
                                automatically rolled back.
                            </Typography>
                            <Button
                                variant="contained"
                                disabled={
                                    busy || dirty || !connected || complete
                                }
                                onClick={() => void run("apply")}
                            >
                                Approve and{" "}
                                {state.plan.stage === "config"
                                    ? "save configuration"
                                    : "apply changes"}
                            </Button>
                        </Stack>
                    </Paper>
                )}

                {(state.results?.length > 0 ||
                    state.logs?.length > 0 ||
                    state.canResume ||
                    state.busy) && (
                    <Paper variant="outlined" sx={{ p: 2.5 }}>
                        <Stack spacing={2}>
                            <Typography variant="h2">
                                Progress and results
                            </Typography>
                            <Stack spacing={1}>
                                {state.results?.map((result) => (
                                    <Box
                                        key={result.id}
                                        sx={{
                                            display: "flex",
                                            gap: 1,
                                            alignItems: "baseline",
                                            flexWrap: "wrap",
                                        }}
                                    >
                                        <Typography
                                            sx={{ fontWeight: 600 }}
                                            variant="body2"
                                        >
                                            {result.id}
                                        </Typography>
                                        <Chip
                                            size="small"
                                            label={result.state}
                                        />
                                        <Typography variant="body2">
                                            {result.message}
                                        </Typography>
                                    </Box>
                                ))}
                            </Stack>
                            {state.logs?.length > 0 && (
                                <Box
                                    component="pre"
                                    aria-label="Execution output"
                                    sx={{
                                        m: 0,
                                        p: 2,
                                        bgcolor: "action.hover",
                                        whiteSpace: "pre-wrap",
                                        overflowWrap: "anywhere",
                                        maxHeight: 300,
                                        overflow: "auto",
                                        fontSize: 12,
                                    }}
                                >
                                    {state.logs.join("\n")}
                                </Box>
                            )}
                            <Stack direction="row" sx={{ gap: 1 }}>
                                {state.busy && (
                                    <Button
                                        color="warning"
                                        variant="outlined"
                                        disabled={pending || !connected}
                                        onClick={() => void run("cancel")}
                                    >
                                        Request cancellation
                                    </Button>
                                )}
                                {state.canResume && !state.busy && (
                                    <Button
                                        variant="outlined"
                                        disabled={
                                            pending || dirty || !connected
                                        }
                                        onClick={() => void run("resume")}
                                    >
                                        Resume saved work
                                    </Button>
                                )}
                            </Stack>
                        </Stack>
                    </Paper>
                )}
                <Typography variant="caption" color="text.secondary">
                    Keep the terminal process running until the installation
                    finishes. Closing this browser page does not stop approved
                    work.
                </Typography>
            </Stack>
        </Container>
    );
}
