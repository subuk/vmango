import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Networks } from "./Lists";
import { NodeDetail } from "./NodeDetail";

const request = vi.hoisted(() => vi.fn());
vi.mock("../api/client", async (original) => ({
    ...(await original<typeof import("../api/client")>()),
    request,
}));
afterEach(() => {
    cleanup();
    request.mockReset();
});
const catalogs = [
    {
        id: "west",
        name: "Western cloud",
        networks: [{ id: "lan", name: "Private LAN" }],
    },
    {
        id: "east",
        name: "Eastern cloud",
        networks: [
            { id: "lan", name: "Eastern LAN" },
            { id: "public", name: "Public access" },
        ],
    },
];
function show(path = "/networks/") {
    const router = createMemoryRouter(
        [{ path: "/networks/", element: <Networks /> }],
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
it.each([
    ["default", "default"],
    ["default", "br-west"],
])(
    "shows one catalog entry for hosts bound to %s and %s without reading backend diagnostics",
    async (first, second) => {
        request.mockImplementation(async (path: string) => {
            if (path === "/api/v1/clouds") return catalogs;
            if (path === "/api/v1/hosts")
                return [{ id: "host-a" }, { id: "host-b" }];
            if (path.endsWith("host-a/networks"))
                return [{ name: first, node: "host-a" }];
            if (path.endsWith("host-b/networks"))
                return [{ name: second, node: "host-b" }];
            throw new Error(path);
        });
        show();
        expect(await screen.findAllByText("Private LAN")).toHaveLength(1);
        expect(screen.getByText("lan")).toBeTruthy();
        expect(
            screen.getByRole("columnheader", { name: "Network ID" }),
        ).toBeTruthy();
        expect(screen.queryByText("host-a")).toBeNull();
        expect(screen.queryByText("default")).toBeNull();
        expect(request.mock.calls.map(([path]) => path)).toEqual([
            "/api/v1/clouds",
        ]);
    },
);
it("scopes networks to the selected cloud and updates the URL when switching", async () => {
    request.mockResolvedValue(catalogs);
    const router = show("/networks/?cloud=east");
    expect(await screen.findByText("Eastern LAN")).toBeTruthy();
    expect(screen.queryByText("Private LAN")).toBeNull();
    await userEvent.click(screen.getByRole("combobox", { name: "Cloud" }));
    await userEvent.click(
        screen.getByRole("option", { name: "Western cloud" }),
    );
    expect(await screen.findByText("Private LAN")).toBeTruthy();
    expect(screen.queryByText("Eastern LAN")).toBeNull();
    expect(router.state.location.search).toBe("?cloud=west");
});
it("shows loading until the catalog arrives", async () => {
    request.mockReturnValue(new Promise(() => {}));
    show();
    expect(screen.getByRole("status").textContent).toContain("Loading");
    expect(screen.queryByRole("combobox")).toBeNull();
});
it("explains an empty cloud configuration", async () => {
    request.mockResolvedValue([]);
    show();
    expect(await screen.findByText(/No clouds are configured/)).toBeTruthy();
});
it("explains an empty network catalog", async () => {
    request.mockResolvedValue([{ ...catalogs[0], networks: [] }]);
    show();
    expect(
        await screen.findByText(
            "No logical networks are configured in this cloud.",
        ),
    ).toBeTruthy();
});
it("does not silently show another cloud for an unknown selection", async () => {
    request.mockResolvedValue(catalogs);
    show("/networks/?cloud=removed");
    expect(
        await screen.findByText(
            "Select a configured cloud to view its logical networks.",
        ),
    ).toBeTruthy();
    expect(screen.queryByText("Private LAN")).toBeNull();
});

it("keeps backend network names in explicitly labeled host diagnostics", async () => {
    request.mockImplementation(async (path: string) => {
        if (path.endsWith("/networks"))
            return [{ node: "host-a", name: "backend-private" }];
        if (path.endsWith("/pools")) return [];
        return {
            id: "host-a",
            hostname: "host-a",
            arch: "x86_64",
            memory_bytes: 1024,
            cpus: [],
            numa: [],
        };
    });
    const router = createMemoryRouter(
        [{ path: "/nodes/:node/", element: <NodeDetail /> }],
        { initialEntries: ["/nodes/host-a/"] },
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
    expect(await screen.findByText("backend-private")).toBeTruthy();
    expect(
        screen.getByRole("heading", {
            name: "Backend networks (read-only diagnostics)",
        }),
    ).toBeTruthy();
    expect(request).toHaveBeenCalledWith("/api/v1/hosts/host-a/networks");
});
