import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import {
    AdoptMachine,
    HardwareProfileFields,
    GuestSystemFields,
    CPUObservationFields,
    HardwareProfiles,
    type HardwareProfile,
} from "./HardwareProfiles";
import type { VM } from "../api/hooks";

const mocks = vi.hoisted(() => ({ request: vi.fn() }));
vi.mock("../api/client", async (original) => ({
    ...(await original<typeof import("../api/client")>()),
    request: mocks.request,
}));
vi.mock("../app/Auth", () => ({ useMe: () => ({ id: "operator" }) }));
const profile: HardwareProfile = {
    id: "linux-scsi",
    name: "Linux SCSI",
    machine: {
        type: "q35",
        acceleration: "hardware",
        type_explicit: true,
        acceleration_explicit: false,
    },
    firmware: "efi",
    disk_bus: "scsi",
    disk_controller: "virtio-scsi",
    nic_model: "virtio",
    graphics: "vnc",
    video_model: "none",
    guest_agent: true,
    hugepages: false,
    config_drive: {
        enabled: true,
        format: "nocloud",
        device_type: "disk",
        bus: "virtio",
    },
};
function show(
    element: React.ReactNode,
    networks: { id: string; kind: string; backend: string }[] = [],
) {
    mocks.request.mockImplementation(async (path: string, method: string) => {
        if (method === "POST") return { id: "accepted" };
        if (path.endsWith("/configuration")) return { snapshot_id: "snapshot" };
        if (path.endsWith("/hardware-profiles")) return [profile];
        if (path.includes("/configuration/hosts/")) return { networks };
        throw new Error(path);
    });
    render(
        <QueryClientProvider
            client={
                new QueryClient({
                    defaultOptions: { queries: { retry: false } },
                })
            }
        >
            <MemoryRouter>{element}</MemoryRouter>
        </QueryClientProvider>,
    );
}
afterEach(() => {
    cleanup();
    mocks.request.mockReset();
});
it("shows ordinary disk and config-drive policies separately without editable listener settings", async () => {
    show(<HardwareProfiles cloud="local" />);
    expect(await screen.findByText("Linux SCSI")).toBeTruthy();
    expect(screen.getByText("Ordinary disk bus")).toBeTruthy();
    expect(screen.getByText("q35 (explicit)")).toBeTruthy();
    expect(screen.getByText("hardware (default for new VMs)")).toBeTruthy();
    expect(screen.getByText("nocloud · disk · virtio")).toBeTruthy();
    expect(screen.queryByRole("textbox")).toBeNull();
});
it("requires explicit disk and profile choices, automatically matching configured networks", async () => {
    const close = vi.fn();
    show(
        <AdoptMachine
            vm={
                {
                    id: "vm",
                    name: "Existing VM",
                    cloud: "local",
                    node: "host",
                    disks: [
                        {
                            device_id: "vdb",
                            source: "/external.iso",
                            device: "disk",
                            format: "raw",
                        },
                    ],
                    interfaces: [
                        {
                            device_id: "mac:52:54:00:00:00:01",
                            mac: "52:54:00:00:00:01",
                            binding: { type: "qemu-user" },
                        },
                    ],
                } as VM
            }
            onClose={close}
        />,
        [{ id: "internet", kind: "qemu-user", backend: "" }],
    );
    const button = screen.getByRole("button", {
        name: "Adopt machine",
    }) as HTMLButtonElement;
    expect(button.disabled).toBe(true);
    await userEvent.click(
        await screen.findByRole("combobox", { name: /Hardware profile/ }),
    );
    await userEvent.click(
        screen.getByRole("option", { name: "Linux SCSI (linux-scsi)" }),
    );
    expect(button.disabled).toBe(true);
    await userEvent.click(
        screen.getByRole("combobox", { name: /Config drive/ }),
    );
    await userEvent.click(screen.getByRole("option", { name: /external.iso/ }));
    expect(
        await screen.findByText(/matched from host configuration/),
    ).toBeTruthy();
    expect(button.disabled).toBe(false);
    expect(screen.getByText(/preserves existing hardware/)).toBeTruthy();
    await userEvent.click(button);
    await waitFor(() =>
        expect(mocks.request).toHaveBeenCalledWith(
            "/api/v1/clouds/local/machines/vm/adopt",
            "POST",
            {
                hardware_profile: "linux-scsi",
                config_drive: "vdb",
                networks: { "mac:52:54:00:00:00:01": "internet" },
            },
            undefined,
            expect.anything(),
        ),
    );
    expect(close).toHaveBeenCalledOnce();
});

