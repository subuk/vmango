import { useMe } from "../app/Auth";
import { Box, Button, Stack } from "@mui/material";
import {
    adminCloudPath,
    adminCloudURL,
    useAdministration,
    type AdminCloud,
    type Installation,
} from "../api/administration";
import { PageTitle, QueryState, bytes } from "../components/common";
import { DetailSection } from "../components/Information";
import { ResourceTable } from "../components/ResourceTable";
import { Fields, link } from "./AdministrationFields";
const root = "/api/v1/administration";
export function InstallationPage() {
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
                    <Stack spacing={2}>
                        <DetailSection title="Runtime settings">
                            <Fields
                                entries={[
                                    ["Version", i.version],
                                    ["Snapshot ID", i.snapshot_id],
                                    ["Loaded at (UTC)", i.loaded_at],
                                    ["Activation", i.activation],
                                    ["Log level", i.log_level],
                                ]}
                            />
                        </DetailSection>
                        <Box
                            sx={{
                                display: "grid",
                                gridTemplateColumns: {
                                    xs: "1fr",
                                    md: "1fr 1fr",
                                },
                                gap: 2,
                            }}
                        >
                            <DetailSection title="Task execution">
                                <Fields
                                    entries={[
                                        ["Workers", i.tasks.workers],
                                        [
                                            "Retention (days)",
                                            i.tasks.retention_days,
                                        ],
                                    ]}
                                />
                            </DetailSection>
                            <DetailSection title="Image transfers">
                                <Fields
                                    entries={[
                                        [
                                            "Maximum size",
                                            bytes(i.image_transfers.max_bytes),
                                        ],
                                        [
                                            "Timeout (seconds)",
                                            i.image_transfers.timeout_seconds,
                                        ],
                                        [
                                            "Idle timeout (seconds)",
                                            i.image_transfers
                                                .idle_timeout_seconds,
                                        ],
                                    ]}
                                />
                            </DetailSection>
                        </Box>
                        <DetailSection title="Configuration catalogs">
                            <Stack
                                direction="row"
                                sx={{ gap: 3, flexWrap: "wrap" }}
                            >
                                {link(
                                    "/administration/clouds/",
                                    "Cloud configuration",
                                )}
                                {link(
                                    "/administration/configuration/namespaces/",
                                    "Namespace configuration",
                                )}
                            </Stack>
                        </DetailSection>
                    </Stack>
                )}
            </QueryState>
        </>
    );
}
export function Clouds({ snapshot }: { snapshot?: string }) {
    const principal = useMe()?.id;
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
                        paramPrefix="clouds."
                        preferenceKey={`${principal}:administration-clouds`}
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
                paramPrefix={`${title.toLowerCase().replaceAll(" ", "-")}.`}
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
export function CloudConfiguration({
    cloud,
    snapshot,
}: {
    cloud: AdminCloud;
    snapshot?: string;
}) {
    const principal = useMe()?.id;
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
                    <Stack spacing={2}>
                        <DetailSection title="Cloud policy">
                            <Fields
                                entries={[
                                    ["Cloud ID", c.id],
                                    ["Backend", c.backend_kind],
                                    [
                                        "Host inventory",
                                        c.capabilities.host_inventory,
                                    ],
                                    [
                                        "Image availability",
                                        c.capabilities.image_availability,
                                    ],
                                ]}
                            />
                            <Stack
                                direction="row"
                                sx={{ gap: 2, flexWrap: "wrap", mt: 1.5 }}
                            >
                                {c.namespace_ids?.map((id) => (
                                    <Box key={id}>
                                        {link(
                                            `/administration/configuration/namespaces/${encodeURIComponent(id)}/`,
                                            id,
                                        )}
                                    </Box>
                                ))}
                            </Stack>
                        </DetailSection>
                        <Catalog title="Zones" rows={c.zones} />
                        <DetailSection title="Instance types">
                            <ResourceTable
                                rows={c.instance_types}
                                paramPrefix="instance-types."
                                preferenceKey={`${principal}:administration-instance-types:${cloud.id}`}
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
                                        defaultHidden: true,
                                        value: (t) =>
                                            t.hugepages
                                                ? "Enabled"
                                                : "Disabled",
                                    },
                                    {
                                        id: "autostart",
                                        label: "Autostart",
                                        defaultHidden: true,
                                        value: (t) =>
                                            t.autostart
                                                ? "Enabled"
                                                : "Disabled",
                                    },
                                ]}
                            />
                        </DetailSection>
                        <Catalog title="Logical networks" rows={c.networks} />
                        <DetailSection title="Storage classes">
                            <ResourceTable
                                rows={c.storage_classes}
                                paramPrefix="storage-classes."
                                preferenceKey={`${principal}:administration-storage-classes:${cloud.id}`}
                                rowKey={(s) => s.id}
                                search={(s) =>
                                    `${s.id} ${s.name} ${s.description || ""} ${s.clone_mode}`
                                }
                                columns={[
                                    {
                                        id: "name",
                                        label: "Name",
                                        value: (s) => s.name,
                                    },
                                    {
                                        id: "id",
                                        label: "ID",
                                        value: (s) => s.id,
                                    },
                                    {
                                        id: "description",
                                        label: "Description",
                                        value: (s) => s.description || "—",
                                    },
                                    {
                                        id: "clone_mode",
                                        label: "Clone mode",
                                        value: (s) => s.clone_mode || "copy",
                                    },
                                ]}
                            />
                        </DetailSection>
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
                                    `${adminCloudURL(c.id)}hosts/`,
                                    "Open hosts and configuration",
                                )}
                            </DetailSection>
                        )}
                    </Stack>
                )}
            </QueryState>
        </>
    );
}
