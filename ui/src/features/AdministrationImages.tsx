import { ImageHardwareProfile } from "./HardwareProfiles";
import {
    Box,
    Button,
    Chip,
    MenuItem,
    Stack,
    Tab,
    Tabs,
    Table,
    TableBody,
    TableCell,
    TableContainer,
    TableHead,
    TableRow,
    TextField,
    Typography,
} from "@mui/material";
import { useLocation, useParams, useSearchParams } from "react-router";
import { useQuery } from "@tanstack/react-query";
import { useMe } from "../app/Auth";
import { request } from "../api/client";
import {
    adminCloudPath,
    adminCloudURL,
    useAdministration,
    type AdminCloud,
    type AdminImage,
    type ImageAvailability,
} from "../api/administration";
import { PageTitle, QueryState, bytes } from "../components/common";
import { DetailSection } from "../components/Information";
import { ResourceTable } from "../components/ResourceTable";
import { Fields, link } from "./AdministrationFields";

export function Images({
    cloud,
    snapshot,
}: {
    cloud: AdminCloud;
    snapshot?: string;
}) {
    const principal = useMe()?.id;
    const [params, setParams] = useSearchParams();
    const location = useLocation();
    const arch = params.get("arch") || "";
    const matrix =
        cloud.capabilities.image_availability &&
        params.get("view") === "availability";
    const q = useAdministration<AdminImage[]>(
        `${adminCloudPath(cloud.id)}/images`,
        true,
        snapshot,
    );
    const update = (key: string, value: string) =>
        setParams(
            (old) => {
                const next = new URLSearchParams(old);
                value ? next.set(key, value) : next.delete(key);
                next.delete("availabilityPage");
                return next;
            },
            { replace: true },
        );
    return (
        <>
            <PageTitle
                title="Images"
                description="Configured image catalog. Host observations report compatibility and source state independently."
                action={
                    <Button onClick={() => void q.refetch()}>
                        Refresh configuration
                    </Button>
                }
            />
            {cloud.capabilities.image_availability && (
                <Tabs
                    value={matrix ? "availability" : "catalog"}
                    onChange={(_, value) =>
                        update("view", value === "catalog" ? "" : value)
                    }
                    aria-label="Image view"
                    sx={{ mb: 2 }}
                >
                    <Tab value="catalog" label="Catalog" />
                    <Tab value="availability" label="Host availability" />
                </Tabs>
            )}
            <QueryState query={q}>
                {(rows) => (
                    <>
                        <TextField
                            select
                            size="small"
                            label="Architecture"
                            value={arch}
                            onChange={(e) => update("arch", e.target.value)}
                            sx={{ mb: 2, minWidth: 190 }}
                            slotProps={{
                                inputLabel: { shrink: true },
                                select: { displayEmpty: true },
                            }}
                        >
                            <MenuItem value="">All architectures</MenuItem>
                            {[...new Set(rows.map((i) => i.arch))]
                                .sort()
                                .map((a) => (
                                    <MenuItem key={a} value={a}>
                                        {a}
                                    </MenuItem>
                                ))}
                        </TextField>
                        {matrix ? (
                            <ImageAvailabilityMatrix
                                cloud={cloud}
                                images={rows.filter(
                                    (i) => !arch || i.arch === arch,
                                )}
                            />
                        ) : (
                            <ResourceTable
                                rows={rows.filter(
                                    (i) => !arch || i.arch === arch,
                                )}
                                rowKey={(i) => i.id}
                                preferenceKey={`${principal}:administration-images:${cloud.id}`}
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
                                                `${adminCloudURL(cloud.id)}images/${encodeURIComponent(i.id)}/${location.search}`,
                                                i.name,
                                            ),
                                    },
                                    {
                                        id: "id",
                                        label: "ID",
                                        defaultHidden: true,
                                        value: (i) => i.id,
                                    },
                                    {
                                        id: "os",
                                        label: "Operating system",
                                        value: (i) =>
                                            `${i.os_name} ${i.os_version}`,
                                    },
                                    {
                                        id: "arch",
                                        label: "Architecture",
                                        value: (i) => i.arch,
                                    },
                                    {
                                        id: "source",
                                        label: "Configured source",
                                        value: (i) => i.source.mode,
                                    },
                                    {
                                        id: "memory",
                                        label: "Minimum memory",
                                        defaultHidden: true,
                                        value: (i) => i.minimum_memory_bytes,
                                        render: (i) =>
                                            bytes(i.minimum_memory_bytes),
                                    },
                                    {
                                        id: "disk",
                                        label: "Minimum disk",
                                        defaultHidden: true,
                                        value: (i) => i.minimum_disk_bytes,
                                        render: (i) =>
                                            bytes(i.minimum_disk_bytes),
                                    },
                                ]}
                                empty="No images configured for this architecture."
                            />
                        )}
                    </>
                )}
            </QueryState>
        </>
    );
}

