import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MachineDetail } from "./MachineDetail";
import type { VM } from "../api/hooks";

const machine = vi.hoisted(() => ({ data: {} as VM }));
vi.mock("../api/hooks", async (original) => ({
    ...(await original<typeof import("../api/hooks")>()),
    useMachine: () => ({ data: machine.data, isPending: false }),
}));
vi.mock("../api/tasks", () => ({
    useResourceBusy: () => ({ unavailable: false }),
    useResourceTasks: () => ({ data: { items: [] }, isError: false }),
}));
afterEach(cleanup);
function show(overrides: Partial<VM> = {}) {
    machine.data = {
        id: "vm-1",
        name: "Web server",
        cloud: "west",
        zone: "zone-a",
        node: "host-a",
        state: "stopped",
        managed: true,
        vcpus: 4,
        memory_bytes: 4 * 2 ** 30,
        arch: "x86_64",
        instance_type: "medium",
        image: "ubuntu",
        cpu_request: 4,
        memory_request_bytes: 4 * 2 ** 30,
        interfaces: [],
        disks: [],
        ip_addresses: [],
        configured_keys: [],
        cpu_pinning: { emulator: [], vcpus: [] },
        firmware: "efi",
        disk_controller: "virtio",
        autostart: false,
        guest_agent: false,
        hugepages: false,
        graphics: "vnc",
        graphics_listen: "",
        video_model: "virtio",
        ...overrides,
    };
    render(
        <QueryClientProvider client={new QueryClient()}>
            <MemoryRouter>
                <MachineDetail />
            </MemoryRouter>
        </QueryClientProvider>,
    );
}
it("groups machine placement, access and hardware and explains absent addresses and pinning", () => {
    show();
    const placement = screen.getByRole("region", {
        name: "Identity and placement",
    });
    expect(
        within(placement)
            .getByRole("link", { name: "host-a" })
            .getAttribute("href"),
    ).toBe("/nodes/host-a/");
    expect(within(placement).getByText("ubuntu")).toBeTruthy();
    expect(
        within(
            screen.getByRole("region", { name: "Network access" }),
        ).getByText("No IP addresses reported."),
    ).toBeTruthy();
    expect(
        within(
            screen.getByRole("region", { name: "Console and CPU placement" }),
        ).getAllByText("Not pinned"),
    ).toHaveLength(2);
    expect(screen.getByText("No configured keys.")).toBeTruthy();
});
it("distinguishes unavailable key data from an empty key configuration and keeps read-only status visible", () => {
    show({
        configured_keys: null,
        managed: false,
        ip_addresses: ["192.0.2.10"],
    });
    expect(screen.getByText("Key configuration is unavailable.")).toBeTruthy();
    expect(screen.queryByText("No configured keys.")).toBeNull();
    expect(screen.getByText("Read-only · adoption required")).toBeTruthy();
    expect(
        screen.getByRole("button", { name: "Copy IP 192.0.2.10" }),
    ).toBeTruthy();
});
