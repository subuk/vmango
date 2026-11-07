import { useSearchParams } from "react-router";
import {
    Alert,
    Box,
    Button,
    MenuItem,
    Stack,
    TextField,
    Typography,
} from "@mui/material";
import {
    adminCloudPath,
    adminCloudURL,
    useAdministration,
    type AdminCloud,
    type HostList,
    type Installation,
} from "../api/administration";
import { PageTitle, QueryState, bytes } from "../components/common";
import { DetailSection, Summary } from "../components/Information";
import { ResourceTable } from "../components/ResourceTable";
import { Fields, link } from "./AdministrationFields";
import { AdministrationActivity } from "./AdministrationOperations";

const root = "/api/v1/administration";

export function AdministrationOverview() {
    const installation = useAdministration<Installation>(
        `${root}/configuration`,
    );
    const clouds = useAdministration<AdminCloud[]>(`${root}/clouds`);
    return (
        <>
            <PageTitle
                title="Overview"
                description="Operational attention and configured infrastructure across the installation."
                action={
                    <Button
                        onClick={() => {
                            void installation.refetch();
                            void clouds.refetch();
                        }}
                    >
                        Refresh
                    </Button>
                }
            />
            <Box sx={{ mb: 2 }}>
                <AdministrationActivity />
            </Box>
            <QueryState query={clouds}>
                {(items) => (
                    <Stack spacing={2}>
                        <Summary
                            label="Administration summary"
                            items={[
                                {
                                    label: "Clouds",
                                    value: items.length,
                                    detail: "Configured backends",
                                },
                                {
                                    label: "Hosts",
                                    value: items.reduce(
                                        (n, c) =>
                                            n + (c.libvirt?.hosts.length || 0),
                                        0,
                                    ),
                                    detail: "Configured; host-capable clouds",
                                },
                                {
                                    label: "Images",
                                    value: items.reduce(
                                        (n, c) => n + c.images.length,
                                        0,
                                    ),
                                    detail: "Configured cloud-owned definitions",
                                },
                                {
                                    label: "Zones",
                                    value: items.reduce(
                                        (n, c) => n + c.zones.length,
                                        0,
                                    ),
                                    detail: "Configured placement locations",
                                },
                            ]}
                        />
                        <OverviewObservations clouds={items} />
                        <Box
                            sx={{
                                display: "grid",
                                gridTemplateColumns: {
                                    xs: "1fr",
                                    lg: "minmax(0, 2fr) minmax(260px, 1fr)",
                                },
                                gap: 2,
                            }}
                        >
                            <DetailSection
                                title="Cloud infrastructure"
                                description="Configuration is available independently of live host observations."
                            >
                                <ResourceTable
                                    rows={items}
                                    rowKey={(c) => c.id}
                                    search={(c) =>
                                        `${c.id} ${c.name} ${c.backend_kind}`
                                    }
                                    columns={[
                                        {
                                            id: "name",
                                            label: "Cloud",
                                            value: (c) => c.name,
                                            render: (c) =>
                                                link(
                                                    adminCloudURL(c.id),
                                                    c.name,
                                                ),
                                        },
                                        {
                                            id: "backend",
                                            label: "Backend",
                                            value: (c) => c.backend_kind,
                                        },
                                        {
                                            id: "zones",
                                            label: "Configured zones",
                                            value: (c) => c.zones.length,
                                        },
                                        {
                                            id: "images",
                                            label: "Configured images",
                                            value: (c) => c.images.length,
                                        },
                                    ]}
                                    empty="No clouds are configured."
                                />
                            </DetailSection>
                            <DetailSection title="Installation">
                                <QueryState query={installation}>
                                    {(i) => (
                                        <>
                                            <Fields
                                                entries={[
                                                    ["Version", i.version],
                                                    [
                                                        "Loaded at (UTC)",
                                                        i.loaded_at,
                                                    ],
                                                    [
                                                        "Task workers",
                                                        i.tasks.workers,
                                                    ],
                                                ]}
                                            />
                                            <Typography
                                                variant="body2"
                                                color="text.secondary"
                                                sx={{ my: 1.5 }}
                                            >
                                                Configuration changes take
                                                effect after a restart.
                                            </Typography>
                                            <Stack spacing={1}>
                                                {link(
                                                    "/administration/configuration/",
                                                    "View configuration",
                                                )}
                                                {link(
                                                    "/administration/configuration/namespaces/",
                                                    "Review namespaces",
                                                )}
                                                {link(
                                                    "/administration/tasks/",
                                                    "Open task history",
                                                )}
                                            </Stack>
                                        </>
                                    )}
                                </QueryState>
                            </DetailSection>
                        </Box>
                    </Stack>
                )}
            </QueryState>
        </>
    );
}

