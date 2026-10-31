import {
    Box,
    Breadcrumbs,
    Button,
    Link,
    Stack,
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableRow,
    Typography,
} from "@mui/material";
import { Link as RouterLink, useParams } from "react-router";
import {
    esc,
    machineURL,
    type Network,
    type Node,
    type Pool,
} from "../api/hooks";
import {
    adminCloudPath,
    adminCloudURL,
    useAdministration,
} from "../api/administration";
import type { components } from "../api/schema";
import { Capacity, DetailSection, Summary } from "../components/Information";
import { ResourceTable } from "../components/ResourceTable";
import { APIError } from "../api/client";
import { bytes, PageTitle, QueryState } from "../components/common";

export function NodeDetail() {
    const { node = "", cloud = "" } = useParams();
    const base = `${adminCloudPath(cloud)}/hosts/${esc(node)}`;
    const q = useAdministration<Node>(base);
    const pools = useAdministration<Pool[]>(`${base}/pools`);
    const networks = useAdministration<Network[]>(`${base}/networks`);
    const capabilities = useAdministration<
        components["schemas"]["ApiCapabilities"]
    >(`${base}/capabilities`);
    const catalog = useAdministration<components["schemas"]["HostCatalog"]>(
        `${base}/catalog`,
    );
    if (q.error instanceof APIError && [403, 404].includes(q.error.status))
        return <QueryState query={q}>{() => null}</QueryState>;
    return (
        <>
            <Breadcrumbs sx={{ mb: 2 }}>
                <Link
                    component={RouterLink}
                    to={`${adminCloudURL(cloud)}hosts/`}
                >
                    Hosts
                </Link>
                <Typography>{node}</Typography>
            </Breadcrumbs>
            <PageTitle
                title={node}
                description="Live host hardware, resource topology and backend diagnostics."
                action={
                    <Button
                        onClick={() => {
                            void q.refetch();
                            void pools.refetch();
                            void networks.refetch();
                            void capabilities.refetch();
                            void catalog.refetch();
                        }}
                    >
                        Refresh observations
                    </Button>
                }
            />
            <Box sx={{ mb: 3 }}>
                <Link
                    component={RouterLink}
                    to={`${adminCloudURL(cloud)}configuration/hosts/${esc(node)}/`}
                >
                    Open configured host settings
                </Link>
            </Box>
            <QueryState query={q}>
                {(n) => (
                    <Stack spacing={3}>
                        <Summary
                            label="Host summary"
                            items={[
                                {
                                    label: "Host CPUs",
                                    value: n.cpus.length,
                                    detail: `${n.threads_per_core || "—"} threads per core`,
                                },
                                {
                                    label: "Installed memory",
                                    value: bytes(n.memory_bytes),
                                    detail: "Physical host memory",
                                },
                                {
                                    label: "NUMA cells",
                                    value: n.numa.length,
                                    detail: "Memory locality domains",
                                },
                                {
                                    label: "Architecture",
                                    value: n.arch,
                                    detail: n.iommu
                                        ? "IOMMU available"
                                        : "IOMMU unavailable",
                                },
                            ]}
                        />
                        <DetailSection
                            title="Live diagnostics"
                            description="Hardware identity and virtualization capabilities."
                        >
                            <Box component="dl" className="details">
                                <dt>Hostname</dt>
                                <dd>{n.hostname}</dd>
                                <dt>Architecture</dt>
                                <dd>{n.arch}</dd>
                                <dt>CPU</dt>
                                <dd>
                                    {n.cpu_vendor} {n.cpu_model} {n.cpu_info}
                                </dd>
                                <dt>Memory</dt>
                                <dd>{bytes(n.memory_bytes)}</dd>
                                <dt>Threads per core</dt>
                                <dd>{n.threads_per_core}</dd>
                                <dt>IOMMU</dt>
                                <dd>{n.iommu ? "Available" : "Unavailable"}</dd>
                            </Box>
                        </DetailSection>
                        <DetailSection
                            title="CPU topology"
                            description="Sockets, cores and virtual machine CPU assignments."
                        >
                            <Box className="table-scroll">
                                <Table size="small">
                                    <TableHead>
                                        <TableRow>
                                            {[
                                                "CPU ID",
                                                "Socket",
                                                "Core",
                                                "NUMA",
                                                "Pins",
                                            ].map((s) => (
                                                <TableCell key={s}>
                                                    {s}
                                                </TableCell>
                                            ))}
                                        </TableRow>
                                    </TableHead>
                                    <TableBody>
                                        {n.cpus.map((cpu) => (
                                            <TableRow key={cpu.id}>
                                                <TableCell>{cpu.id}</TableCell>
                                                <TableCell>
                                                    {cpu.socket}
                                                </TableCell>
                                                <TableCell>
                                                    {cpu.core}
                                                </TableCell>
                                                <TableCell>
                                                    {cpu.numa}
                                                </TableCell>
                                                <TableCell>
                                                    {cpu.pins.map((pin, i) => (
                                                        <div key={i}>
                                                            {cloud ? (
                                                                <Link
                                                                    component={
                                                                        RouterLink
                                                                    }
                                                                    to={machineURL(
                                                                        cloud,
                                                                        pin.vm,
                                                                    )}
                                                                >
                                                                    {pin.vm}
                                                                </Link>
                                                            ) : (
                                                                pin.vm
                                                            )}{" "}
                                                            {pin.description}
                                                        </div>
                                                    ))}
                                                </TableCell>
                                            </TableRow>
                                        ))}
                                    </TableBody>
                                </Table>
                            </Box>
                        </DetailSection>
                        <DetailSection
                            title="NUMA and hugepages"
                            description="Memory by cell and available page allocations."
                        >
                            <Box className="table-scroll">
                                <Table size="small">
                                    <TableHead>
                                        <TableRow>
                                            {[
                                                "Cell ID",
                                                "Memory",
                                                "Free / total 4 KiB pages",
                                                "Free / total 2 MiB pages",
                                                "Free / total 1 GiB pages",
                                            ].map((s) => (
                                                <TableCell key={s}>
                                                    {s}
                                                </TableCell>
                                            ))}
                                        </TableRow>
                                    </TableHead>
                                    <TableBody>
                                        {n.numa.map((cell) => (
                                            <TableRow key={cell.id}>
                                                <TableCell>{cell.id}</TableCell>
                                                <TableCell>
                                                    {bytes(cell.memory_bytes)}
                                                </TableCell>
                                                <TableCell>
                                                    {cell.free_pages_4k} /{" "}
                                                    {cell.pages_4k}
                                                </TableCell>
                                                <TableCell>
                                                    {cell.free_pages_2m} /{" "}
                                                    {cell.pages_2m}
                                                </TableCell>
                                                <TableCell>
                                                    {cell.free_pages_1g} /{" "}
                                                    {cell.pages_1g}
                                                </TableCell>
                                            </TableRow>
                                        ))}
                                    </TableBody>
                                </Table>
                            </Box>
                        </DetailSection>
                    </Stack>
                )}
            </QueryState>
            <Box sx={{ mt: 3 }}>
                <DetailSection
                    title="Backend networks (read-only diagnostics)"
                    description="Networks exposed by libvirt on this host."
                >
                    <QueryState query={networks}>
                        {(rows) => (
                            <ResourceTable
                                rows={rows}
                                rowKey={(n) => n.name}
                                search={(n) => n.name}
                                columns={[
                                    {
                                        id: "name",
                                        label: "Backend network",
                                        value: (n) => n.name,
                                    },
                                ]}
                                empty="No backend networks are available on this host."
                            />
                        )}
                    </QueryState>
                </DetailSection>
            </Box>
            <Box sx={{ mt: 3 }}>
                <DetailSection
                    title="Storage pools"
                    description="Host storage capacity, usage and available space."
                >
                    <QueryState query={pools}>
                        {(rows) => (
                            <Box className="table-scroll">
                                <Table size="small">
                                    <TableHead>
                                        <TableRow>
                                            {[
                                                "Pool",
                                                "Size",
                                                "Used",
                                                "Free",
                                                "Capacity",
                                            ].map((s) => (
                                                <TableCell key={s}>
                                                    {s}
                                                </TableCell>
                                            ))}
                                        </TableRow>
                                    </TableHead>
                                    <TableBody>
                                        {rows.length === 0 && (
                                            <TableRow>
                                                <TableCell colSpan={5}>
                                                    No storage pools are
                                                    available on this host.
                                                </TableCell>
                                            </TableRow>
                                        )}
                                        {rows.map((p) => (
                                            <TableRow key={p.name}>
                                                <TableCell>{p.name}</TableCell>
                                                <TableCell>
                                                    {bytes(p.size_bytes)}
                                                </TableCell>
                                                <TableCell>
                                                    {bytes(p.used_bytes)}
                                                </TableCell>
                                                <TableCell
                                                    sx={{ fontWeight: 600 }}
                                                >
                                                    {bytes(p.free_bytes)}
                                                </TableCell>
                                                <TableCell>
                                                    <Capacity
                                                        used={p.used_bytes}
                                                        total={p.size_bytes}
                                                        label={`${p.name} storage usage`}
                                                    />
                                                </TableCell>
                                            </TableRow>
                                        ))}
                                    </TableBody>
                                </Table>
                            </Box>
                        )}
                    </QueryState>
                </DetailSection>
            </Box>
            <Box sx={{ mt: 3 }}>
                <DetailSection title="Live launch capabilities">
                    <QueryState query={capabilities}>
                        {(c) => (
                            <Box component="dl" className="details">
                                {Object.entries(c).map(([key, value]) => (
                                    <Box
                                        component="div"
                                        key={key}
                                        sx={{ display: "contents" }}
                                    >
                                        <dt>{key.replaceAll("_", " ")}</dt>
                                        <dd>
                                            {Array.isArray(value)
                                                ? value.join(", ")
                                                : typeof value === "object"
                                                  ? Object.entries(value || {})
                                                        .map(
                                                            ([id, models]) =>
                                                                `${id}: ${(models as string[]).join(", ")}`,
                                                        )
                                                        .join("; ")
                                                  : String(value)}
                                        </dd>
                                    </Box>
                                ))}
                            </Box>
                        )}
                    </QueryState>
                </DetailSection>
            </Box>
            <Box sx={{ mt: 3 }}>
                <DetailSection title="Live logical catalog">
                    <QueryState query={catalog}>
                        {(c) => (
                            <Box component="dl" className="details">
                                <dt>Networks</dt>
                                <dd>
                                    {(c.networks || [])
                                        .map((n) => n.name)
                                        .join(", ") || "None"}
                                </dd>
                                <dt>Storage classes</dt>
                                <dd>
                                    {(c.storage_classes || [])
                                        .map((s) => s.name)
                                        .join(", ") || "None"}
                                </dd>
                            </Box>
                        )}
                    </QueryState>
                </DetailSection>
            </Box>
        </>
    );
}
