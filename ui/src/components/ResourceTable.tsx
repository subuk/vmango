import { useLayoutEffect, useRef, useState, type ReactNode } from "react";
import {
    Box,
    Button,
    Checkbox,
    Menu,
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

export type Column<T> = {
    id: string;
    label: string;
    value: (row: T) => string | number;
    render?: (row: T) => ReactNode;
    numeric?: boolean;
    defaultHidden?: boolean;
};
export type ResourceFilter<T> = {
    id: string;
    label: string;
    value: (row: T) => string;
    options?: { value: string; label: string }[];
    allLabel?: string;
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
    filters = [],
    paramPrefix = "",
    preferenceKey,
    selectedKey,
    onSelect,
    scrollKey,
    ariaLabel = "Scrollable resources",
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
    filters?: ResourceFilter<T>[];
    paramPrefix?: string;
    preferenceKey?: string;
    selectedKey?: string;
    onSelect?: (row: T) => void;
    scrollKey?: string;
    ariaLabel?: string;
}) {
    const [params, setParams] = useSearchParams();
    const [menu, setMenu] = useState<HTMLElement | null>(null);
    const [preference, setPreference] = useState<{
        key: string;
        ids: string[];
    }>();
    const container = useRef<HTMLDivElement>(null);
    const storageKey = preferenceKey
        ? `vmango.columns.${preferenceKey}`
        : undefined;
    let stored: string[] | undefined;
    if (storageKey) {
        if (preference?.key === storageKey) stored = preference.ids;
        else
            try {
                const value: unknown = JSON.parse(
                    localStorage.getItem(storageKey) || "null",
                );
                if (
                    Array.isArray(value) &&
                    value.every((id) => typeof id === "string")
                )
                    stored = value;
            } catch {
                /* Storage may be unavailable in private browsing. */
            }
    }
    const visible = columns.filter((c) =>
        stored ? stored.includes(c.id) : !c.defaultHidden,
    );
    if (!visible.length && columns.length) visible.push(columns[0]);
    const key = (name: string) => `${paramPrefix}${name}`;
    const get = (name: string) => params.get(key(name)) || "";
    const update = (name: string, value: string) =>
        setParams(
            (old) => {
                const next = new URLSearchParams(old);
                value ? next.set(key(name), value) : next.delete(key(name));
                return next;
            },
            { replace: true },
        );
    const allFilters: ResourceFilter<T>[] = [
        ...(node
            ? [
                  {
                      id: "node",
                      label: "Host",
                      value: node,
                      allLabel: "All hosts",
                  },
              ]
            : []),
        ...(state
            ? [
                  {
                      id: "state",
                      label: "State",
                      value: state,
                      allLabel: "All states",
                  },
              ]
            : []),
        ...(pool
            ? [
                  {
                      id: "pool",
                      label: poolLabel,
                      value: pool,
                      allLabel:
                          poolLabel === "Storage class"
                              ? "All storage classes"
                              : "All pools",
                  },
              ]
            : []),
        ...filters,
    ];
    const query = get("q");
    const column = columns.find((c) => c.id === get("sort")) || visible[0];
    const desc = get("direction") === "desc";
    const filtered = rows.filter(
        (row) =>
            (!query ||
                search(row)
                    .toLocaleLowerCase()
                    .includes(query.toLocaleLowerCase())) &&
            allFilters.every(
                (filter) =>
                    !get(filter.id) || filter.value(row) === get(filter.id),
            ),
    );
    filtered.sort((a, b) => {
        const x = column.value(a),
            y = column.value(b);
        const order =
            typeof x === "number" && typeof y === "number"
                ? x - y
                : String(x).localeCompare(String(y), undefined, {
                      numeric: true,
                  });
        return (desc ? -order : order) || rowKey(a).localeCompare(rowKey(b));
    });
    const active = query || allFilters.some((f) => get(f.id));
    useLayoutEffect(() => {
        if (!scrollKey || !container.current) return;
        try {
            const position = JSON.parse(
                sessionStorage.getItem(`vmango.scroll.${scrollKey}`) || "null",
            );
            if (position) {
                container.current.scrollTop = position.top;
                container.current.scrollLeft = position.left;
            }
        } catch {
            /* Scroll restoration is best effort. */
        }
    }, [scrollKey]);
    return (
        <Box className="resource-panel">
            <Stack
                className="filter-toolbar"
                direction="row"
                sx={{ gap: 2, flexWrap: "wrap", p: 2, alignItems: "center" }}
            >
                <TextField
                    label="Search"
                    value={query}
                    onChange={(e) => update("q", e.target.value)}
                    sx={{ maxWidth: 300 }}
                />
                {allFilters.map((filter) => {
                    const options =
                        (filter.options && [...filter.options]) ||
                        [...new Set(rows.map(filter.value))]
                            .sort()
                            .map((value) => ({ value, label: value }));
                    if (
                        get(filter.id) &&
                        !options.some((o) => o.value === get(filter.id))
                    )
                        options.push({
                            value: get(filter.id),
                            label: get(filter.id),
                        });
                    return (
                        <TextField
                            key={filter.id}
                            select
                            label={filter.label}
                            slotProps={{
                                select: { displayEmpty: true },
                                inputLabel: { shrink: true },
                            }}
                            value={get(filter.id)}
                            onChange={(e) => update(filter.id, e.target.value)}
                            sx={{ minWidth: 150 }}
                        >
                            <MenuItem value="">
                                {filter.allLabel ||
                                    `All ${filter.label.toLowerCase()}s`}
                            </MenuItem>
                            {options.map((option) => (
                                <MenuItem
                                    key={option.value}
                                    value={option.value}
                                >
                                    {option.label}
                                </MenuItem>
                            ))}
                        </TextField>
                    );
                })}
                <Button
                    disabled={!active && !get("sort") && !get("direction")}
                    onClick={() =>
                        setParams(
                            (old) => {
                                const next = new URLSearchParams(old);
                                [
                                    "q",
                                    "node",
                                    "state",
                                    "pool",
                                    "sort",
                                    "direction",
                                    ...allFilters.map((f) => f.id),
                                ].forEach((name) => next.delete(key(name)));
                                return next;
                            },
                            { replace: true },
                        )
                    }
                >
                    Reset
                </Button>
                {storageKey && (
                    <>
                        <Button
                            onClick={(e) => setMenu(e.currentTarget)}
                            aria-haspopup="menu"
                            aria-expanded={!!menu}
                        >
                            Columns
                        </Button>
                        <Menu
                            anchorEl={menu}
                            open={!!menu}
                            onClose={() => setMenu(null)}
                        >
                            {columns.map((c) => (
                                <MenuItem
                                    key={c.id}
                                    role="menuitemcheckbox"
                                    aria-checked={visible.includes(c)}
                                    disabled={
                                        visible.length === 1 &&
                                        visible.includes(c)
                                    }
                                    onClick={() => {
                                        const ids = columns
                                            .filter((candidate) =>
                                                candidate.id === c.id
                                                    ? !visible.includes(c)
                                                    : visible.includes(
                                                          candidate,
                                                      ),
                                            )
                                            .map((candidate) => candidate.id);
                                        setPreference({ key: storageKey, ids });
                                        try {
                                            localStorage.setItem(
                                                storageKey,
                                                JSON.stringify(ids),
                                            );
                                        } catch {
                                            /* Keep in-memory preferences when storage is disabled. */
                                        }
                                    }}
                                >
                                    <Checkbox
                                        checked={visible.includes(c)}
                                        tabIndex={-1}
                                        disableRipple
                                        slotProps={{
                                            input: {
                                                "aria-hidden": true,
                                                readOnly: true,
                                            },
                                        }}
                                        sx={{ pointerEvents: "none" }}
                                    />
                                    {c.label}
                                </MenuItem>
                            ))}
                        </Menu>
                    </>
                )}
            </Stack>
            <TableContainer
                component={Box}
                ref={container}
                sx={{
                    borderTop: 1,
                    borderColor: "divider",
                    bgcolor: "background.paper",
                }}
                tabIndex={0}
                aria-label={ariaLabel}
                onScroll={() => {
                    if (!scrollKey || !container.current) return;
                    try {
                        sessionStorage.setItem(
                            `vmango.scroll.${scrollKey}`,
                            JSON.stringify({
                                top: container.current.scrollTop,
                                left: container.current.scrollLeft,
                            }),
                        );
                    } catch {
                        /* Scroll restoration is best effort. */
                    }
                }}
            >
                <Table size="small">
                    <TableHead>
                        <TableRow>
                            {visible.map((c) => (
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
                                                    const next =
                                                        new URLSearchParams(
                                                            old,
                                                        );
                                                    next.set(key("sort"), c.id);
                                                    next.set(
                                                        key("direction"),
                                                        column.id === c.id &&
                                                            !desc
                                                            ? "desc"
                                                            : "asc",
                                                    );
                                                    return next;
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
                        {filtered.map((row) => (
                            <TableRow
                                key={rowKey(row)}
                                hover
                                selected={
                                    !!onSelect && rowKey(row) === selectedKey
                                }
                                aria-selected={
                                    onSelect
                                        ? rowKey(row) === selectedKey
                                        : undefined
                                }
                                tabIndex={onSelect ? 0 : undefined}
                                onClick={
                                    onSelect
                                        ? (event) => {
                                              if (
                                                  !(
                                                      event.target as HTMLElement
                                                  ).closest(
                                                      "a,button,input,select,textarea",
                                                  )
                                              )
                                                  onSelect(row);
                                          }
                                        : undefined
                                }
                                onKeyDown={
                                    onSelect
                                        ? (event) => {
                                              if (
                                                  event.target ===
                                                      event.currentTarget &&
                                                  (event.key === "Enter" ||
                                                      event.key === " ")
                                              ) {
                                                  event.preventDefault();
                                                  onSelect(row);
                                              }
                                          }
                                        : undefined
                                }
                                sx={
                                    onSelect
                                        ? {
                                              cursor: "pointer",
                                              "&:focus-visible": {
                                                  outline: "2px solid",
                                                  outlineColor: "primary.main",
                                                  outlineOffset: -2,
                                              },
                                          }
                                        : undefined
                                }
                            >
                                {visible.map((c) => (
                                    <TableCell
                                        key={c.id}
                                        align={c.numeric ? "right" : "left"}
                                    >
                                        {c.render
                                            ? c.render(row)
                                            : c.value(row)}
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
                                        {actions(row)}
                                    </TableCell>
                                )}
                            </TableRow>
                        ))}
                    </TableBody>
                </Table>
            </TableContainer>
            {!filtered.length && (
                <Typography
                    color="text.secondary"
                    sx={{ py: 4, textAlign: "center" }}
                >
                    {rows.length
                        ? "No matches. Change or reset the filters."
                        : empty}
                </Typography>
            )}
            <Typography
                variant="body2"
                color="text.secondary"
                sx={{ px: 2, py: 1.5, borderTop: 1, borderColor: "divider" }}
            >
                Showing {filtered.length} of {rows.length} resources
            </Typography>
        </Box>
    );
}