function OverviewObservations({ clouds }: { clouds: AdminCloud[] }) {
    const capable = clouds.filter((c) => c.capabilities.host_inventory);
    const [params, setParams] = useSearchParams();
    const selected = params.get("observation_cloud") || "";
    const select = (id: string) =>
        setParams(
            (old) => {
                const next = new URLSearchParams(old);
                next.set("observation_cloud", id);
                return next;
            },
            { replace: true },
        );
    const cloud = capable.find((c) => c.id === selected) || capable[0];
    if (!cloud)
        return (
            <Alert severity="info">
                Configured clouds do not expose host observations.
            </Alert>
        );
    return (
        <Box>
            {selected && !capable.some((c) => c.id === selected) && (
                <Alert severity="info" sx={{ mb: 1.5 }}>
                    The requested observation cloud is unavailable or does not
                    expose host observations. Showing {cloud.name}.
                </Alert>
            )}
            {capable.length > 1 && (
                <TextField
                    select
                    size="small"
                    label="Observation cloud"
                    value={cloud.id}
                    onChange={(e) => select(e.target.value)}
                    sx={{ mb: 1.5, minWidth: 240 }}
                >
                    {capable.map((c) => (
                        <MenuItem key={c.id} value={c.id}>
                            {c.name}
                        </MenuItem>
                    ))}
                </TextField>
            )}
            <CloudHostObservations key={cloud.id} cloud={cloud} />
        </Box>
    );
}

function CloudHostObservations({ cloud }: { cloud: AdminCloud }) {
    const q = useAdministration<HostList>(
        `${adminCloudPath(cloud.id)}/hosts`,
        cloud.capabilities.host_inventory,
    );
    if (!cloud.capabilities.host_inventory) return null;
    return (
        <DetailSection
            title={`${cloud.name} host observations`}
            description="Live host inventory for this cloud only. Machine and volume inventories are loaded in their own views."
        >
            <Stack
                direction="row"
                sx={{ mb: 1.5, alignItems: "center", gap: 2, flexWrap: "wrap" }}
            >
                <Button size="small" onClick={() => void q.refetch()}>
                    Refresh hosts
                </Button>
                <Typography variant="caption" color="text.secondary">
                    {q.dataUpdatedAt
                        ? `Updated ${new Date(q.dataUpdatedAt).toLocaleTimeString()}`
                        : "Observation pending"}
                </Typography>
                {link(`${adminCloudURL(cloud.id)}hosts/`, "Inspect hosts")}
            </Stack>
            <QueryState query={q}>
                {(result) => (
                    <>
                        {!!result.unavailable_hosts.length && (
                            <Alert severity="warning" sx={{ mb: 2 }}>
                                {result.unavailable_hosts.length} unavailable
                                host observations. Inventory and capacity below
                                cover observed hosts only.
                                <Stack
                                    direction="row"
                                    sx={{ gap: 2, mt: 1, flexWrap: "wrap" }}
                                >
                                    {result.unavailable_hosts.map((h) => (
                                        <Box key={h.id}>
                                            {link(
                                                `${adminCloudURL(cloud.id)}hosts/${encodeURIComponent(h.id)}/`,
                                                h.id,
                                            )}{" "}
                                            <Typography
                                                component="span"
                                                variant="caption"
                                            >
                                                ({h.code})
                                            </Typography>
                                        </Box>
                                    ))}
                                </Stack>
                            </Alert>
                        )}
                        <Summary
                            label={`${cloud.name} observed host summary`}
                            items={[
                                {
                                    label: "Observed hosts",
                                    value: result.items.length,
                                    detail: `${cloud.libvirt?.hosts.length || 0} configured in ${cloud.name}`,
                                },
                                {
                                    label: "Unavailable hosts",
                                    value: result.unavailable_hosts.length,
                                    detail: "Observation failures",
                                    tone: result.unavailable_hosts.length
                                        ? "warning"
                                        : undefined,
                                },
                                {
                                    label: "Observed memory",
                                    value: bytes(
                                        result.items.reduce(
                                            (n, h) => n + h.memory_bytes,
                                            0,
                                        ),
                                    ),
                                    detail: "Observed hosts only",
                                },
                            ]}
                        />
                    </>
                )}
            </QueryState>
            {q.isError && (
                <Typography variant="body2" sx={{ mt: 1 }}>
                    {link(
                        `${adminCloudURL(cloud.id)}configuration/`,
                        "Configured cloud settings remain available",
                    )}
                </Typography>
            )}
        </DetailSection>
    );
}

