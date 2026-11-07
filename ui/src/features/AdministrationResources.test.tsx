import { afterEach, expect, it, vi } from "vitest";
import {
    act,
    cleanup,
    fireEvent,
    render,
    screen,
    waitFor,
    within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AdministrationInventory } from "./AdministrationResources";
import { APIError } from "../api/client";
import { Appearance } from "../app/theme";
const mocks = vi.hoisted(() => ({ request: vi.fn(), principal: "operator" }));
vi.mock("../api/client", async (original) => ({
    ...(await original<typeof import("../api/client")>()),
    request: mocks.request,
}));
vi.mock("../app/Auth", () => ({ useMe: () => ({ id: mocks.principal }) }));
vi.mock("./AdministrationOperations", () => ({
    AdministrationResourceTasks: () => <div>Related task history</div>,
}));
const machine = {
    id: "vm-one",
    name: "Alpha",
    node: "host-one",
    cloud: "west",
    namespace_id: "ops",
    state: "running",
    vcpus: 2,
    memory_bytes: 2 ** 30,
    ip_addresses: ["192.0.2.1"],
    disks: [
        { volume_id: "disk-one", alias: "Root", device: "disk", bus: "virtio" },
    ],
    interfaces: [{ network: "lan", mac: "aa:bb", ip_addresses: [] }],
    image: "ubuntu",
    instance_type: "small",
    arch: "aarch64",
};
const unassigned = {
    ...machine,
    id: "vm-two",
    name: "Beta",
    namespace_id: undefined,
    node: "host-two",
    state: "stopped",
    vcpus: 10,
    memory_bytes: 10 * 2 ** 30,
};
function setup(
    path = "/administration/clouds/west/machines/?namespace=workload",
    themed = false,
) {
    const queries = new QueryClient({
        defaultOptions: { queries: { retry: false } },
    });
    const router = createMemoryRouter(
        ["machines", "volumes"].flatMap((kind) => [
            {
                path: `/administration/clouds/:cloud/${kind}/`,
                element: (
                    <AdministrationInventory
                        kind={kind as "machines" | "volumes"}
                    />
                ),
            },
            {
                path: `/administration/clouds/:cloud/${kind}/:id/`,
                element: (
                    <AdministrationInventory
                        kind={kind as "machines" | "volumes"}
                    />
                ),
            },
        ]),
        { initialEntries: [path] },
    );
    render(
        <QueryClientProvider client={queries}>
            {themed ? (
                <Appearance>
                    <RouterProvider router={router} />
                </Appearance>
            ) : (
                <RouterProvider router={router} />
            )}
        </QueryClientProvider>,
    );
    return { router, queries };
}
function base() {
    mocks.request.mockImplementation(async (path: string) =>
        path.endsWith("/machines")
            ? { items: [machine, unassigned], unavailable_hosts: [] }
            : path.endsWith("/vm-one")
              ? machine
              : path.endsWith("/vm-two")
                ? unassigned
                : path === "/api/v1/namespaces"
                  ? {
                        items: [
                            { id: "ops", name: "Operations", clouds: ["west"] },
                        ],
                    }
                  : Promise.reject(new Error(`Unexpected request ${path}`)),
    );
}
afterEach(() => {
    cleanup();
    mocks.request.mockReset();
    mocks.principal = "operator";
    localStorage.clear();
    sessionStorage.clear();
    vi.unstubAllGlobals();
});

