import type { ReactNode } from "react";
import {
    Box,
    Button,
    MenuItem,
    Stack,
    Table,
    TableBody,
    TableCell,
    TableContainer,
    TableHead,
    TableRow,
    TableSortLabel,
    TextField,
    Typography,
} from "@mui/material";
import { useSearchParams } from "react-router";
import { useNodes } from "../api/hooks";

export type Column<T> = {
    id: string;
    label: string;
    value: (row: T) => string | number;
    render?: (row: T) => ReactNode;
    numeric?: boolean;
};
export function ResourceTable<T>({
    rows,
    columns,
    rowKey,
    search,
    node,
    state,
    pool,
    poolLabel = "Pool",
    actions,
    empty = "No resources yet.",
}: {
    rows: T[];
    columns: Column<T>[];
    rowKey: (row: T) => string;
    search: (row: T) => string;
    node?: (row: T) => string;
    state?: (row: T) => string;
    pool?: (row: T) => string;
    poolLabel?: string;
    actions?: (row: T) => ReactNode;
    empty?: string;
}) {
    const [params, setParams] = useSearchParams();
    const nodes = useNodes(!!node);
    const update = (key: string, value: string) =>
        setParams(
            (old) => {
                const p = new URLSearchParams(old);
                value ? p.set(key, value) : p.delete(key);
                return p;
            },
            { replace: true },
        );
    const query = params.get("q") || "";
    const selectedNode = params.get("node") || "";
    const selectedState = params.get("state") || "";
    const selectedPool = params.get("pool") || "";
    const sort = params.get("sort") || columns[0].id;
    const desc = params.get("direction") === "desc";
    const column = columns.find((c) => c.id === sort) || columns[0];
    const filtered = rows.filter(
        (r) =>
            (!query ||
                search(r)
                    .toLocaleLowerCase()
                    .includes(query.toLocaleLowerCase())) &&
            (!node || !selectedNode || node(r) === selectedNode) &&
            (!state || !selectedState || state(r) === selectedState) &&
            (!pool || !selectedPool || pool(r) === selectedPool),
    );
    filtered.sort((a, b) => {
        const x = column.value(a);
        const y = column.value(b);
        const order =
            typeof x === "number" && typeof y === "number"
                ? x - y
                : String(x).localeCompare(String(y), undefined, {
                      numeric: true,
                  });
        return (desc ? -order : order) || rowKey(a).localeCompare(rowKey(b));
    });
    const active = query || selectedNode || selectedState || selectedPool;
    return (
        <>
            <Stack
                className="filter-toolbar"
                direction="row"
                sx={{ gap: 2, flexWrap: "wrap", mb: 2 }}
            >
                <TextField
                    label="Search"
                    value={query}
                    onChange={(e) => update("q", e.target.value)}
                    sx={{ maxWidth: 300 }}
                />
                {node && (
                    <TextField
                        select
                        label="Host"
                        slotProps={{
                            select: { displayEmpty: true },
                            inputLabel: { shrink: true },
                        }}
                        value={selectedNode}
                        onChange={(e) => update("node", e.target.value)}
                        sx={{ width: 210 }}
                    >
                        <MenuItem value="">All hosts</MenuItem>
                        {[
                            ...new Set([
                                ...(nodes.data || []).map((n) => n.id),
                                ...rows.map(node),
                            ]),
                        ].map((n) => (
                            <MenuItem key={n} value={n}>
                                {n}
                            </MenuItem>
                        ))}
                    </TextField>
                )}
                {state && (
                    <TextField
                        select
                        label="State"
                        slotProps={{
                            select: { displayEmpty: true },
                            inputLabel: { shrink: true },
                        }}
                        value={selectedState}
                        onChange={(e) => update("state", e.target.value)}
                        sx={{ width: 160 }}
                    >
                        <MenuItem value="">All states</MenuItem>
                        {[...new Set(rows.map(state))].map((s) => (
                            <MenuItem key={s} value={s}>
                                {s}
                            </MenuItem>
                        ))}
                    </TextField>
                )}
                {pool && (
                    <TextField
                        select
                        label={poolLabel}
                        slotProps={{
                            select: { displayEmpty: true },
                            inputLabel: { shrink: true },
                        }}
                        value={selectedPool}
                        onChange={(e) => update("pool", e.target.value)}
                        sx={{ width: 180 }}
                    >
                        <MenuItem value="">
                            All{" "}
                            {poolLabel === "Storage class"
                                ? "storage classes"
                                : "pools"}
                        </MenuItem>
                        {[...new Set(rows.map(pool))].map((p) => (
                            <MenuItem key={p} value={p}>
                                {p}
                            </MenuItem>
                        ))}
                    </TextField>
                )}
                <Button
                    onClick={() => setParams({}, { replace: true })}
                    disabled={!active && !params.has("sort")}
                >
                    Reset
                </Button>
            </Stack>
            <TableContainer
                component={Box}
                sx={{
                    border: 1,
                    borderColor: "divider",
                    borderRadius: 1,
                    bgcolor: "background.paper",
                }}
                tabIndex={0}
                aria-label="Scrollable resources"
            >
                <Table size="small">
                    <TableHead>
                        <TableRow>
                            {columns.map((c) => (
                                <TableCell
                                    key={c.id}
                                    align={c.numeric ? "right" : "left"}
                                    sortDirection={
                                        column.id === c.id
                                            ? desc
                                                ? "desc"
                                                : "asc"
                                            : false
                                    }
                                >
                                    <TableSortLabel
                                        active={column.id === c.id}
                                        direction={desc ? "desc" : "asc"}
                                        onClick={() =>
                                            setParams(
                                                (old) => {
                                                    const p =
                                                        new URLSearchParams(
                                                            old,
                                                        );
                                                    p.set("sort", c.id);
                                                    p.set(
                                                        "direction",
                                                        column.id === c.id &&
                                                            !desc
                                                            ? "desc"
                                                            : "asc",
                                                    );
                                                    return p;
                                                },
                                                { replace: true },
                                            )
                                        }
                                    >
                                        {c.label}
                                    </TableSortLabel>
                                </TableCell>
                            ))}
                            {actions && (
                                <TableCell align="right">Actions</TableCell>
                            )}
                        </TableRow>
                    </TableHead>
                    <TableBody>
                        {filtered.map((r) => (
                            <TableRow key={rowKey(r)} hover>
                                {columns.map((c) => (
                                    <TableCell
                                        key={c.id}
                                        align={c.numeric ? "right" : "left"}
                                    >
                                        {c.render ? c.render(r) : c.value(r)}
                                    </TableCell>
                                ))}
                                {actions && (
                                    <TableCell
                                        align="right"
                                        sx={{
                                            whiteSpace: "nowrap",
                                            "& .MuiButton-root": {
                                                fontSize: 13,
                                                padding: "3px 8px",
                                                minHeight: 28,
                                            },
                                        }}
                                    >
                                        {actions(r)}
                                    </TableCell>
                                )}
                            </TableRow>
                        ))}
                    </TableBody>
                </Table>
            </TableContainer>
            {filtered.length === 0 && (
                <Typography
                    color="text.secondary"
                    sx={{ py: 4, textAlign: "center" }}
                >
                    {rows.length
                        ? "No matches. Change or reset the filters."
                        : empty}
                </Typography>
            )}
            <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
                {filtered.length} of {rows.length}
            </Typography>
        </>
    );
}
