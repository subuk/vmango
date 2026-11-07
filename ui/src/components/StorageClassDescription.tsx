import { Box, Typography } from "@mui/material";
import type { components } from "../api/schema";

export const dependentDiskNotice =
    "Its source must remain available and must not be changed or deleted while this disk exists, even when the machine is stopped.";

export function diskDependencyLabel(mode: string | undefined) {
    return mode === "overlay" ? "Depends on its source" : "Independent disk";
}

export function StorageClassDescription({
    storageClass,
    cloning = false,
}: {
    storageClass?: components["schemas"]["StorageClass"];
    cloning?: boolean;
}) {
    if (!storageClass) return null;
    return (
        <Box aria-live="polite">
            {storageClass.description && (
                <Typography variant="body2">
                    {storageClass.description}
                </Typography>
            )}
            {cloning && (
                <Typography variant="body2" color="text.secondary">
                    {storageClass.clone_mode === "overlay"
                        ? `Creates a space-saving copy that depends on its source. ${dependentDiskNotice}`
                        : "Creates an independent copy that does not depend on its source after creation."}
                </Typography>
            )}
        </Box>
    );
}
