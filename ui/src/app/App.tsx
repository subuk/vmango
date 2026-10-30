import { lazy, Suspense, useState } from "react";
import {
    Alert,
    Box,
    Button,
    Drawer,
    Link,
    List,
    ListItemButton,
    ListItemText,
    Menu,
    MenuItem,
    TextField,
    Typography,
} from "@mui/material";
import {
    Link as RouterLink,
    NavLink,
    Navigate,
    Route,
    Routes,
} from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import { useAppearance } from "./theme";
import { useMe } from "./Auth";
import { TaskMonitor, Tasks, TaskDetail } from "../features/Tasks";
import { logout } from "../api/client";

const Machines = lazy(() =>
    import("../features/Lists").then((m) => ({ default: m.Machines })),
);
const Nodes = lazy(() =>
    import("../features/Lists").then((m) => ({ default: m.Nodes })),
);
const Networks = lazy(() =>
    import("../features/Lists").then((m) => ({ default: m.Networks })),
);
const Volumes = lazy(() =>
    import("../features/Volumes").then((m) => ({ default: m.Volumes })),
);
const Keys = lazy(() =>
    import("../features/Keys").then((m) => ({ default: m.Keys })),
);
const KeyDetail = lazy(() =>
    import("../features/Keys").then((m) => ({ default: m.KeyDetail })),
);
const Tokens = lazy(() =>
    import("../features/Tokens").then((m) => ({ default: m.Tokens })),
);
const NodeDetail = lazy(() =>
    import("../features/NodeDetail").then((m) => ({ default: m.NodeDetail })),
);
const MachineDetail = lazy(() =>
    import("../features/MachineDetail").then((m) => ({
        default: m.MachineDetail,
    })),
);
const CreateMachine = lazy(() =>
    import("../features/MachineForm").then((m) => ({
        default: m.CreateMachine,
    })),
);
const EditMachine = lazy(() =>
    import("../features/MachineForm").then((m) => ({ default: m.EditMachine })),
);
const Console = lazy(() =>
    import("../consoles/Console").then((m) => ({ default: m.Console })),
);
const ApiDocs = lazy(() =>
    import("../features/ApiDocs").then((m) => ({ default: m.ApiDocs })),
);
const sections = [
    ["Nodes", "/nodes/"],
    ["Machines", "/machines/"],
    ["Volumes", "/volumes/"],
    ["Networks", "/networks/"],
    ["Keys", "/keys/"],
    ["Tasks", "/tasks/"],
];