export function CloudOverview({
    cloud: c,
    refresh,
}: {
    cloud: AdminCloud;
    refresh: () => void;
}) {
    return (
        <>
            <PageTitle
                title={c.name}
                description={`${c.backend_kind} · Cloud operations and configuration`}
                action={<Button onClick={refresh}>Refresh</Button>}
            />
            <AdministrationActivity cloud={c.id} />
            <Stack spacing={2}>
                <CloudHostObservations cloud={c} />
                <Summary
                    items={[
                        {
                            label: "Zones",
                            value: c.zones.length,
                            detail: "Configured",
                        },
                        {
                            label: "Instance types",
                            value: c.instance_types.length,
                            detail: "Configured",
                        },
                        {
                            label: "Images",
                            value: c.images.length,
                            detail: "Configured",
                        },
                        ...(c.capabilities.host_inventory
                            ? [
                                  {
                                      label: "Hosts",
                                      value: c.libvirt?.hosts.length || 0,
                                      detail: "Configured",
                                  },
                              ]
                            : []),
                    ]}
                />
                <DetailSection title="Configured cloud">
                    <Fields
                        entries={[
                            ["Cloud ID", c.id],
                            ["Backend", c.backend_kind],
                            ["Zones", c.zones.map((z) => z.name).join(", ")],
                            [
                                "Logical networks",
                                c.networks.map((n) => n.name).join(", "),
                            ],
                            [
                                "Storage classes",
                                c.storage_classes.map((s) => s.name).join(", "),
                            ],
                        ]}
                    />
                    <Box sx={{ mt: 1.5 }}>
                        {link(
                            `${adminCloudURL(c.id)}configuration/`,
                            "View cloud configuration",
                        )}
                    </Box>
                </DetailSection>
                <DetailSection title="Admitted namespaces">
                    <Stack direction="row" sx={{ flexWrap: "wrap", gap: 2 }}>
                        {c.namespace_ids?.map((id) => (
                            <Box key={id}>
                                {link(
                                    `/administration/configuration/namespaces/${encodeURIComponent(id)}/`,
                                    id,
                                )}
                            </Box>
                        ))}
                        {!c.namespace_ids?.length && (
                            <Typography>
                                No namespaces admit this cloud.
                            </Typography>
                        )}
                    </Stack>
                </DetailSection>
            </Stack>
        </>
    );
}
