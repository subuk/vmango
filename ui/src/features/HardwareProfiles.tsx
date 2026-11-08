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
                [
                    "Requested CPU",
                    profile.cpu
                        ? `${profile.cpu.mode}${profile.cpu.model ? ` · ${profile.cpu.model}` : ""}${profile.cpu.vendor ? ` · ${profile.cpu.vendor}` : ""}${profile.cpu.mode_explicit ? " (explicit)" : " (default for new VMs; preserved on adoption)"}`
                        : "Preserved legacy policy",
                ],
                [
                    "CPU features",
                    profile.cpu?.features
                        ?.map((f) => `${f.name}: ${f.policy}`)
                        .join("; ") || "Mode/model defaults",
                ],
                [
                    "Native placement",
                    profile.placement
                        ? `${profile.placement.mode}${profile.placement.memory_mode ? ` · ${profile.placement.memory_mode} memory` : ""}`
                        : "Static for new VMs; existing affinity preserved",
                ],
                [
                    "Hyper-V policy",
                    profile.hyperv
                        ? describeHyperV(profile.hyperv)
                        : "Not enabled for new VMs; existing presentation preserved",
                ],
                [
                    "KVM presentation",
                    profile.kvm
                        ? Object.entries(profile.kvm)
                              .map(
                                  ([name, value]) =>
                                      `${name}: ${value ? "on" : "off"}`,
                              )
                              .join("; ") || "Backend defaults"
                        : "Visible for new VMs; backend defaults / preserved on adoption",
                ],
                [
                    "Requested machine",
                    profile.machine
                        ? `${profile.machine.type}${profile.machine.type_explicit ? " (explicit)" : " (default for new VMs)"}`
                        : "Preserved legacy policy",
                ],
                [
                    "Requested acceleration",
                    profile.machine
                        ? `${profile.machine.acceleration}${profile.machine.acceleration_explicit ? " (explicit)" : " (default for new VMs)"}`
                        : "Preserved legacy policy",
                ],
                [
                    "Requested hardware clock",
                    profile.clock
                        ? `${profile.clock.mode}${profile.clock.timezone ? ` · ${profile.clock.timezone}` : ""}${profile.clock.mode_explicit ? " (explicit)" : " (default for new VMs; preserved on adoption)"}`
                        : "Preserved legacy policy",
                ],
                ["SMBIOS constraints", describeSMBIOS(profile.smbios)],
                [
                    "Timer constraints",
                    profile.timers
                        ?.map(
                            (t) =>
                                `${t.name}: ${t.tickpolicy || (t.present === undefined ? "backend defaults" : t.present ? "present" : "absent")}`,
                        )
                        .join("; ") ||
                        "Backend defaults / preserved on adoption",
                ],
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

export function PlatformBindingFields({
    binding,
}: {
    binding: components["schemas"]["PlatformBinding"];
}) {
    return (
        <Fields
            entries={[
                ["Machine type", binding.machine || "Unavailable"],
                ["Acceleration backend", binding.domain_type || "Unavailable"],
                ["Emulator on host", binding.emulator || "Unavailable"],
            ]}
        />
    );
}

function describeSMBIOS(info?: components["schemas"]["SMBIOS"]) {
    if (!info) return "Unspecified";
    return (
        Object.entries(info)
            .flatMap(([block, values]) =>
                Object.entries(values || {}).map(
                    ([key, value]) => `${block}.${key}: ${value}`,
                ),
            )
            .join("; ") || "No descriptive constraints"
    );
}

