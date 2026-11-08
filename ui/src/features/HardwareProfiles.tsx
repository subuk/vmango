import { useState } from "react";
import { Alert, MenuItem, Stack, TextField, Typography } from "@mui/material";
import { useRequest } from "../app/Workspace";
import { adminCloudPath, useAdministration } from "../api/administration";
import type { components } from "../api/schema";
import type { VM } from "../api/hooks";
import { DetailSection } from "../components/Information";
import { OperationDialog } from "../components/OperationDialog";
import { QueryState } from "../components/common";
import { Fields } from "./AdministrationFields";

export type HardwareProfile = components["schemas"]["Profile"];

export function HardwareProfileFields({
    profile,
}: {
    profile: HardwareProfile;
}) {
    const cd = profile.config_drive;
    return (
        <Fields
            entries={[
                ["Profile ID", profile.id],
                ["Firmware", profile.firmware],
                ["Ordinary disk bus", profile.disk_bus],
                [
                    "Disk controller",
                    profile.disk_controller || "Per-device / platform default",
                ],
                ["Network device", profile.nic_model],
                ["Graphics", profile.graphics],
                ["Video model", profile.video_model],
                ["Guest agent", profile.guest_agent ?? true],
                ["Hugepages", profile.hugepages],
                [
                    "Config drive",
                    cd.enabled === false
                        ? "Disabled"
                        : `${cd.format} · ${cd.device_type} · ${cd.bus}`,
                ],
            ]}
        />
    );
}

export function HardwareProfiles({
    cloud,
    snapshot,
}: {
    cloud: string;
    snapshot?: string;
}) {
    const q = useAdministration<HardwareProfile[]>(
        `${adminCloudPath(cloud)}/hardware-profiles`,
        true,
        snapshot,
    );
    return (
        <DetailSection
            title="Hardware profiles"
            description="Images select a reusable guest platform. Changes apply to newly accepted operations; existing machines retain their applied policy."
        >
            <QueryState query={q}>
                {(profiles) => (
                    <Stack spacing={3}>
                        {profiles.length === 0 && (
                            <Typography color="text.secondary">
                                No hardware profiles configured.
                            </Typography>
                        )}
                        {profiles.map((profile) => (
                            <DetailSection
                                key={profile.id}
                                title={profile.name}
                            >
                                <HardwareProfileFields profile={profile} />
                            </DetailSection>
                        ))}
                    </Stack>
                )}
            </QueryState>
        </DetailSection>
    );
}

export function ImageHardwareProfile({
    cloud,
    image,
    snapshot,
}: {
    cloud: string;
    image: string;
    snapshot?: string;
}) {
    const q = useAdministration<HardwareProfile>(
        `${adminCloudPath(cloud)}/images/${encodeURIComponent(image)}/hardware-profile`,
        true,
        snapshot,
    );
    return (
        <DetailSection
            title="Selected hardware profile"
            description="The image uses this complete profile, without per-image hardware overrides."
        >
            <QueryState query={q}>
                {(profile) => <HardwareProfileFields profile={profile} />}
            </QueryState>
        </DetailSection>
    );
}

export function AdoptMachine({ vm, onClose }: { vm: VM; onClose: () => void }) {
    const request = useRequest();
    const [selected, setSelected] = useState("");
    const profiles = useAdministration<HardwareProfile[]>(
        `${adminCloudPath(vm.cloud)}/hardware-profiles`,
    );
    const profile = profiles.data?.find((p) => p.id === selected);
    return (
        <OperationDialog
            title={`Adopt ${vm.name}`}
            action="Adopt machine"
            onClose={onClose}
            submitDisabled={!profile}
            run={() =>
                request(
                    `/api/v1/clouds/${encodeURIComponent(vm.cloud)}/machines/${encodeURIComponent(vm.id)}/adopt`,
                    "POST",
                    { hardware_profile: selected },
                )
            }
        >
            {() => (
                <Stack spacing={2}>
                    <Alert severity="info">
                        Choose a profile compatible with this machine. Adoption
                        records the policy and preserves existing hardware.
                        Incompatible settings must be resolved explicitly before
                        adoption.
                    </Alert>
                    <QueryState query={profiles}>
                        {(items) => (
                            <TextField
                                select
                                required
                                label="Hardware profile"
                                name="hardware_profile"
                                value={selected}
                                onChange={(e) => setSelected(e.target.value)}
                                fullWidth
                            >
                                {items.map((p) => (
                                    <MenuItem key={p.id} value={p.id}>
                                        {p.name} ({p.id})
                                    </MenuItem>
                                ))}
                            </TextField>
                        )}
                    </QueryState>
                    {profile && <HardwareProfileFields profile={profile} />}
                </Stack>
            )}
        </OperationDialog>
    );
}
