import type { ReactNode } from "react";
import {
    Alert,
    Box,
    Breadcrumbs,
    Button,
    Link,
    MenuItem,
    Paper,
    Stack,
    TextField,
    Typography,
} from "@mui/material";
import {
    Link as RouterLink,
    Route,
    Routes,
    useParams,
    useSearchParams,
} from "react-router";
import {
    adminCloudPath,
    adminCloudURL,
    useAdministration,
    type AdminCloud,
    type AdminHost,
    type AdminImage,
    type Installation,
    type ImageAvailability,
    type HostList,
} from "../api/administration";
import { PageTitle, QueryState, bytes } from "../components/common";
import { DetailSection, Summary } from "../components/Information";
import { ResourceTable } from "../components/ResourceTable";
import { NodeDetail } from "./NodeDetail";

const root = "/api/v1/administration";
const link = (to: string, text: string) => (
    <Link component={RouterLink} to={to}>
        {text}
    </Link>
);
const setting = (v: unknown): ReactNode =>
    v === null || v === undefined || v === ""
        ? "Unspecified"
        : typeof v === "boolean"
          ? v
              ? "Enabled"
              : "Disabled"
          : String(v);
function Fields({ entries }: { entries: [string, unknown][] }) {
    return (
        <Box component="dl" className="details">
            {entries.map(([name, value]) => (
                <Box key={name} sx={{ display: "contents" }}>
                    <dt>{name}</dt>
                    <dd style={{ overflowWrap: "anywhere" }}>
                        {setting(value)}
                    </dd>
                </Box>
            ))}
        </Box>
    );
}
export function Administration() {
    const snapshot = useAdministration<Installation>(`${root}/configuration`);
    return (
        <>
            <Routes>
                <Route index element={<AdministrationOverview />} />
                <Route path="configuration/" element={<InstallationPage />} />
                <Route
                    path="clouds/"
                    element={<Clouds snapshot={snapshot.data?.snapshot_id} />}
                />
                <Route
                    path="clouds/:cloud/*"
                    element={
                        <CloudPages snapshot={snapshot.data?.snapshot_id} />
                    }
                />
                <Route
                    path="*"
                    element={<Alert severity="error">Page not found.</Alert>}
                />
            </Routes>
        </>
    );
}
function AdministrationOverview() {
    const installation = useAdministration<Installation>(
        `${root}/configuration`,
    );
    const clouds = useAdministration<AdminCloud[]>(`${root}/clouds`);
    return (
        <>
            <PageTitle
                title="Overview"
                description="Infrastructure catalogs, installation status and operations."
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
            <QueryState query={clouds}>
                {(items) => (
                    <>
                        <Summary
                            label="Administration summary"
                            items={[
                                {
                                    label: "Clouds",
                                    value: items.length,
                                    detail: "Configured backends",
                                },
                                {
                                    label: "Zones",
                                    value: items.reduce(
                                        (n, c) => n + c.zones.length,
                                        0,
                                    ),
                                    detail: "Placement locations",
                                },
                                {
                                    label: "Images",
                                    value: items.reduce(
                                        (n, c) => n + c.images.length,
                                        0,
                                    ),
                                    detail: "Cloud-owned definitions",
                                },
                                {
                                    label: "Instance types",
                                    value: items.reduce(
                                        (n, c) => n + c.instance_types.length,
                                        0,
                                    ),
                                    detail: "Resource profiles",
                                },
                            ]}
                        />
                        <Box className="administration-overview-grid">
                            <DetailSection
                                title="Cloud infrastructure"
                                description="Explore catalogs, host settings and live diagnostics."
                            >
                                {items.length ? (
                                    <Stack spacing={1.5}>
                                        {items.map((c) => (
                                            <Paper
                                                key={c.id}
                                                variant="outlined"
                                                sx={{ p: 2 }}
                                            >
                                                <Link
                                                    component={RouterLink}
                                                    to={adminCloudURL(c.id)}
                                                    sx={{ fontWeight: 650 }}
                                                >
                                                    {c.name}
                                                </Link>
                                                <Typography
                                                    variant="body2"
                                                    color="text.secondary"
                                                    sx={{ mt: 0.5 }}
                                                >
                                                    {c.backend_kind} ·{" "}
                                                    {c.zones.length} zones ·{" "}
                                                    {c.images.length} images
                                                    {c.capabilities
                                                        .host_inventory
                                                        ? ` · ${c.libvirt?.hosts.length || 0} hosts`
                                                        : ""}
                                                </Typography>
                                            </Paper>
                                        ))}
                                    </Stack>
                                ) : (
                                    <Typography color="text.secondary">
                                        No clouds are configured.
                                    </Typography>
                                )}
                                <Button
                                    component={RouterLink}
                                    to="/administration/clouds/"
                                    sx={{ mt: 2 }}
                                >
                                    View all clouds
                                </Button>
                            </DetailSection>
                            <Stack spacing={3}>
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
                                                    sx={{ mt: 2 }}
                                                >
                                                    Configuration changes take
                                                    effect after a restart.
                                                </Typography>
                                                <Button
                                                    component={RouterLink}
                                                    to="/administration/configuration/"
                                                    sx={{ mt: 1 }}
                                                >
                                                    View configuration
                                                </Button>
                                            </>
                                        )}
                                    </QueryState>
                                </DetailSection>
                                <DetailSection
                                    title="Operations"
                                    description="Follow active work and inspect completed or interrupted tasks."
                                >
                                    <Typography
                                        variant="body2"
                                        color="text.secondary"
                                    >
                                        Task history keeps execution stages,
                                        resource references and recovery
                                        details.
                                    </Typography>
                                    <Button
                                        component={RouterLink}
                                        to="/tasks/"
                                        sx={{ mt: 2 }}
                                    >
                                        Open task history
                                    </Button>
                                </DetailSection>
                            </Stack>
                        </Box>
                    </>
                )}
            </QueryState>
        </>
    );
}
function InstallationPage() {
    const q = useAdministration<Installation>(`${root}/configuration`);
    return (
        <>
            <PageTitle
                title="Installation configuration"
                description="Settings accepted at startup. Edit the configuration file and restart Vmango to activate changes."
                action={
                    <Button onClick={() => void q.refetch()}>Refresh</Button>
                }
            />
            <QueryState query={q}>
                {(i) => (
                    <DetailSection title="Runtime settings">
                        <Fields
                            entries={[
                                ["Version", i.version],
                                ["Snapshot ID", i.snapshot_id],
                                ["Loaded at (UTC)", i.loaded_at],
                                ["Activation", i.activation],
                                ["Log level", i.log_level],
                                ["Task workers", i.tasks.workers],
                                [
                                    "Task retention (days)",
                                    i.tasks.retention_days,
                                ],
                                [
                                    "Image transfer limit",
                                    bytes(i.image_transfers.max_bytes),
                                ],
                                [
                                    "Transfer timeout (seconds)",
                                    i.image_transfers.timeout_seconds,
                                ],
                                [
                                    "Idle timeout (seconds)",
                                    i.image_transfers.idle_timeout_seconds,
                                ],
                            ]}
                        />
                    </DetailSection>
                )}
            </QueryState>
        </>
    );
}
function Clouds({ snapshot }: { snapshot?: string }) {
    const q = useAdministration<AdminCloud[]>(`${root}/clouds`, true, snapshot);
    return (
        <>
            <PageTitle
                title="Clouds"
                description="Configured infrastructure and provisioning catalogs."
                action={
                    <Button onClick={() => void q.refetch()}>Refresh</Button>
                }
            />
            <QueryState query={q}>
                {(rows) => (
                    <ResourceTable
                        rows={rows}
                        rowKey={(c) => c.id}
                        search={(c) => `${c.id} ${c.name} ${c.backend_kind}`}
                        columns={[
                            {
                                id: "name",
                                label: "Cloud",
                                value: (c) => c.name,
                                render: (c) =>
                                    link(adminCloudURL(c.id), c.name),
                            },
                            { id: "id", label: "ID", value: (c) => c.id },
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
                )}
            </QueryState>
        </>
    );
}
function CloudPages({ snapshot }: { snapshot?: string }) {
    const { cloud = "" } = useParams();
    const q = useAdministration<AdminCloud>(
        adminCloudPath(cloud),
        true,
        snapshot,
    );
    return (
        <QueryState query={q}>
            {(c) => (
                <>
                    <Breadcrumbs sx={{ mb: 2 }}>
                        {link("/administration/clouds/", "Clouds")}
                        {link(adminCloudURL(c.id), c.name)}
                    </Breadcrumbs>
                    <Routes>
                        <Route
                            index
                            element={
                                <CloudOverview
                                    cloud={c}
                                    refresh={() => void q.refetch()}
                                />
                            }
                        />
                        <Route
                            path="configuration/"
                            element={
                                <CloudConfiguration
                                    cloud={c}
                                    snapshot={snapshot}
                                />
                            }
                        />
                        <Route
                            path="configuration/hosts/"
                            element={
                                c.capabilities.host_inventory ? (
                                    <ConfiguredHosts
                                        cloud={c}
                                        snapshot={snapshot}
                                    />
                                ) : (
                                    <NotFound />
                                )
                            }
                        />
                        <Route
                            path="configuration/hosts/:node/"
                            element={
                                c.capabilities.host_inventory ? (
                                    <ConfiguredHost
                                        cloud={c}
                                        snapshot={snapshot}
                                    />
                                ) : (
                                    <NotFound />
                                )
                            }
                        />
                        <Route
                            path="hosts/"
                            element={
                                c.capabilities.host_inventory ? (
                                    <LiveHosts cloud={c} />
                                ) : (
                                    <NotFound />
                                )
                            }
                        />
                        <Route
                            path="hosts/:node/"
                            element={
                                c.capabilities.host_inventory ? (
                                    <NodeDetail />
                                ) : (
                                    <NotFound />
                                )
                            }
                        />
                        <Route
                            path="images/"
                            element={<Images cloud={c} snapshot={snapshot} />}
                        />
                        <Route
                            path="images/:image/"
                            element={
                                <ImageDetail cloud={c} snapshot={snapshot} />
                            }
                        />
                        <Route path="*" element={<NotFound />} />
                    </Routes>
                </>
            )}
        </QueryState>
    );
}
function NotFound() {
    return <Alert severity="error">Page not found.</Alert>;
}
function CloudOverview({
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
                description="Configured cloud overview"
                action={<Button onClick={refresh}>Refresh</Button>}
            />
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
            </DetailSection>
        </>
    );
}
function Catalog({
    title,
    rows,
}: {
    title: string;
    rows: { id: string; name: string }[];
}) {
    return (
        <DetailSection title={title}>
            <ResourceTable
                rows={rows}
                rowKey={(e) => e.id}
                search={(e) => `${e.id} ${e.name}`}
                columns={[
                    { id: "id", label: "ID", value: (e) => e.id },
                    { id: "name", label: "Name", value: (e) => e.name },
                ]}
                empty={`No ${title.toLowerCase()} configured.`}
            />
        </DetailSection>
    );
}
function CloudConfiguration({
    cloud,
    snapshot,
}: {
    cloud: AdminCloud;
    snapshot?: string;
}) {
    const q = useAdministration<AdminCloud>(
        `${adminCloudPath(cloud.id)}/configuration`,
        true,
        snapshot,
    );
    return (
        <>
            <PageTitle
                title={`${cloud.name} configuration`}
                description="Configured catalogs and policy"
                action={
                    <Button onClick={() => void q.refetch()}>Refresh</Button>
                }
            />
            <QueryState query={q}>
                {(c) => (
                    <Stack spacing={3}>
                        <Catalog title="Zones" rows={c.zones} />
                        <DetailSection title="Instance types">
                            <ResourceTable
                                rows={c.instance_types}
                                rowKey={(t) => t.id}
                                search={(t) => `${t.id} ${t.name}`}
                                columns={[
                                    {
                                        id: "id",
                                        label: "ID",
                                        value: (t) => t.id,
                                    },
                                    {
                                        id: "name",
                                        label: "Name",
                                        value: (t) => t.name,
                                    },
                                    {
                                        id: "cpu",
                                        label: "vCPUs / request",
                                        value: (t) =>
                                            `${t.vcpus} / ${t.cpu_request}`,
                                    },
                                    {
                                        id: "memory",
                                        label: "Memory / request",
                                        value: (t) =>
                                            `${bytes(t.memory_bytes)} / ${bytes(t.memory_request_bytes)}`,
                                    },
                                    {
                                        id: "disk",
                                        label: "Boot disk",
                                        value: (t) => t.boot_disk_bytes,
                                        render: (t) => bytes(t.boot_disk_bytes),
                                    },
                                    {
                                        id: "hugepages",
                                        label: "Hugepages",
                                        value: (t) =>
                                            t.hugepages
                                                ? "Enabled"
                                                : "Disabled",
                                    },
                                    {
                                        id: "autostart",
                                        label: "Autostart",
                                        value: (t) =>
                                            t.autostart
                                                ? "Enabled"
                                                : "Disabled",
                                    },
                                ]}
                            />
                        </DetailSection>
                        <Catalog title="Logical networks" rows={c.networks} />
                        <Catalog
                            title="Storage classes"
                            rows={c.storage_classes}
                        />
                        <DetailSection title="Image references">
                            {c.images.length
                                ? c.images.map((i) => (
                                      <Box key={i}>
                                          {link(
                                              `${adminCloudURL(c.id)}images/${encodeURIComponent(i)}/`,
                                              i,
                                          )}
                                      </Box>
                                  ))
                                : "No images configured."}
                        </DetailSection>
                        {c.capabilities.host_inventory && (
                            <DetailSection title="Configured hosts">
                                {link(
                                    `${adminCloudURL(c.id)}configuration/hosts/`,
                                    "Open configured hosts",
                                )}
                            </DetailSection>
                        )}
                    </Stack>
                )}
            </QueryState>
        </>
    );
}
function ConfiguredHosts({
    cloud,
    snapshot,
}: {
    cloud: AdminCloud;
    snapshot?: string;
}) {
    const q = useAdministration<
        { id: string; cloud_id: string; zone: string }[]
    >(`${adminCloudPath(cloud.id)}/configuration/hosts`, true, snapshot);
    return (
        <>
            <PageTitle
                title="Configured hosts"
                action={
                    <Button onClick={() => void q.refetch()}>Refresh</Button>
                }
            />
            <QueryState query={q}>
                {(rows) => (
                    <ResourceTable
                        rows={rows}
                        rowKey={(h) => h.id}
                        search={(h) => `${h.id} ${h.zone}`}
                        columns={[
                            {
                                id: "id",
                                label: "Host",
                                value: (h) => h.id,
                                render: (h) =>
                                    link(
                                        `${adminCloudURL(cloud.id)}configuration/hosts/${encodeURIComponent(h.id)}/`,
                                        h.id,
                                    ),
                            },
                            { id: "zone", label: "Zone", value: (h) => h.zone },
                        ]}
                    />
                )}
            </QueryState>
        </>
    );
}
function ConfiguredHost({
    cloud,
    snapshot,
}: {
    cloud: AdminCloud;
    snapshot?: string;
}) {
    const { node = "" } = useParams();
    const q = useAdministration<AdminHost>(
        `${adminCloudPath(cloud.id)}/configuration/hosts/${encodeURIComponent(node)}`,
        true,
        snapshot,
    );
    return (
        <>
            <PageTitle
                title={node}
                description="Configured host settings and bindings"
                action={
                    <Button onClick={() => void q.refetch()}>Refresh</Button>
                }
            />
            <QueryState query={q}>
                {(h) => (
                    <Stack spacing={3}>
                        <Box>
                            {link(
                                `${adminCloudURL(cloud.id)}hosts/${encodeURIComponent(h.id)}/`,
                                "Open live diagnostics",
                            )}
                        </Box>
                        <DetailSection title="Configured settings">
                            <Fields
                                entries={[
                                    ["Cloud", h.cloud_id],
                                    ["Zone", h.zone],
                                    [
                                        "Placement labels",
                                        Object.entries(h.labels || {})
                                            .map(([k, v]) => `${k}=${v}`)
                                            .join(", "),
                                    ],
                                    ["CPU overcommit", h.cpu_overcommit],
                                    ["CPU reserve", h.cpu_reserve],
                                    [
                                        "Memory reserve",
                                        bytes(h.memory_reserve_bytes),
                                    ],
                                    [
                                        "Emulator",
                                        `${h.emulator.value || h.emulator.state} (${h.emulator.source})`,
                                    ],
                                    ["Image pool", h.image_pool],
                                    ["Config drive pool", h.config_drive_pool],
                                    [
                                        "Config drive suffix",
                                        h.config_drive_suffix,
                                    ],
                                    [
                                        "Config drive format",
                                        h.config_drive_format,
                                    ],
                                    ["Graphics listener", h.graphics_listen],
                                    ["Cache", h.cache],
                                    [
                                        "QCOW metadata preallocation",
                                        h.qcow_prealloc_metadata,
                                    ],
                                ]}
                            />
                        </DetailSection>
                        <DetailSection title="Configured network bindings">
                            <ResourceTable
                                rows={h.networks}
                                rowKey={(n) => n.id}
                                search={(n) => `${n.id} ${n.backend}`}
                                columns={[
                                    {
                                        id: "id",
                                        label: "Logical network",
                                        value: (n) => n.id,
                                    },
                                    {
                                        id: "kind",
                                        label: "Kind",
                                        value: (n) => n.kind,
                                    },
                                    {
                                        id: "backend",
                                        label: "Backend",
                                        value: (n) => n.backend,
                                    },
                                    {
                                        id: "model",
                                        label: "Model",
                                        value: (n) => n.model || "Inherited",
                                    },
                                    {
                                        id: "vlan",
                                        label: "VLAN",
                                        value: (n) => n.vlan,
                                    },
                                ]}
                            />
                        </DetailSection>
                        <DetailSection title="Configured storage bindings">
                            <ResourceTable
                                rows={h.storage}
                                rowKey={(s) => s.id}
                                search={(s) => `${s.id} ${s.pool}`}
                                columns={[
                                    {
                                        id: "id",
                                        label: "Storage class",
                                        value: (s) => s.id,
                                    },
                                    {
                                        id: "pool",
                                        label: "Pool",
                                        value: (s) => s.pool,
                                    },
                                    {
                                        id: "format",
                                        label: "Format",
                                        value: (s) => s.format,
                                    },
                                ]}
                            />
                        </DetailSection>
                    </Stack>
                )}
            </QueryState>
        </>
    );
}
function LiveHosts({ cloud }: { cloud: AdminCloud }) {
    const q = useAdministration<HostList>(`${adminCloudPath(cloud.id)}/hosts`);
    return (
        <>
            <PageTitle
                title="Live hosts"
                description="Current host diagnostics"
                action={
                    <Button onClick={() => void q.refetch()}>Refresh</Button>
                }
            />
            <QueryState query={q}>
                {(result) => (
                    <>
                        {!!result.unavailable_hosts.length && (
                            <Alert severity="warning">
                                Unavailable hosts:{" "}
                                {result.unavailable_hosts
                                    .map((h) => `${h.id} (${h.code})`)
                                    .join(", ")}
                                . Configured host settings remain available.
                            </Alert>
                        )}
                        <ResourceTable
                            rows={result.items}
                            rowKey={(h) => h.id}
                            search={(h) => `${h.id} ${h.hostname} ${h.arch}`}
                            columns={[
                                {
                                    id: "id",
                                    label: "Host",
                                    value: (h) => h.id,
                                    render: (h) =>
                                        link(
                                            `${adminCloudURL(cloud.id)}hosts/${encodeURIComponent(h.id)}/`,
                                            h.id,
                                        ),
                                },
                                {
                                    id: "hostname",
                                    label: "Hostname",
                                    value: (h) => h.hostname,
                                },
                                {
                                    id: "arch",
                                    label: "Architecture",
                                    value: (h) => h.arch,
                                },
                                {
                                    id: "memory",
                                    label: "Memory",
                                    value: (h) => h.memory_bytes,
                                    render: (h) => bytes(h.memory_bytes),
                                },
                            ]}
                            empty={
                                result.unavailable_hosts.length
                                    ? "Live inventory is unavailable."
                                    : "No hosts configured."
                            }
                        />
                    </>
                )}
            </QueryState>
        </>
    );
}
function Images({ cloud, snapshot }: { cloud: AdminCloud; snapshot?: string }) {
    const [params, setParams] = useSearchParams();
    const arch = params.get("arch") || "";
    const q = useAdministration<AdminImage[]>(
        `${adminCloudPath(cloud.id)}/images${arch ? `?arch=${encodeURIComponent(arch)}` : ""}`,
        true,
        snapshot,
    );
    return (
        <>
            <PageTitle
                title="Configured images"
                description="Cloud-owned definitions, independent of live source availability"
                action={
                    <Button onClick={() => void q.refetch()}>Refresh</Button>
                }
            />
            <TextField
                select
                label="Architecture"
                value={arch}
                onChange={(e) =>
                    setParams(e.target.value ? { arch: e.target.value } : {})
                }
                sx={{ mb: 3, minWidth: 180 }}
            >
                <MenuItem value="">All architectures</MenuItem>
                <MenuItem value="x86_64">x86_64</MenuItem>
                <MenuItem value="aarch64">aarch64</MenuItem>
            </TextField>
            <QueryState query={q}>
                {(rows) => (
                    <ResourceTable
                        rows={rows}
                        rowKey={(i) => `${i.cloud_id}/${i.id}`}
                        search={(i) =>
                            `${i.id} ${i.name} ${i.os_name} ${i.os_version} ${i.arch}`
                        }
                        columns={[
                            {
                                id: "name",
                                label: "Image",
                                value: (i) => i.name,
                                render: (i) =>
                                    link(
                                        `${adminCloudURL(cloud.id)}images/${encodeURIComponent(i.id)}/`,
                                        i.name,
                                    ),
                            },
                            { id: "id", label: "ID", value: (i) => i.id },
                            {
                                id: "os",
                                label: "OS",
                                value: (i) => `${i.os_name} ${i.os_version}`,
                            },
                            {
                                id: "arch",
                                label: "Architecture",
                                value: (i) => i.arch,
                            },
                        ]}
                        empty="No images configured for this architecture."
                    />
                )}
            </QueryState>
        </>
    );
}
function ImageDetail({
    cloud,
    snapshot,
}: {
    cloud: AdminCloud;
    snapshot?: string;
}) {
    const { image = "" } = useParams();
    const q = useAdministration<AdminImage>(
        `${adminCloudPath(cloud.id)}/images/${encodeURIComponent(image)}`,
        true,
        snapshot,
    );
    return (
        <>
            <PageTitle
                title={image}
                description="Configured image definition"
                action={
                    <Button onClick={() => void q.refetch()}>
                        Refresh configuration
                    </Button>
                }
            />
            <QueryState query={q}>
                {(i) => (
                    <Stack spacing={3}>
                        <DetailSection title={i.name}>
                            <Fields
                                entries={[
                                    ["Cloud", i.cloud_id],
                                    ["ID", i.id],
                                    [
                                        "Operating system",
                                        `${i.os_name} ${i.os_version}`,
                                    ],
                                    ["Architecture", i.arch],
                                    [
                                        "Minimum memory",
                                        bytes(i.minimum_memory_bytes),
                                    ],
                                    [
                                        "Minimum disk",
                                        bytes(i.minimum_disk_bytes),
                                    ],
                                    [
                                        "Configured source protected",
                                        i.protected,
                                    ],
                                    ["Configured source hidden", i.hidden],
                                ]}
                            />
                        </DetailSection>
                        <DetailSection title="Configured guest defaults">
                            <Fields
                                entries={[
                                    ["Firmware", i.defaults.firmware],
                                    ["Disk bus", i.defaults.disk_bus],
                                    [
                                        "Disk controller",
                                        i.defaults.disk_controller,
                                    ],
                                    ["NIC model", i.defaults.nic_model],
                                    ["Graphics", i.defaults.graphics],
                                    ["Video model", i.defaults.video_model],
                                    ["Guest agent", i.defaults.guest_agent],
                                    [
                                        "Initialization",
                                        i.defaults.initialization,
                                    ],
                                ]}
                            />
                        </DetailSection>
                        <DetailSection title="Configured source">
                            <Fields
                                entries={[
                                    ["Mode", i.source.mode],
                                    ["Local path", i.source.path],
                                    [
                                        "Remote source",
                                        i.source.remote
                                            ? `${i.source.remote.scheme}://${i.source.remote.host}${i.source.remote.port ? `:${i.source.remote.port}` : ""} (redacted)`
                                            : undefined,
                                    ],
                                    ["SHA-256", i.source.sha256],
                                    ["Artifact format", i.source.format],
                                    [
                                        "Declared source size",
                                        i.source.size_bytes
                                            ? bytes(i.source.size_bytes)
                                            : "Unspecified (0 bytes)",
                                    ],
                                ]}
                            />
                        </DetailSection>
                        <DetailSection title="Default and inheritance state">
                            <ResourceTable
                                rows={i.settings}
                                rowKey={(s) => s.field}
                                search={(s) =>
                                    `${s.field} ${s.state} ${s.source}`
                                }
                                columns={[
                                    {
                                        id: "field",
                                        label: "Field",
                                        value: (s) => s.field,
                                    },
                                    {
                                        id: "state",
                                        label: "State",
                                        value: (s) => s.state,
                                    },
                                    {
                                        id: "source",
                                        label: "Source",
                                        value: (s) => s.source,
                                    },
                                ]}
                            />
                        </DetailSection>
                        {cloud.capabilities.image_availability && (
                            <Availability cloud={cloud.id} image={i.id} />
                        )}
                    </Stack>
                )}
            </QueryState>
        </>
    );
}
function Availability({ cloud, image }: { cloud: string; image: string }) {
    const q = useAdministration<ImageAvailability>(
        `${adminCloudPath(cloud)}/images/${encodeURIComponent(image)}/availability`,
    );
    return (
        <DetailSection
            title="Live image availability"
            description="Compatibility and source state are observed separately for each host."
        >
            <Button onClick={() => void q.refetch()} sx={{ mb: 2 }}>
                Refresh observations
            </Button>
            <QueryState query={q}>
                {(a) => (
                    <ResourceTable
                        rows={a.libvirt?.hosts || []}
                        rowKey={(h) => h.host_id}
                        search={(h) => `${h.host_id} ${h.code || ""}`}
                        columns={[
                            {
                                id: "host",
                                label: "Host",
                                value: (h) => h.host_id,
                            },
                            {
                                id: "observation",
                                label: "Observation",
                                value: (h) => h.observation,
                            },
                            {
                                id: "compatibility",
                                label: "Compatibility",
                                value: (h) => h.compatibility,
                            },
                            {
                                id: "source",
                                label: "Source state",
                                value: (h) => h.source_state,
                            },
                            {
                                id: "code",
                                label: "Diagnostic",
                                value: (h) => h.code || "—",
                            },
                        ]}
                    />
                )}
            </QueryState>
        </DetailSection>
    );
}