it("loads one inventory, sorts allocated resources numerically, and fetches only selected details", async () => {
    base();
    const { router } = setup();
    await screen.findByRole("button", { name: "Alpha" });
    expect(mocks.request.mock.calls.map(([p]) => p)).toEqual([
        "/api/v1/administration/clouds/west/machines",
    ]);
    expect(screen.queryByRole("columnheader", { name: "ID" })).toBeNull();
    await userEvent.click(screen.getByText("vCPU"));
    expect(
        screen
            .getAllByRole("button", { name: /Alpha|Beta/ })
            .map((n) => n.textContent),
    ).toEqual(["Alpha", "Beta"]);
    await userEvent.click(screen.getByText("vCPU"));
    expect(
        screen
            .getAllByRole("button", { name: /Alpha|Beta/ })
            .map((n) => n.textContent),
    ).toEqual(["Beta", "Alpha"]);
    await userEvent.click(screen.getByRole("button", { name: "Alpha" }));
    const inspector = await screen.findByRole("complementary", {
        name: "Machine inspector",
    });
    await within(inspector).findByRole("heading", { name: "Alpha" });
    expect(mocks.request.mock.calls.map(([p]) => p)).toEqual([
        "/api/v1/administration/clouds/west/machines",
        "/api/v1/administration/clouds/west/machines/vm-one",
    ]);
    expect(
        new URLSearchParams(router.state.location.search).get("selected"),
    ).toBe("vm-one");
    expect(screen.getByRole("columnheader", { name: "RAM" })).toBeTruthy();
    expect(document.activeElement).toBe(inspector);
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("complementary")).toBeNull();
    expect(document.activeElement).toBe(
        screen.getByRole("button", { name: "Alpha" }),
    );
});

it("filters all namespaces including Unassigned without changing workload context", async () => {
    base();
    const { router } = setup();
    await screen.findByRole("button", { name: "Alpha" });
    await userEvent.click(screen.getByRole("combobox", { name: "Namespace" }));
    await userEvent.click(screen.getByRole("option", { name: "Unassigned" }));
    const params = new URLSearchParams(router.state.location.search);
    expect(params.get("namespace")).toBe("workload");
    expect(params.get("inventory_namespace")).toBe("Unassigned");
    expect(screen.queryByRole("button", { name: "Alpha" })).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Beta" }));
    const inspector = screen.getByRole("complementary");
    await within(inspector).findByRole("heading", { name: "Beta" });
    expect(
        mocks.request.mock.calls.some(([p]) => p === "/api/v1/namespaces"),
    ).toBe(false);
    await userEvent.click(
        within(inspector).getByRole("button", { name: "Assign to namespace…" }),
    );
    await userEvent.click(
        await within(inspector).findByRole("combobox", {
            name: "Destination namespace",
        }),
    );
    await userEvent.click(screen.getByRole("option", { name: "Operations" }));
    expect(
        within(inspector)
            .getByRole("link", { name: "Open assignment preview" })
            .getAttribute("href"),
    ).toBe("/machines/west/vm-two/assign/?namespace=ops");
    expect(
        mocks.request.mock.calls.every(([, method]) => method === "GET"),
    ).toBe(true);
});

it("retains filters, selected resource, and list scroll through full detail navigation", async () => {
    base();
    const { router } = setup(
        "/administration/clouds/west/machines/?namespace=workload&inventory_namespace=ops&q=Alpha",
    );
    await userEvent.click(await screen.findByRole("button", { name: "Alpha" }));
    const container = screen.getAllByLabelText("Scrollable resources")[0];
    container.scrollTop = 180;
    container.scrollLeft = 90;
    fireEvent.scroll(container);
    const inspector = screen.getByRole("complementary");
    await within(inspector).findByRole("heading", { name: "Alpha" });
    // Main inventory search must not filter nested disks or interfaces.
    expect(within(inspector).getByRole("link", { name: "Root" })).toBeTruthy();
    await userEvent.click(
        within(inspector).getByRole("link", { name: "Full details" }),
    );
    await userEvent.click(
        await screen.findByRole("link", { name: "Back to inventory" }),
    );
    await screen.findByRole("button", { name: "Alpha" });
    expect(router.state.location.search).toContain("inventory_namespace=ops");
    expect(router.state.location.search).toContain("q=Alpha");
    expect(router.state.location.search).toContain("selected=vm-one");
    expect(screen.getAllByLabelText("Scrollable resources")[0].scrollTop).toBe(
        180,
    );
    expect(screen.getAllByLabelText("Scrollable resources")[0].scrollLeft).toBe(
        90,
    );
});

