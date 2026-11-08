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
function show(element: React.ReactNode) {
    mocks.request.mockImplementation(async (path: string, method: string) => {
        if (method === "POST") return { id: "accepted" };
        if (path.endsWith("/configuration")) return { snapshot_id: "snapshot" };
        if (path.endsWith("/hardware-profiles")) return [profile];
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
it("requires explicit profile choice before adopting and sends only the selected reference", async () => {
    const close = vi.fn();
    show(
        <AdoptMachine
            vm={{ id: "vm", name: "Existing VM", cloud: "local" } as VM}
            onClose={close}
        />,
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
    expect(button.disabled).toBe(false);
    expect(screen.getByText(/preserves existing hardware/)).toBeTruthy();
    await userEvent.click(button);
    await waitFor(() =>
        expect(mocks.request).toHaveBeenCalledWith(
            "/api/v1/clouds/local/machines/vm/adopt",
            "POST",
            { hardware_profile: "linux-scsi" },
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