type AvailabilityResult = {
    image: string;
    data?: ImageAvailability;
    error?: string;
};
const matrixPageSize = 8;

/** One observation request returns all hosts; only the current image page is loaded. */
export async function loadImageAvailability(
    cloud: string,
    images: string[],
    signal: AbortSignal,
): Promise<AvailabilityResult[]> {
    const results: AvailabilityResult[] = [];
    let cursor = 0;
    await Promise.all(
        Array.from({ length: Math.min(3, images.length) }, async () => {
            while (cursor < images.length && !signal.aborted) {
                const image = images[cursor++];
                try {
                    const data = await request<ImageAvailability>(
                        `${adminCloudPath(cloud)}/images/${encodeURIComponent(image)}/availability`,
                        "GET",
                        undefined,
                        signal,
                    );
                    results.push({ image, data });
                } catch (error) {
                    if (signal.aborted) throw error;
                    results.push({
                        image,
                        error:
                            error instanceof Error
                                ? error.message
                                : "Observation failed",
                    });
                }
            }
        }),
    );
    return results;
}

function ImageAvailabilityMatrix({
    cloud,
    images,
}: {
    cloud: AdminCloud;
    images: AdminImage[];
}) {
    const me = useMe();
    const [params, setParams] = useSearchParams();
    const location = useLocation();
    const search = params.get("availabilitySearch") || "";
    const matches = images.filter((i) =>
        `${i.id} ${i.name}`.toLowerCase().includes(search.toLowerCase()),
    );
    const pages = Math.max(1, Math.ceil(matches.length / matrixPageSize));
    const page = Math.min(
        pages - 1,
        Math.max(0, Math.floor(Number(params.get("availabilityPage")) || 0)),
    );
    const visible = matches.slice(
        page * matrixPageSize,
        (page + 1) * matrixPageSize,
    );
    const ids = visible.map((i) => i.id);
    const query = useQuery({
        queryKey: ["administration", me?.id, "image-matrix", cloud.id, ids],
        queryFn: ({ signal }) => loadImageAvailability(cloud.id, ids, signal),
        enabled: cloud.capabilities.image_availability && ids.length > 0,
        refetchInterval: false,
        refetchOnWindowFocus: false,
    });
    const hosts = [
        ...new Set([
            ...(cloud.libvirt?.hosts.map((h) => h.id) || []),
            ...(query.data || []).flatMap(
                (r) => r.data?.libvirt?.hosts.map((h) => h.host_id) || [],
            ),
        ]),
    ].sort();
    const update = (key: string, value: string) =>
        setParams(
            (old) => {
                const next = new URLSearchParams(old);
                value ? next.set(key, value) : next.delete(key);
                if (key === "availabilitySearch")
                    next.delete("availabilityPage");
                return next;
            },
            { replace: true },
        );
    return (
        <DetailSection
            title="Image availability by host"
            description="Live observations for this page only. Each cell separates observation, compatibility and source; unknown does not mean ready."
        >
            <Stack
                direction="row"
                sx={{ gap: 2, alignItems: "center", mb: 2, flexWrap: "wrap" }}
            >
                <TextField
                    size="small"
                    label="Search availability images"
                    value={search}
                    onChange={(e) =>
                        update("availabilitySearch", e.target.value)
                    }
                />
                <Button
                    disabled={query.isFetching || !ids.length}
                    onClick={() => void query.refetch()}
                >
                    Refresh observations
                </Button>
                <Typography variant="body2" color="text.secondary">
                    {query.isFetching
                        ? "Observing current page…"
                        : query.dataUpdatedAt
                          ? `Updated ${new Date(query.dataUpdatedAt).toLocaleTimeString()}`
                          : "No observations yet"}
                </Typography>
            </Stack>
            <TableContainer
                tabIndex={0}
                aria-label="Scrollable image availability matrix"
            >
                <Table size="small" aria-label="Image availability matrix">
                    <TableHead>
                        <TableRow>
                            <TableCell>Image / host</TableCell>
                            {hosts.map((h) => (
                                <TableCell key={h}>
                                    {link(
                                        `${adminCloudURL(cloud.id)}hosts/${encodeURIComponent(h)}/`,
                                        h,
                                    )}
                                </TableCell>
                            ))}
                        </TableRow>
                    </TableHead>
                    <TableBody>
                        {visible.map((i) => {
                            const result = query.data?.find(
                                (r) => r.image === i.id,
                            );
                            return (
                                <TableRow key={i.id}>
                                    <TableCell sx={{ minWidth: 150 }}>
                                        {link(
                                            `${adminCloudURL(cloud.id)}images/${encodeURIComponent(i.id)}/${location.search}`,
                                            i.name,
                                        )}
                                        <Typography
                                            variant="caption"
                                            component="div"
                                            color="text.secondary"
                                        >
                                            {i.arch}
                                        </Typography>
                                    </TableCell>
                                    {hosts.map((host) => {
                                        const observation =
                                            result?.data?.libvirt?.hosts.find(
                                                (h) => h.host_id === host,
                                            );
                                        return (
                                            <TableCell
                                                key={host}
                                                sx={{
                                                    minWidth: 180,
                                                    verticalAlign: "top",
                                                }}
                                            >
                                                <Chip
                                                    size="small"
                                                    variant="outlined"
                                                    label={
                                                        result?.error
                                                            ? "Unavailable"
                                                            : observation?.observation ||
                                                              "Unknown"
                                                    }
                                                    color={
                                                        result?.error ||
                                                        observation?.observation ===
                                                            "unavailable"
                                                            ? "warning"
                                                            : "default"
                                                    }
                                                />
                                                <Typography
                                                    variant="caption"
                                                    component="div"
                                                    sx={{ mt: 0.5 }}
                                                >
                                                    Compatibility:{" "}
                                                    {observation?.compatibility ||
                                                        "unknown"}
                                                </Typography>
                                                <Typography
                                                    variant="caption"
                                                    component="div"
                                                >
                                                    Source:{" "}
                                                    {observation?.source_state ||
                                                        "unknown"}
                                                </Typography>
                                                {(result?.error ||
                                                    observation?.code) && (
                                                    <Typography
                                                        variant="caption"
                                                        component="div"
                                                        color="text.secondary"
                                                    >
                                                        {result?.error ||
                                                            observation?.code}
                                                    </Typography>
                                                )}
                                            </TableCell>
                                        );
                                    })}
                                </TableRow>
                            );
                        })}
                    </TableBody>
                </Table>
            </TableContainer>
            {!visible.length && (
                <Typography sx={{ py: 2 }}>No matching images.</Typography>
            )}
            {!hosts.length && (
                <Typography sx={{ py: 2 }}>
                    No host observations available.
                </Typography>
            )}
            <Stack direction="row" sx={{ mt: 2, alignItems: "center", gap: 1 }}>
                <Typography variant="body2" sx={{ mr: "auto" }}>
                    Showing {visible.length} of {matches.length} images · Page{" "}
                    {page + 1} of {pages}
                </Typography>
                <Button
                    disabled={page === 0}
                    onClick={() => update("availabilityPage", String(page - 1))}
                >
                    Previous images
                </Button>
                <Button
                    disabled={page + 1 >= pages}
                    onClick={() => update("availabilityPage", String(page + 1))}
                >
                    Next images
                </Button>
            </Stack>
        </DetailSection>
    );
}

