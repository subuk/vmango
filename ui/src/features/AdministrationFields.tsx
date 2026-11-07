import { isValidElement, type ReactNode } from "react";
import { Box, Link } from "@mui/material";
import { WorkspaceLink as RouterLink } from "../app/Workspace";
export const link = (to: string, text: string) => (
    <Link component={RouterLink} to={to}>
        {text}
    </Link>
);
const setting = (v: unknown): ReactNode =>
    v === null || v === undefined || v === ""
        ? "Unspecified"
        : isValidElement(v)
          ? v
          : typeof v === "boolean"
            ? v
                ? "Enabled"
                : "Disabled"
            : String(v);
export function Fields({ entries }: { entries: [string, unknown][] }) {
    return (
        <Box
            component="dl"
            sx={{
                display: "grid",
                gridTemplateColumns: {
                    xs: "minmax(110px, 40%) minmax(0, 1fr)",
                    md: "minmax(150px, 28%) minmax(0, 1fr)",
                },
                m: 0,
                fontSize: 13,
                "& dt, & dd": {
                    m: 0,
                    py: 0.8,
                    borderBottom: "1px solid",
                    borderColor: "divider",
                },
                "& dt": { color: "text.secondary", pr: 2 },
            }}
        >
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