it.each([
    [{ items: [], unavailable_hosts: [] }, "No machines in this cloud."],
    [
        { items: [], unavailable_hosts: ["offline"] },
        "Live inventory is unavailable; an empty inventory cannot be confirmed.",
    ],
    [
        { items: [machine], unavailable_hosts: ["offline"] },
        "Inventory is incomplete.",
    ],
])(
    "distinguishes empty and incomplete inventory %#",
    async (response, message) => {
        mocks.request.mockResolvedValue(response);
        setup();
        expect(
            await screen.findByText(new RegExp(message.replaceAll(".", "\\."))),
        ).toBeTruthy();
        if (response.unavailable_hosts.length)
            expect(screen.queryByText("No machines in this cloud.")).toBeNull();
    },
);

it("removes denied data and does not reuse data across principal or cloud", async () => {
    base();
    const { router } = setup();
    await screen.findByRole("button", { name: "Alpha" });
    mocks.request.mockRejectedValue(
        new APIError(403, { detail: "Access denied" }),
    );
    await userEvent.click(screen.getByRole("button", { name: "Refresh" }));
    expect(await screen.findByText("Access denied")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Alpha" })).toBeNull();
    mocks.principal = "other";
    mocks.request.mockImplementation(() => new Promise(() => {}));
    await act(() =>
        router.navigate(
            "/administration/clouds/east/machines/?selected=vm-one",
        ),
    );
    await waitFor(() =>
        expect(mocks.request.mock.calls.at(-1)?.[0]).toBe(
            "/api/v1/administration/clouds/east/machines",
        ),
    );
    expect(screen.queryByRole("button", { name: "Alpha" })).toBeNull();
    expect(screen.queryByRole("heading", { name: "Alpha" })).toBeNull();
});

it("shows volume allocation and dependency columns with an attachment filter", async () => {
    mocks.request.mockResolvedValue({
        items: [
            {
                id: "vol-1",
                name: "Disk",
                namespace_id: "ops",
                node: "host-one",
                size_bytes: 10 * 2 ** 30,
                storage_class: "fast",
                pool: "ssd",
                attached_to: "vm-one",
                clone_mode: "overlay",
                protected: true,
            },
        ],
        unavailable_hosts: [],
    });
    const { router } = setup(
        "/administration/clouds/west/volumes/?namespace=workload",
    );
    await screen.findByRole("button", { name: "Disk" });
    expect(screen.getByRole("cell", { name: "10 GiB" })).toBeTruthy();
    expect(screen.getByRole("cell", { name: "fast / ssd" })).toBeTruthy();
    expect(screen.getByRole("cell", { name: "overlay" })).toBeTruthy();
    expect(screen.getByRole("cell", { name: "Yes" })).toBeTruthy();
    await userEvent.click(screen.getByRole("combobox", { name: "Attachment" }));
    await userEvent.click(screen.getByRole("option", { name: "Unattached" }));
    expect(screen.getByText(/No matches/)).toBeTruthy();
    expect(router.state.location.search).toContain("attachment=unattached");
    expect(mocks.request).toHaveBeenCalledTimes(1);
});

it("uses the active dark palette for the inspector and sticky table heading", async () => {
    base();
    vi.stubGlobal("matchMedia", () => ({
        matches: false,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
    }));
    localStorage.setItem("vmango.theme", "dark");
    setup(undefined, true);
    await userEvent.click(await screen.findByRole("button", { name: "Alpha" }));
    const inspector = screen.getByRole("complementary", {
        name: "Machine inspector",
    });
    expect(getComputedStyle(inspector).backgroundColor).toBe("rgb(32, 38, 45)");
    expect(getComputedStyle(inspector).borderTopColor).toBe("rgb(69, 80, 92)");
    const heading = screen.getAllByRole("table")[0].querySelector("thead")!;
    expect(getComputedStyle(heading).backgroundColor).toBe("rgb(32, 38, 45)");
});
