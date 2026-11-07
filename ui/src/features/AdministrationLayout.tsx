import { WorkspaceNavLink as NavLink } from "../app/Workspace";
import {
    Box,
    Divider,
    List,
    ListItemButton,
    ListItemText,
    MenuItem,
    Paper,
    TextField,
    Typography,
} from "@mui/material";
import { Outlet, useLocation, useMatch, useNavigate } from "react-router";
import {
    adminCloudPath,
    adminCloudURL,
    useAdministration,
    type AdminCloud,
} from "../api/administration";

function NavigationItem({
    to,
    children,
    end = false,
}: {
    to: string;
    children: string;
    end?: boolean;
}) {
    const location = useLocation();
    const workspace = new URLSearchParams(location.search).get("namespace");
    const target =
        workspace && !to.includes("?")
            ? `${to}?namespace=${encodeURIComponent(workspace)}`
            : to;
    return (
        <ListItemButton
            component={NavLink}
            to={target}
            end={end}
            sx={{
                borderRadius: 1,
                minHeight: 36,
                py: 0.25,
                borderLeft: "3px solid transparent",
                "&.active": {
                    bgcolor: "action.selected",
                    borderLeftColor: "primary.main",
                    color: "primary.main",
                    "& .MuiListItemText-primary": { fontWeight: 650 },
                },
            }}
        >
            <ListItemText
                primary={children}
                slotProps={{ primary: { sx: { fontSize: 14 } } }}
            />
        </ListItemButton>
    );
}
export function AdministrationLayout() {
    const match = useMatch("/administration/clouds/:cloud/*");
    const location = useLocation();
    const navigate = useNavigate();
    const cloudID = match?.params.cloud || "";
    const cloud = useAdministration<AdminCloud>(
        adminCloudPath(cloudID),
        !!cloudID,
    );
    const clouds = useAdministration<AdminCloud[]>(
        "/api/v1/administration/clouds",
    );
    const base = adminCloudURL(cloudID);
    const selected = cloud.data && !cloud.isError ? cloud.data : undefined;
    return (
        <Box className="administration-layout">
            <Paper
                variant="outlined"
                component="aside"
                className="administration-sidebar"
            >
                <Box sx={{ px: 1.5, pt: 1.5, pb: 1 }}>
                    <Typography
                        variant="overline"
                        color="text.secondary"
                        sx={{ fontWeight: 650, letterSpacing: 1 }}
                    >
                        Administration
                    </Typography>
                    <Typography
                        variant="caption"
                        color="text.secondary"
                        sx={{ display: "block" }}
                    >
                        Configuration and observations
                    </Typography>
                </Box>
                <List
                    component="nav"
                    aria-label="Administration navigation"
                    sx={{ px: 0.75, py: 0.5 }}
                >
                    <NavigationItem to="/administration/" end>
                        Overview
                    </NavigationItem>
                    <NavigationItem to="/administration/clouds/" end>
                        Clouds
                    </NavigationItem>
                    <NavigationItem to="/administration/configuration/namespaces/">
                        Namespaces
                    </NavigationItem>
                    <NavigationItem to="/administration/configuration/" end>
                        Installation configuration
                    </NavigationItem>
                </List>
                <Divider sx={{ mx: 1.5, my: 1 }} />
                <Box sx={{ px: 1.5, py: 1 }}>
                    <TextField
                        select
                        fullWidth
                        size="small"
                        label="Selected cloud"
                        value={cloudID}
                        disabled={!clouds.data}
                        slotProps={{
                            inputLabel: { shrink: true },
                            select: { displayEmpty: true },
                        }}
                        onChange={(event) => {
                            const nextCloud = clouds.data?.find(
                                (c) => c.id === event.target.value,
                            );
                            if (!nextCloud) return;
                            const section =
                                match?.params["*"]?.split("/")[0] || "";
                            const supported =
                                [
                                    "machines",
                                    "volumes",
                                    "images",
                                    "configuration",
                                ].includes(section) ||
                                (section === "hosts" &&
                                    nextCloud.capabilities.host_inventory);
                            const next = new URLSearchParams();
                            const old = new URLSearchParams(location.search);
                            for (const key of [
                                "q",
                                "sort",
                                "direction",
                                "inventory_namespace",
                                "state",
                                "namespace",
                            ]) {
                                const value = old.get(key);
                                if (value) next.set(key, value);
                            }
                            navigate({
                                pathname: `${adminCloudURL(nextCloud.id)}${supported ? `${section}/` : ""}`,
                                search: next.toString(),
                            });
                        }}
                    >
                        <MenuItem value="" disabled>
                            Choose cloud
                        </MenuItem>
                        {cloudID &&
                            !clouds.data?.some((c) => c.id === cloudID) && (
                                <MenuItem value={cloudID}>{cloudID}</MenuItem>
                            )}
                        {clouds.data?.map((c) => (
                            <MenuItem key={c.id} value={c.id}>
                                {c.name}
                            </MenuItem>
                        ))}
                    </TextField>
                    {clouds.isError && (
                        <Typography variant="caption" color="error">
                            Cloud list unavailable
                        </Typography>
                    )}
                </Box>
                {cloudID && selected && (
                    <List
                        component="nav"
                        aria-label="Cloud administration"
                        sx={{ px: 0.75, py: 0.5 }}
                    >
                        <NavigationItem to={base} end>
                            Cloud overview
                        </NavigationItem>
                        <NavigationItem to={`${base}machines/`}>
                            Machine inventory
                        </NavigationItem>
                        <NavigationItem to={`${base}volumes/`}>
                            Volume inventory
                        </NavigationItem>
                        {selected.capabilities.host_inventory && (
                            <NavigationItem to={`${base}hosts/`}>
                                Hosts
                            </NavigationItem>
                        )}
                        <NavigationItem to={`${base}images/`}>
                            Images
                        </NavigationItem>
                        <NavigationItem to={`${base}configuration/`}>
                            Cloud configuration
                        </NavigationItem>
                    </List>
                )}
                <Divider sx={{ mx: 1.5, my: 1 }} />
                <Typography
                    variant="overline"
                    color="text.secondary"
                    sx={{ px: 1.5 }}
                >
                    Operations
                </Typography>
                <List
                    component="nav"
                    aria-label="Operations navigation"
                    sx={{ px: 0.75, py: 0.5 }}
                >
                    <NavigationItem to="/administration/tasks/">
                        Task history
                    </NavigationItem>
                    <NavigationItem to="/tasks/">
                        Workspace tasks
                    </NavigationItem>
                </List>
            </Paper>
            <Box className="administration-content">
                <Outlet />
            </Box>
        </Box>
    );
}
