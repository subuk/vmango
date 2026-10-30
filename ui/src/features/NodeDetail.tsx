import {
    Box,
    Breadcrumbs,
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
    useResource,
    type Network,
    type Node,
    type Pool,
} from "../api/hooks";
import { ResourceTable } from "../components/ResourceTable";
import { bytes, PageTitle, QueryState } from "../components/common";

export function NodeDetail() {
    const { node = "" } = useParams();
    const q = useResource<Node>(`/api/v1/hosts/${esc(node)}`);
    const pools = useResource<Pool[]>(`/api/v1/hosts/${esc(node)}/pools`);
    const networks = useResource<Network[]>(
        `/api/v1/hosts/${esc(node)}/networks`,
    );
    return (
        <>
            <Breadcrumbs sx={{ mb: 2 }}>
                <Link component={RouterLink} to="/nodes/">
                    Hosts
                </Link>
                <Typography>{node}</Typography>
            </Breadcrumbs>
            <PageTitle title={node} />
            <QueryState query={q}>
                {(n) => (
                    <Stack spacing={4}>
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
                        <Box>
                            <Typography variant="h2" sx={{ mb: 2 }}>
                                CPU topology
                            </Typography>
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
                                                            <Link
                                                                component={
                                                                    RouterLink
                                                                }
                                                                to={machineURL(
                                                                    node,
                                                                    pin.vm,
                                                                )}
                                                            >
                                                                {pin.vm}
                                                            </Link>{" "}
                                                            {pin.description}
                                                        </div>
                                                    ))}
                                                </TableCell>
                                            </TableRow>
                                        ))}
                                    </TableBody>
                                </Table>
                            </Box>
                        </Box>
                        <Box>
                            <Typography variant="h2" sx={{ mb: 2 }}>
                                NUMA and hugepages
                            </Typography>
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
                        </Box>
                    </Stack>
                )}
            </QueryState>
            <Box sx={{ mt: 4 }}>
                <Typography variant="h2" sx={{ mb: 2 }}>
                    Backend networks (read-only diagnostics)
                </Typography>
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
            </Box>
            <Box sx={{ mt: 4 }}>
                <Typography variant="h2" sx={{ mb: 2 }}>
                    Storage pools
                </Typography>
                <QueryState query={pools}>
                    {(rows) => (
                        <Box className="table-scroll">
                            <Table size="small">
                                <TableHead>
                                    <TableRow>
                                        {["Pool", "Size", "Used", "Free"].map(
                                            (s) => (
                                                <TableCell key={s}>
                                                    {s}
                                                </TableCell>
                                            ),
                                        )}
                                    </TableRow>
                                </TableHead>
                                <TableBody>
                                    {rows.map((p) => (
                                        <TableRow key={p.name}>
                                            <TableCell>{p.name}</TableCell>
                                            <TableCell>
                                                {bytes(p.size_bytes)}
                                            </TableCell>
                                            <TableCell>
                                                {bytes(p.used_bytes)}
                                            </TableCell>
                                            <TableCell>
                                                {bytes(p.free_bytes)}
                                            </TableCell>
                                        </TableRow>
                                    ))}
                                </TableBody>
                            </Table>
                        </Box>
                    )}
                </QueryState>
            </Box>
        </>
    );
}
