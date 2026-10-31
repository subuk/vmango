import {
    Box,
    Divider,
    List,
    ListItemButton,
    ListItemText,
    Paper,
    Typography,
} from "@mui/material";
import { NavLink, Outlet, useMatch } from "react-router";
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
    return (
        <ListItemButton
            component={NavLink}
            to={to}
            end={end}
            sx={{
                borderRadius: 1,
                mb: 0.5,
                borderLeft: "3px solid transparent",
                "&.active": {
                    bgcolor: "action.selected",
                    borderLeftColor: "primary.main",
                    color: "primary.main",
                    "& .MuiListItemText-primary": { fontWeight: 650 },
                },
            }}
        >
            <ListItemText primary={children} />
        </ListItemButton>
    );
}

export function AdministrationLayout() {
    const match = useMatch("/administration/clouds/:cloud/*");
    const cloudID = match?.params.cloud || "";
    const cloud = useAdministration<AdminCloud>(
        adminCloudPath(cloudID),
        !!cloudID,
    );
    const base = adminCloudURL(cloudID);
    return (
        <Box className="administration-layout">
            <Paper
                variant="outlined"
                component="aside"
                className="administration-sidebar"
            >
                <Box sx={{ px: 2, pt: 2.5, pb: 1 }}>
                    <Typography
                        variant="overline"
                        color="text.secondary"
                        sx={{ fontWeight: 650, letterSpacing: 1.3 }}
                    >
                        Administration
                    </Typography>
                </Box>
                <List
                    component="nav"
                    aria-label="Administration navigation"
                    sx={{ px: 1, py: 0.5 }}
                >
                    <NavigationItem to="/administration/" end>
                        Overview
                    </NavigationItem>
                    <NavigationItem to="/administration/clouds/">
                        Clouds
                    </NavigationItem>
                    <NavigationItem to="/administration/configuration/">
                        Configuration
                    </NavigationItem>
                    <NavigationItem to="/tasks/">Tasks</NavigationItem>
                </List>
                {cloudID && cloud.data && !cloud.isError && (
                    <>
                        <Divider sx={{ mx: 2, my: 1.5 }} />
                        <Box sx={{ px: 2, pb: 1 }}>
                            <Typography
                                variant="caption"
                                color="text.secondary"
                            >
                                Selected cloud
                            </Typography>
                            <Typography
                                sx={{
                                    fontWeight: 650,
                                    overflowWrap: "anywhere",
                                }}
                            >
                                {cloud.data.name}
                            </Typography>
                        </Box>
                        <List
                            component="nav"
                            aria-label="Cloud administration"
                            sx={{ px: 1, pt: 0 }}
                        >
                            <NavigationItem to={base} end>
                                Overview
                            </NavigationItem>
                            <NavigationItem to={`${base}configuration/`}>
                                Configuration
                            </NavigationItem>
                            <NavigationItem to={`${base}images/`}>
                                Images
                            </NavigationItem>
                            {cloud.data.capabilities.host_inventory && (
                                <NavigationItem to={`${base}hosts/`}>
                                    Hosts
                                </NavigationItem>
                            )}
                        </List>
                    </>
                )}
            </Paper>
            <Box className="administration-content">
                <Outlet />
            </Box>
        </Box>
    );
}