export function GuestSystemFields({
    system,
}: {
    system: components["schemas"]["GuestSystemObservation"];
}) {
    const clock = system.clock;
    return (
        <Fields
            entries={[
                ["SMBIOS presentation", system.smbios_mode],
                ["SMBIOS information", describeSMBIOS(system.smbios)],
                ["SMBIOS system UUID", system.system_uuid],
                [
                    "Hardware clock",
                    clock
                        ? [
                              clock.mode,
                              clock.timezone,
                              clock.adjustment &&
                                  `adjustment=${clock.adjustment}`,
                              clock.basis && `basis=${clock.basis}`,
                              clock.start && `start=${clock.start}`,
                          ]
                              .filter(Boolean)
                              .join(" · ")
                        : "Unspecified",
                ],
                [
                    "Timers",
                    clock?.timers
                        ?.map((t) =>
                            [
                                t.name,
                                t.tickpolicy,
                                t.present && `present=${t.present}`,
                                t.track && `track=${t.track}`,
                                t.mode && `mode=${t.mode}`,
                                t.frequency && `frequency=${t.frequency}`,
                                t.catchup &&
                                    `catchup=${t.catchup.threshold}/${t.catchup.slew}/${t.catchup.limit}`,
                            ]
                                .filter(Boolean)
                                .join(" · "),
                        )
                        .join("; ") || "Unspecified",
                ],
            ]}
        />
    );
}

export function CPUObservationFields({
    value,
}: {
    value: components["schemas"]["CPUObservation"];
}) {
    const t = value.topology;
    return (
        <Fields
            entries={[
                [
                    "CPU mode / model / vendor",
                    [value.mode, value.model, value.vendor]
                        .filter(Boolean)
                        .join(" · ") || "Backend default",
                ],
                [
                    "CPU features",
                    value.features
                        ?.map((f) => `${f.name}: ${f.policy}`)
                        .join("; ") || "Mode/model defaults",
                ],
                [
                    "Guest topology",
                    t
                        ? `${t.sockets} sockets · ${t.cores} cores · ${t.threads} threads${t.dies ? ` · ${t.dies} dies` : ""}${t.clusters ? ` · ${t.clusters} clusters` : ""}`
                        : "Not explicitly described",
                ],
                ["Guest NUMA cells", value.guest_numa],
                [
                    "CPU placement",
                    `${value.placement || "static"}${value.cpuset ? ` · CPUs ${value.cpuset}` : ""}`,
                ],
                [
                    "Memory placement",
                    [
                        value.memory_placement,
                        value.memory_mode,
                        value.memory_nodeset
                            ? `nodes ${value.memory_nodeset}`
                            : "",
                    ]
                        .filter(Boolean)
                        .join(" · ") || "Unrestricted",
                ],
                [
                    "Hyper-V presentation",
                    [
                        value.hyperv_mode,
                        value.hyperv
                            ?.map(
                                (f) =>
                                    `${f.name}: ${f.state}${f.value ? ` (${f.value})` : ""}`,
                            )
                            .join("; "),
                    ]
                        .filter(Boolean)
                        .join(" · ") || "Not explicitly enabled",
                ],
                [
                    "KVM presentation",
                    value.kvm
                        ?.map(
                            (f) =>
                                `${f.name}: ${f.state}${f.value ? ` (${f.value})` : ""}`,
                        )
                        .join("; ") || "Backend defaults",
                ],
            ]}
        />
    );
}

function describeHyperV(h: NonNullable<HardwareProfile["hyperv"]>) {
    if (h.mode !== "custom")
        return `${h.mode} (backend-selected enlightenments)`;
    const values = Object.entries(h)
        .filter(
            ([name, value]) => name !== "mode" && typeof value === "boolean",
        )
        .map(([name, value]) => `${name}: ${value ? "on" : "off"}`);
    if (h.spinlocks)
        values.push(
            `spinlocks: ${h.spinlocks.enabled ? `on (${h.spinlocks.retries} retries)` : "off"}`,
        );
    if (h.stimer)
        values.push(
            `stimer: ${h.stimer.enabled ? `on (direct ${h.stimer.direct ? "on" : "off"})` : "off"}`,
        );
    return `custom · ${values.join("; ") || "all features disabled"}`;
}