it("distinguishes omitted clock constraints from observed settings", () => {
    show(
        <HardwareProfileFields
            profile={{
                ...profile,
                clock: { mode: "utc", mode_explicit: false },
                smbios: { system: { product: "Guest" } },
                timers: [{ name: "hpet", present: false }],
            }}
        />,
    );
    expect(
        screen.getByText("utc (default for new VMs; preserved on adoption)"),
    ).toBeTruthy();
    expect(screen.getByText("system.product: Guest")).toBeTruthy();
    expect(screen.getByText("hpet: absent")).toBeTruthy();
});
it("shows observed clock and timer attributes without inferring missing settings", () => {
    show(
        <GuestSystemFields
            system={{
                clock: {
                    mode: "variable",
                    basis: "localtime",
                    adjustment: "3600",
                    timers: [
                        { name: "hpet" },
                        { name: "tsc", mode: "native", frequency: 12345 },
                    ],
                },
            }}
        />,
    );
    expect(
        screen.getByText("variable · adjustment=3600 · basis=localtime"),
    ).toBeTruthy();
    expect(
        screen.getByText("hpet; tsc · mode=native · frequency=12345"),
    ).toBeTruthy();
});

it("separates requested CPU constraints from effective backend presentation", () => {
    show(
        <HardwareProfileFields
            profile={{
                ...profile,
                cpu: {
                    mode: "maximum",
                    mode_explicit: false,
                    features: [{ name: "aes", policy: "require" }],
                },
                hyperv: { mode: "host-model" },
                kvm: { hidden: false, pv_ipi: true },
            }}
        />,
    );
    expect(
        screen.getByText(
            "maximum (default for new VMs; preserved on adoption)",
        ),
    ).toBeTruthy();
    expect(screen.getByText("aes: require")).toBeTruthy();
    expect(
        screen.getByText("host-model (backend-selected enlightenments)"),
    ).toBeTruthy();
    expect(screen.getByText("hidden: off; pv_ipi: on")).toBeTruthy();
});
it("shows native placement nodesets and extra observed hypervisor features", () => {
    show(
        <CPUObservationFields
            value={{
                mode: "host-passthrough",
                guest_numa: false,
                placement: "auto",
                memory_placement: "auto",
                memory_mode: "strict",
                memory_nodeset: "0-1",
                hyperv_mode: "passthrough",
                hyperv: [{ name: "evmcs", state: "on" }],
                kvm: [{ name: "hint-dedicated", state: "on" }],
            }}
        />,
    );
    expect(screen.getByText("auto · strict · nodes 0-1")).toBeTruthy();
    expect(screen.getByText("passthrough · evmcs: on")).toBeTruthy();
    expect(screen.getByText("hint-dedicated: on")).toBeTruthy();
});

it("distinguishes Secure Boot support from initial key enrollment", () => {
    render(
        <HardwareProfileFields
            profile={{
                ...profile,
                firmware_policy: {
                    type: "efi",
                    profile: "release-a",
                    secure_boot: true,
                    enrolled_keys: false,
                },
            }}
        />,
    );
    expect(screen.getByText("efi · release-a")).toBeTruthy();
    expect(screen.getByText("Required")).toBeTruthy();
    expect(screen.getByText("Not enrolled")).toBeTruthy();
});

it("keeps ordinary SATA and independent SCSI config-drive requirements visible", () => {
    show(
        <HardwareProfileFields
            profile={{
                ...profile,
                disk_bus: "sata",
                disk_controller: "",
                disks: { bus: "sata" },
                config_drive: {
                    enabled: true,
                    format: "openstack",
                    device_type: "cdrom",
                    bus: "scsi",
                    controller_model: "lsilogic",
                },
            }}
        />,
    );
    expect(screen.getByText("AHCI (platform mapping)")).toBeTruthy();
    expect(
        screen.getByText("openstack · cdrom · scsi · lsilogic"),
    ).toBeTruthy();
});
it("distinguishes standalone USB from dependencies and input omission from disable", () => {
    show(
        <HardwareProfileFields
            profile={{
                ...profile,
                presentation: {
                    type: "none",
                    video_model: "none",
                    keyboard: "none",
                    pointer: "none",
                    usb_controller: "qemu-xhci",
                    keyboard_explicit: true,
                },
            }}
        />,
    );
    expect(screen.getByText("qemu-xhci (standalone)")).toBeTruthy();
    expect(screen.getByText("none (explicit)")).toBeTruthy();
    expect(
        screen.getByText("none (new VM default; unconstrained on adoption)"),
    ).toBeTruthy();
});

it("requires configuration for an unmatched NIC and an explicit choice for ambiguous matches", async () => {
    const vm = {
        id: "vm",
        name: "External",
        cloud: "local",
        node: "host",
        interfaces: [
            { device_id: "mac:01", mac: "01", binding: { type: "qemu-user" } },
        ],
    } as VM;
    show(<AdoptMachine vm={vm} onClose={vi.fn()} />);
    expect(
        await screen.findByText(/add a matching network and host binding/),
    ).toBeTruthy();
    expect(
        (
            screen.getByRole("button", {
                name: "Adopt machine",
            }) as HTMLButtonElement
        ).disabled,
    ).toBe(true);
    cleanup();
    show(<AdoptMachine vm={vm} onClose={vi.fn()} />, [
        { id: "one", kind: "qemu-user", backend: "" },
        { id: "two", kind: "qemu-user", backend: "" },
    ]);
    const network = await screen.findByRole("combobox", {
        name: "Network for 01",
    });
    await userEvent.click(network);
    await userEvent.click(screen.getByRole("option", { name: "two" }));
    expect(network.textContent).toContain("two");
});
