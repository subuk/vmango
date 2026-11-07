import { useState } from "react";
import { WorkspaceLink as RouterLink } from "../app/Workspace";
import {
    Alert,
    Box,
    Button,
    Link,
    Stack,
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableRow,
} from "@mui/material";
import { esc, type Node } from "../api/hooks";
import {
    adminCloudPath,
    adminCloudURL,
    useAdministration,
} from "../api/administration";
import type { components } from "../api/schema";
import { DetailSection, Summary } from "../components/Information";
import { bytes, QueryState } from "../components/common";

/** Mounted only when the host diagnostics tab is selected. */
export function NodeDetail({ cloud, node }: { cloud: string; node: string }) {
    const base = `${adminCloudPath(cloud)}/hosts/${esc(node)}`;
    const q = useAdministration<Node>(base);
    const [expanded, setExpanded] = useState(false);
    const capabilities = useAdministration<
        components["schemas"]["ApiCapabilities"]
    >(`${base}/capabilities`, expanded && q.data?.id === node);
    const catalog = useAdministration<components["schemas"]["HostCatalog"]>(
        `${base}/catalog`,
        expanded && q.data?.id === node,
    );
    return (
        <Stack spacing={3}>
            <Alert severity="info">
                Live observations describe the backend now. Accepted
                configuration remains available in the other tabs.
            </Alert>
            <Button onClick={() => void q.refetch()}>
                Refresh observations
            </Button>
            <QueryState query={q}>
                {(n) =>
                    n.id !== node ? (
                        <Alert severity="error">
                            The observed host does not match this host.
                        </Alert>
                    ) : (
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
                                        {n.cpu_vendor} {n.cpu_model}{" "}
                                        {n.cpu_info}
                                    </dd>
                                    <dt>Memory</dt>
                                    <dd>{bytes(n.memory_bytes)}</dd>
                                    <dt>Threads per core</dt>
                                    <dd>{n.threads_per_core}</dd>
                                    <dt>IOMMU</dt>
                                    <dd>
                                        {n.iommu ? "Available" : "Unavailable"}
                                    </dd>
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
                                                    <TableCell>
                                                        {cpu.id}
                                                    </TableCell>
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
                                                        {cpu.pins.map(
                                                            (pin, i) => (
                                                                <div key={i}>
                                                                    {cloud ? (
                                                                        <Link
                                                                            component={
                                                                                RouterLink
                                                                            }
                                                                            to={`${adminCloudURL(cloud)}machines/${esc(pin.vm)}/`}
                                                                        >
                                                                            {
                                                                                pin.vm
                                                                            }
                                                                        </Link>
                                                                    ) : (
                                                                        pin.vm
                                                                    )}{" "}
                                                                    {
                                                                        pin.description
                                                                    }
                                                                </div>
                                                            ),
                                                        )}
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
                                                    <TableCell>
                                                        {cell.id}
                                                    </TableCell>
                                                    <TableCell>
                                                        {bytes(
                                                            cell.memory_bytes,
                                                        )}
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
                    )
                }
            </QueryState>
            {q.data?.id === node && (
                <Button
                    aria-expanded={expanded}
                    onClick={() => setExpanded(!expanded)}
                >
                    {expanded ? "Hide" : "Load"} launch capabilities and logical
                    catalog
                </Button>
            )}
            {expanded && q.data?.id === node && (
                <>
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
                                                      ? JSON.stringify(value)
                                                      : String(value)}
                                            </dd>
                                        </Box>
                                    ))}
                                </Box>
                            )}
                        </QueryState>
                    </DetailSection>
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
                </>
            )}
        </Stack>
    );
}
