import { useId, type ReactNode } from "react";
import { Box, LinearProgress, Paper, Typography } from "@mui/material";

export function Summary({
    items,
    label = "Resource summary",
}: {
    items: {
        label: string;
        value: ReactNode;
        detail?: ReactNode;
        tone?: "success" | "warning";
    }[];
    label?: string;
}) {
    return (
        <Box component="dl" className="summary-grid" aria-label={label}>
            {items.map((item) => (
                <Paper
                    component="div"
                    variant="outlined"
                    className="summary-item"
                    key={item.label}
                >
                    <Typography
                        component="dt"
                        variant="body2"
                        color="text.secondary"
                    >
                        {item.label}
                    </Typography>
                    <Typography
                        component="dd"
                        className="summary-value"
                        sx={{
                            color: item.tone
                                ? `${item.tone}.main`
                                : "text.primary",
                        }}
                    >
                        {item.value}
                    </Typography>
                    {item.detail && (
                        <Typography
                            component="dd"
                            variant="body2"
                            color="text.secondary"
                            className="summary-detail"
                        >
                            {item.detail}
                        </Typography>
                    )}
                </Paper>
            ))}
        </Box>
    );
}

export function DetailSection({
    title,
    description,
    children,
}: {
    title: string;
    description?: string;
    children: ReactNode;
}) {
    const id = useId();
    return (
        <Paper
            component="section"
            variant="outlined"
            className="detail-section"
            aria-labelledby={id}
        >
            <Box className="section-heading">
                <Typography variant="h2" id={id}>
                    {title}
                </Typography>
                {description && (
                    <Typography
                        variant="body2"
                        color="text.secondary"
                        sx={{ mt: 0.5 }}
                    >
                        {description}
                    </Typography>
                )}
            </Box>
            <Box className="section-content">{children}</Box>
        </Paper>
    );
}

export function Capacity({
    used,
    total,
    label,
}: {
    used: number;
    total: number;
    label: string;
}) {
    const percent =
        total > 0
            ? Math.max(0, Math.min(100, (used / total) * 100))
            : undefined;
    return (
        <Box sx={{ minWidth: 120 }}>
            <Typography
                variant="body2"
                sx={{ mb: 0.75, fontVariantNumeric: "tabular-nums" }}
            >
                {percent === undefined
                    ? "Capacity unavailable"
                    : `${Math.round(percent)}% used`}
            </Typography>
            {percent !== undefined && (
                <LinearProgress
                    variant="determinate"
                    value={percent}
                    aria-label={label}
                    color={percent >= 90 ? "warning" : "primary"}
                    sx={{ height: 5, borderRadius: 1 }}
                />
            )}
        </Box>
    );
}