export function ImageDetail({
    cloud,
    snapshot,
}: {
    cloud: AdminCloud;
    snapshot?: string;
}) {
    const { image = "" } = useParams();
    const location = useLocation();
    const listParams = new URLSearchParams(location.search);
    let hasDetailFilters = false;
    for (const key of [...listParams.keys()]) {
        if (key.startsWith("inheritance.") || key.startsWith("availability.")) {
            listParams.delete(key);
            hasDetailFilters = true;
        }
    }
    const listSearch = hasDetailFilters
        ? listParams.size
            ? `?${listParams}`
            : ""
        : location.search;
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
                    <Stack
                        direction="row"
                        sx={{ alignItems: "center", gap: 2 }}
                    >
                        {link(
                            `${adminCloudURL(cloud.id)}images/${listSearch}`,
                            "All images",
                        )}
                        <Button onClick={() => void q.refetch()}>
                            Refresh configuration
                        </Button>
                    </Stack>
                }
            />
            <QueryState query={q}>
                {(i) => (
                    <Stack spacing={2}>
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
                        {cloud.backend_kind === "libvirt" && (
                            <ImageHardwareProfile
                                cloud={cloud.id}
                                image={i.id}
                                snapshot={snapshot}
                            />
                        )}
                        {!!i.data_disks?.length && (
                            <DetailSection title="Required image disks">
                                <Typography sx={{ mb: 2 }}>
                                    Created automatically for new machines using
                                    the selected storage class.
                                </Typography>
                                <Fields
                                    entries={i.data_disks.map((disk) => [
                                        disk.name,
                                        bytes(disk.size_bytes),
                                    ])}
                                />
                            </DetailSection>
                        )}
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
                                    ["Archive format", i.source.archive_format],
                                    ["Archive member", i.source.archive_member],
                                    [
                                        "Declared extracted size",
                                        i.source.extracted_size_bytes
                                            ? bytes(
                                                  i.source.extracted_size_bytes,
                                              )
                                            : undefined,
                                    ],
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
                                paramPrefix="inheritance."
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
                        paramPrefix="availability."
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
