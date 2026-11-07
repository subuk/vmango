import { afterEach, expect, it, vi } from "vitest";
import {
    cleanup,
    fireEvent,
    render,
    screen,
    within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
    AdministrationHosts,
    AdministrationHostDetail,
} from "./AdministrationHosts";
import type { AdminCloud, AdminHost } from "../api/administration";
const request = vi.hoisted(() => vi.fn());
vi.mock("../api/client", async (original) => ({
    ...(await original<typeof import("../api/client")>()),
    request,
}));
vi.mock("../app/Auth", () => ({ useMe: () => ({ id: "admin" }) }));
vi.mock("./AdministrationOperations", () => ({
    AdministrationResourceTasks: () => <div>Recent tasks</div>,
}));
const cloud: AdminCloud = {
    id: "west",
    name: "West",
    backend_kind: "libvirt",
    capabilities: { host_inventory: true, image_availability: true },
    zones: [],
    instance_types: [],
    networks: [],
    storage_classes: [],
    images: [],
};
const host: AdminHost = {
    id: "offline",
    cloud_id: "west",
    zone: "zone-a",
    labels: { tier: "gold" },
    cpu_overcommit: 2,
    cpu_reserve: 1,
    memory_reserve_bytes: 1024,
    emulator: { value: "", state: "defaulted", source: "backend" },
    image_pool: "images",
    config_drive_pool: "cloudinit",
    config_drive_suffix: ".iso",
    config_drive_format: "iso",
    graphics_listen: "127.0.0.1",
    cache: true,
    qcow_prealloc_metadata: false,
    networks: [
        {
            id: "lan",
            kind: "network",
            backend: "backend-private",
            model: "virtio",
            vlan: 0,
        },
    ],
    storage: [{ id: "fast", pool: "ssd", format: "qcow2" }],
};
const base = "/api/v1/administration/clouds/west";
function fixture(path: string) {
    if (path === "/api/v1/administration/configuration")
        return { snapshot_id: "accepted" };
    if (path === `${base}/configuration/hosts`)
        return [
            host,
            { id: "online", cloud_id: "west", zone: "zone-b" },
            { id: "unknown", cloud_id: "west", zone: "zone-a" },
        ];
    if (path === `${base}/configuration/hosts/offline`) return host;
    if (path === `${base}/hosts`)
        return {
            items: [
                {
                    id: "online",
                    hostname: "online.local",
                    arch: "x86_64",
                    cpus: [],
                    memory_bytes: 4096,
                },
            ],
            unavailable_hosts: [{ id: "offline", code: "host_unavailable" }],
        };
    throw new Error("Host is unavailable");
}
function show(
    path = "/administration/clouds/west/hosts/",
    value = cloud,
    respond: (path: string) => unknown = fixture,
) {
    request.mockImplementation(async (path: string) => respond(path));
    const router = createMemoryRouter(
        [
            {
                path: "/administration/clouds/:cloud/hosts/",
                element: <AdministrationHosts cloud={value} />,
            },
            {
                path: "/administration/clouds/:cloud/hosts/:node/",
                element: <AdministrationHostDetail cloud={value} />,
            },
        ],
        { initialEntries: [path] },
    );
    render(
        <QueryClientProvider
            client={
                new QueryClient({
                    defaultOptions: { queries: { retry: false } },
                })
            }
        >
            <RouterProvider router={router} />
        </QueryClientProvider>,
    );
    return router;
}
afterEach(() => {
    cleanup();
    request.mockReset();
    vi.restoreAllMocks();
    sessionStorage.clear();
});
it("keeps every configured host in one inventory without per-host requests", async () => {
    show();
    expect(await screen.findByRole("link", { name: "offline" })).toBeTruthy();
    expect(screen.getByRole("link", { name: "online" })).toBeTruthy();
    expect(screen.getByRole("link", { name: "unknown" })).toBeTruthy();
    expect(screen.getByText("Unavailable")).toBeTruthy();
    expect(screen.getByText("Observed")).toBeTruthy();
    expect(screen.getByText("Unknown")).toBeTruthy();
    expect(
        request.mock.calls.some(([path]) => String(path).includes("/hosts/")),
    ).toBe(false);
});
it("selects an offline host in place and keeps configuration usable when diagnostics fail", async () => {
    const router = show();
    await userEvent.click(
        await screen.findByRole("button", { name: "Inspect offline" }),
    );
    expect(router.state.location.search).toContain("selected=offline");
    expect(await screen.findByText("tier=gold")).toBeTruthy();
    expect(
        screen
            .getByRole("link", { name: "Open full host page" })
            .getAttribute("href"),
    ).toBe("/administration/clouds/west/hosts/offline/?selected=offline");
    expect(
        request.mock.calls.some(([path]) => path === `${base}/hosts/offline`),
    ).toBe(false);
    await userEvent.click(
        screen.getByRole("tab", { name: "Live diagnostics" }),
    );
    expect(await screen.findByText("Host is unavailable")).toBeTruthy();
    await userEvent.click(screen.getByRole("tab", { name: "Configuration" }));
    expect(await screen.findByText("tier=gold")).toBeTruthy();
    expect(
        request.mock.calls.some(([path]) =>
            /\/(pools|networks|catalog|capabilities)$/.test(String(path)),
        ),
    ).toBe(false);
});
it("loads backend networks only on their tab and preserves configured bindings on failure", async () => {
    show("/administration/clouds/west/hosts/offline/");
    await screen.findByText("tier=gold");
    await userEvent.click(screen.getByRole("tab", { name: "Networks" }));
    expect(await screen.findByText("Host is unavailable")).toBeTruthy();
    const accepted = screen.getByRole("region", {
        name: "Accepted network bindings",
    });
    expect(within(accepted).getByText("backend-private")).toBeTruthy();
    expect(
        screen.getByRole("region", { name: "Observed backend networks" }),
    ).toBeTruthy();
    expect(
        request.mock.calls.filter(
            ([path]) => path === `${base}/hosts/offline/networks`,
        ),
    ).toHaveLength(1);
    expect(
        request.mock.calls.some(([path]) =>
            /\/(pools|catalog|capabilities)$/.test(String(path)),
        ),
    ).toBe(false);
});
it("renders observed backend network names separately from logical bindings", async () => {
    show(
        "/administration/clouds/west/hosts/offline/?hostTab=networks",
        cloud,
        (path: string) =>
            path.endsWith("/networks")
                ? [{ node: "offline", name: "backend-private" }]
                : fixture(path),
    );
    const observed = await screen.findByRole("region", {
        name: "Observed backend networks",
    });
    expect(await within(observed).findByText("backend-private")).toBeTruthy();
});
it("does not issue unsupported requests for hostless clouds", async () => {
    show("/administration/clouds/west/hosts/offline/", {
        ...cloud,
        capabilities: { ...cloud.capabilities, host_inventory: false },
    });
    expect(
        screen.getByText("Host inventory is not supported by this cloud."),
    ).toBeTruthy();
    expect(request).not.toHaveBeenCalled();
});
it("rejects a host configuration returned for a different cloud before live requests", async () => {
    show(
        "/administration/clouds/west/hosts/offline/?hostTab=diagnostics",
        cloud,
        (path: string) =>
            path.endsWith("/hosts/offline")
                ? { ...host, cloud_id: "east" }
                : fixture(path),
    );
    expect(
        await screen.findByText(
            "The configured host does not belong to this cloud and host scope.",
        ),
    ).toBeTruthy();
    expect(
        request.mock.calls.some(([path]) => path === `${base}/hosts/offline`),
    ).toBe(false);
});
it("retains configured rows when the aggregate observation endpoint fails", async () => {
    show("/administration/clouds/west/hosts/", cloud, (path: string) => {
        if (path === `${base}/hosts`) throw new Error("Offline");
        return fixture(path);
    });
    expect(await screen.findByRole("link", { name: "offline" })).toBeTruthy();
    expect(
        await screen.findByText(/Live inventory could not be loaded/),
    ).toBeTruthy();
    expect(screen.getAllByText("Unknown")).toHaveLength(3);
});
it("loads expensive diagnostic catalogs only after explicit expansion", async () => {
    show(
        "/administration/clouds/west/hosts/offline/?hostTab=diagnostics",
        cloud,
        (path) => {
            if (path === `${base}/hosts/offline`)
                return {
                    id: "offline",
                    hostname: "offline.local",
                    arch: "x86_64",
                    memory_bytes: 4096,
                    cpus: [],
                    numa: [],
                    threads_per_core: 2,
                    iommu: false,
                };
            if (path.endsWith("/capabilities")) return { arch: "x86_64" };
            if (path.endsWith("/catalog"))
                return { networks: [], storage_classes: [] };
            return fixture(path);
        },
    );
    const expand = await screen.findByRole("button", {
        name: "Load launch capabilities and logical catalog",
    });
    expect(
        request.mock.calls.some(([path]) =>
            /\/(catalog|capabilities|networks|pools)$/.test(String(path)),
        ),
    ).toBe(false);
    await userEvent.click(expand);
    expect(
        await screen.findByRole("region", { name: "Live logical catalog" }),
    ).toBeTruthy();
    expect(
        request.mock.calls.filter(
            ([path]) => path === `${base}/hosts/offline/catalog`,
        ),
    ).toHaveLength(1);
    expect(
        request.mock.calls.filter(
            ([path]) => path === `${base}/hosts/offline/capabilities`,
        ),
    ).toHaveLength(1);
});
it("loads cloud resource inventories lazily and shows only this cloud and host", async () => {
    show("/administration/clouds/west/hosts/offline/", cloud, (path) => {
        if (path === `${base}/machines`)
            return {
                items: [
                    {
                        id: "mine",
                        name: "Mine",
                        cloud: "west",
                        node: "offline",
                        state: "running",
                        vcpus: 2,
                        memory_bytes: 1024,
                    },
                    {
                        id: "foreign",
                        name: "Foreign",
                        cloud: "east",
                        node: "offline",
                    },
                    {
                        id: "other",
                        name: "Other host",
                        cloud: "west",
                        node: "online",
                    },
                ],
                unavailable_hosts: [],
            };
        if (path === `${base}/volumes`)
            return {
                items: [
                    {
                        id: "disk",
                        name: "Disk",
                        cloud: "west",
                        node: "offline",
                        size_bytes: 1024,
                        pool: "ssd",
                        attached_to: "mine",
                    },
                ],
                unavailable_hosts: [],
            };
        return fixture(path);
    });
    await screen.findByText("tier=gold");
    expect(
        request.mock.calls.some(([path]) =>
            /\/(machines|volumes)$/.test(String(path)),
        ),
    ).toBe(false);
    await userEvent.click(
        screen.getByRole("tab", { name: "Machines & volumes" }),
    );
    expect(await screen.findByRole("link", { name: "Mine" })).toBeTruthy();
    expect(screen.queryByText("Foreign")).toBeNull();
    expect(screen.queryByText("Other host")).toBeNull();
    expect(await screen.findByRole("link", { name: "Disk" })).toBeTruthy();
    expect(
        screen.getByRole("link", { name: "mine" }).getAttribute("href"),
    ).toBe("/administration/clouds/west/machines/mine/");
    expect(
        request.mock.calls.filter(([path]) => path === `${base}/machines`),
    ).toHaveLength(1);
    expect(
        request.mock.calls.filter(([path]) => path === `${base}/volumes`),
    ).toHaveLength(1);
});
it("preserves host filters, selected tab and list position through full detail navigation", async () => {
    const scroll = vi.spyOn(window, "scrollTo").mockImplementation(() => {});
    const router = show(
        "/administration/clouds/west/hosts/?q=offline&zone=zone-a&sort=id&direction=desc",
    );
    await userEvent.click(
        await screen.findByRole("button", { name: "Inspect offline" }),
    );
    await userEvent.click(screen.getByRole("tab", { name: "Networks" }));
    const list = screen.getAllByLabelText("Scrollable resources")[0];
    list.scrollLeft = 120;
    fireEvent.scroll(list);
    await userEvent.click(
        screen.getByRole("link", { name: "Open full host page" }),
    );
    expect(router.state.location.pathname).toBe(
        "/administration/clouds/west/hosts/offline/",
    );
    expect(
        screen
            .getByRole("tab", { name: "Networks" })
            .getAttribute("aria-selected"),
    ).toBe("true");
    await userEvent.click(screen.getByRole("link", { name: "All hosts" }));
    const params = new URLSearchParams(router.state.location.search);
    expect(params.get("q")).toBe("offline");
    expect(params.get("zone")).toBe("zone-a");
    expect(params.get("sort")).toBe("id");
    expect(params.get("direction")).toBe("desc");
    expect(params.get("selected")).toBe("offline");
    expect(params.get("hostTab")).toBe("networks");
    expect(
        await screen.findByRole("region", { name: "Selected host offline" }),
    ).toBeTruthy();
    expect(
        screen
            .getByRole("tab", { name: "Networks" })
            .getAttribute("aria-selected"),
    ).toBe("true");
    expect(screen.getAllByLabelText("Scrollable resources")[0].scrollLeft).toBe(
        120,
    );
    expect(scroll).toHaveBeenCalledWith(0, 0);
    await userEvent.click(
        screen.getByRole("button", { name: "Close host details" }),
    );
    expect(document.activeElement).toBe(
        screen.getByRole("button", { name: "Inspect offline" }),
    );
    await userEvent.click(
        screen.getByRole("button", { name: "Inspect offline" }),
    );
    await userEvent.keyboard("{Escape}");
    expect(
        screen.queryByRole("region", { name: "Selected host offline" }),
    ).toBeNull();
    expect(document.activeElement).toBe(
        screen.getByRole("button", { name: "Inspect offline" }),
    );
});