function ThemeChoice() {
    const { preference, setPreference } = useAppearance();
    return (
        <TextField
            select
            label="Theme"
            value={preference}
            onChange={(e) =>
                setPreference(e.target.value as "light" | "dark" | "system")
            }
            sx={{ width: 120 }}
        >
            {["system", "light", "dark"].map((p) => (
                <MenuItem key={p} value={p}>
                    {p[0].toUpperCase() + p.slice(1)}
                </MenuItem>
            ))}
        </TextField>
    );
}
export function App({ standalone = false }: { standalone?: boolean }) {
    if (standalone)
        return (
            <>
                <Box sx={{ p: 2, display: "flex", justifyContent: "flex-end" }}>
                    <ThemeChoice />
                </Box>
                <Box sx={{ px: { xs: 2, sm: 3 } }}>
                    <Suspense
                        fallback={
                            <Typography role="status">
                                Loading console…
                            </Typography>
                        }
                    >
                        <Routes>
                            <Route
                                path="/console/:node/:id/:kind"
                                element={<Console standalone />}
                            />
                            <Route
                                path="*"
                                element={
                                    <Typography>Console not found.</Typography>
                                }
                            />
                        </Routes>
                    </Suspense>
                </Box>
            </>
        );
    return <Shell />;
}
function Shell() {
    const me = useMe();
    const [account, setAccount] = useState<HTMLElement | null>(null);
    const [drawer, setDrawer] = useState(false);
    const [error, setError] = useState("");
    const [busy, setBusy] = useState(false);
    const queries = useQueryClient();
    return (
        <>
            <Link className="skip-link" href="#main">
                Skip to content
            </Link>
            <Box
                component="header"
                sx={{
                    px: { xs: 2, sm: 3.5 },
                    py: 2,
                    display: "flex",
                    alignItems: "center",
                    gap: 3,
                    borderBottom: 1,
                    borderColor: "divider",
                    bgcolor: "background.paper",
                    flexWrap: "wrap",
                }}
            >
                <Link
                    component={RouterLink}
                    to="/nodes/"
                    className="wordmark"
                    color="text.primary"
                    underline="none"
                >
                    Vmango
                </Link>
                <Button
                    onClick={() => setDrawer(true)}
                    sx={{ display: { md: "none" } }}
                    aria-label="Open navigation"
                >
                    Menu
                </Button>
                <Box
                    component="nav"
                    aria-label="Main navigation"
                    className="top-nav"
                    sx={{ display: { xs: "none", md: "flex" }, gap: 2.5 }}
                >
                    {sections.map(([title, url]) => (
                        <NavLink key={url} to={url}>
                            {title}
                        </NavLink>
                    ))}
                </Box>
                <Box sx={{ flex: 1 }} />
                {me.links.length > 0 && (
                    <TextField
                        select
                        label={me.links_title || "Installation"}
                        value={me.links.find((l) => l.active)?.url || ""}
                        onChange={(e) => {
                            const link = me.links.find(
                                (l) => l.url === e.target.value,
                            );
                            if (link && /^(https?:\/\/|\/[^/])/.test(link.url))
                                location.assign(link.url);
                        }}
                        sx={{ width: 160 }}
                    >
                        <MenuItem value="" disabled>
                            Choose installation
                        </MenuItem>
                        {me.links.map((l) => (
                            <MenuItem key={l.url} value={l.url}>
                                {l.title}
                            </MenuItem>
                        ))}
                    </TextField>
                )}
                <ThemeChoice />
                <Button
                    onClick={(e) => setAccount(e.currentTarget)}
                    aria-haspopup="menu"
                    aria-label="Account menu"
                >
                    {me.name || me.email || "Account"}
                </Button>
                <Menu
                    anchorEl={account}
                    open={!!account}
                    onClose={() => setAccount(null)}
                >
                    <MenuItem
                        component={RouterLink}
                        to="/tokens/"
                        onClick={() => setAccount(null)}
                    >
                        API tokens
                    </MenuItem>
                    <MenuItem
                        component={RouterLink}
                        to="/api/docs"
                        onClick={() => setAccount(null)}
                    >
                        API documentation
                    </MenuItem>
                    <MenuItem
                        disabled={busy}
                        onClick={async () => {
                            if (busy) return;
                            setBusy(true);
                            setError("");
                            try {
                                await logout();
                                queries.clear();
                            } catch (e) {
                                setError((e as Error).message);
                            } finally {
                                setBusy(false);
                                setAccount(null);
                            }
                        }}
                    >
                        Log out
                    </MenuItem>
                </Menu>
            </Box>
            <Drawer open={drawer} onClose={() => setDrawer(false)}>
                <List aria-label="Main navigation" sx={{ minWidth: 240 }}>
                    {sections.map(([title, url]) => (
                        <ListItemButton
                            component={NavLink}
                            to={url}
                            key={url}
                            onClick={() => setDrawer(false)}
                        >
                            <ListItemText primary={title} />
                        </ListItemButton>
                    ))}
                </List>
            </Drawer>
            <Box
                component="main"
                id="main"
                tabIndex={-1}
                sx={{ p: { xs: 2, sm: 3.5 }, minHeight: "75vh" }}
            >
                {error && (
                    <Alert severity="error" sx={{ mb: 2 }}>
                        {error}
                    </Alert>
                )}
                <TaskMonitor />
                <Suspense
                    fallback={
                        <Typography role="status">Loading page…</Typography>
                    }
                >
                    <Routes>
                        <Route
                            path="/"
                            element={<Navigate to="/nodes/" replace />}
                        />
                        <Route
                            path="/login/"
                            element={<Navigate to="/nodes/" replace />}
                        />
                        <Route path="/nodes/" element={<Nodes />} />
                        <Route path="/nodes/:node/" element={<NodeDetail />} />
                        <Route path="/machines/" element={<Machines />} />
                        <Route
                            path="/machines/add/"
                            element={<CreateMachine />}
                        />
                        <Route
                            path="/machines/add-advanced/"
                            element={<Navigate to="/machines/add/" replace />}
                        />
                        <Route
                            path="/machines/:node/:id/"
                            element={<MachineDetail />}
                        />
                        <Route
                            path="/machines/:node/:id/update/"
                            element={<EditMachine />}
                        />
                        <Route
                            path="/machines/:node/:id/console/"
                            element={<Console />}
                        />
                        <Route
                            path="/machines/:node/:id/vnc/"
                            element={<Console />}
                        />
                        <Route path="/tasks/" element={<Tasks />} />
                        <Route path="/tasks/:id/" element={<TaskDetail />} />
                        <Route path="/volumes/" element={<Volumes />} />
                        <Route path="/networks/" element={<Networks />} />
                        <Route path="/keys/" element={<Keys />} />
                        <Route
                            path="/keys/:fingerprint/show/"
                            element={<KeyDetail />}
                        />
                        <Route path="/tokens/" element={<Tokens />} />
                        <Route path="/api/docs" element={<ApiDocs />} />
                        <Route
                            path="*"
                            element={
                                <>
                                    <Typography variant="h1">
                                        Page not found
                                    </Typography>
                                    <Button component={RouterLink} to="/nodes/">
                                        Open nodes
                                    </Button>
                                </>
                            }
                        />
                    </Routes>
                </Suspense>
            </Box>
            <Box
                component="footer"
                sx={{
                    px: 3.5,
                    py: 2,
                    borderTop: 1,
                    borderColor: "divider",
                    color: "text.secondary",
                    fontSize: 12,
                }}
            >
                Vmango {me.version}
            </Box>
        </>
    );
}
