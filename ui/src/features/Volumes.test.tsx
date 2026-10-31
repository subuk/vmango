import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Volumes, VolumeDialog } from "./Volumes";
const request = vi.hoisted(() => vi.fn());
vi.mock("../api/client", async (original) => ({
    ...(await original<typeof import("../api/client")>()),
    request,
}));
vi.mock("../api/tasks", async (original) => ({
    ...(await original<typeof import("../api/tasks")>()),
    useResourceBusy: () => ({ unavailable: false }),
    useResourceTasks: () => ({ data: { items: [] } }),
}));
afterEach(() => {
    cleanup();
    request.mockReset();
});
const clouds = [
    {
        id: "west",
        name: "Western",
        zones: [],
        storage_classes: [],
        storage_availability: [],
    },
    {
        id: "east",
        name: "Eastern",
        zones: [],
        storage_classes: [],
        storage_availability: [],
    },
];
const volume = (node: string) => ({
    id: `${btoa(node).replace(/=+$/, "")}.L3NhbWU`,
    node,
    cloud: "west",
    name: "Equal name",
    zone: "z",
    pool: "pool",
    size_bytes: 1024,
    format: "raw",
    protected: false,
    owned: true,
});
function show(path = "/volumes/") {
    const router = createMemoryRouter(
        [{ path: "/volumes/", element: <Volumes /> }],
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
it("loads only the selected cloud and switches through the URL", async () => {
    request.mockImplementation(async (path: string) =>
        path === "/api/v1/clouds"
            ? clouds
            : { items: [], unavailable_hosts: [] },
    );
    const router = show("/volumes/?cloud=east");
    await screen.findByText("No visible volumes are available.");
    expect(
        request.mock.calls.some(([p]) => p === "/api/v1/clouds/east/volumes"),
    ).toBe(true);
    expect(
        request.mock.calls.some(([p]) => p === "/api/v1/clouds/west/volumes"),
    ).toBe(false);
    await userEvent.click(screen.getByRole("combobox", { name: "Cloud" }));
    await userEvent.click(screen.getByRole("option", { name: "Western" }));
    await waitFor(() =>
        expect(router.state.location.search).toBe("?cloud=west"),
    );
    expect(
        request.mock.calls.some(([p]) => p === "/api/v1/clouds/west/volumes"),
    ).toBe(true);
});
it("keeps equal names on different hosts as separate rows without host discovery", async () => {
    request.mockImplementation(async (path: string) =>
        path === "/api/v1/clouds"
            ? clouds
            : { items: [volume("a"), volume("b")], unavailable_hosts: [] },
    );
    show();
    expect(await screen.findAllByText("Equal name")).toHaveLength(2);
    expect(screen.queryByRole("columnheader", { name: "Host" })).toBeNull();
    expect(
        request.mock.calls.every(([p]) => !String(p).includes("/hosts")),
    ).toBe(true);
});
it.each([false, true])(
    "reports incomplete inventory even when every host fails (%s)",
    async (all) => {
        request.mockImplementation(async (path: string) =>
            path === "/api/v1/clouds"
                ? clouds
                : {
                      items: all ? [] : [volume("a")],
                      unavailable_hosts: ["offline"],
                  },
        );
        show();
        expect(
            await screen.findByText(
                /The list is incomplete. Unavailable hosts: offline/,
            ),
        ).toBeTruthy();
        if (!all) expect(screen.getByText("Equal name")).toBeTruthy();
    },
);
it("starts creation in the selected cloud", async () => {
    request.mockImplementation(async (path: string) =>
        path === "/api/v1/clouds"
            ? clouds
            : { items: [], unavailable_hosts: [] },
    );
    show("/volumes/?cloud=east");
    await screen.findByText("No visible volumes are available.");
    await userEvent.click(
        screen.getByRole("button", { name: "Create volume" }),
    );
    expect(
        screen.getAllByRole("combobox", { name: "Cloud" }).at(-1)?.textContent,
    ).toContain("Eastern");
});

it.each([
    null,
    { storage_classes: [] },
    { storage_classes: [{ id: "standard", name: "Standard" }] },
])("clones using only resource-detail choices (%j)", async (catalog) => {
    const v = volume("a");
    request.mockResolvedValue({ ...v, catalog });
    render(
        <QueryClientProvider
            client={
                new QueryClient({
                    defaultOptions: { queries: { retry: false } },
                })
            }
        >
            <VolumeDialog
                mode="clone"
                volume={v as import("../api/hooks").Volume}
                onClose={() => {}}
            />
        </QueryClientProvider>,
    );
    if (catalog === null)
        expect(
            await screen.findByText("Storage choices are unavailable."),
        ).toBeTruthy();
    else if (catalog.storage_classes.length === 0)
        expect(
            await screen.findByText(
                "No storage classes are available for cloning.",
            ),
        ).toBeTruthy();
    else
        await waitFor(() =>
            expect(
                (
                    screen.getByRole("button", {
                        name: "Clone volume",
                    }) as HTMLButtonElement
                ).disabled,
            ).toBe(false),
        );
    expect(
        request.mock.calls.every(
            ([path]) => path === `/api/v1/clouds/west/volumes/${v.id}`,
        ),
    ).toBe(true);
    if (!catalog?.storage_classes.length)
        expect(
            (
                screen.getByRole("button", {
                    name: "Clone volume",
                }) as HTMLButtonElement
            ).disabled,
        ).toBe(true);
});
