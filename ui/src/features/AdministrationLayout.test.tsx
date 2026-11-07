import { afterEach, expect, it, vi } from "vitest";
import {
    act,
    cleanup,
    render,
    screen,
    waitFor,
    within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryRouter, RouterProvider } from "react-router";
import { AdministrationLayout } from "./AdministrationLayout";
const mocks = vi.hoisted(() => ({
    request: vi.fn(),
    identity: { id: "one" },
    listeners: new Set<() => void>(),
}));
vi.mock("../api/client", async (original) => ({
    ...(await original<typeof import("../api/client")>()),
    request: mocks.request,
}));
vi.mock("../app/Auth", async () => {
    const { useSyncExternalStore } = await import("react");
    return {
        useMe: () =>
            useSyncExternalStore(
                (fn) => {
                    mocks.listeners.add(fn);
                    return () => {
                        mocks.listeners.delete(fn);
                    };
                },
                () => mocks.identity,
            ),
    };
});
const cloud = (id: string, hosts = true) => ({
    id,
    name: id.toUpperCase(),
    capabilities: { host_inventory: hosts },
});
function setup(path: string) {
    mocks.request.mockImplementation(async (path: string) =>
        path === "/api/v1/administration/configuration"
            ? { snapshot_id: "accepted" }
            : path === "/api/v1/administration/clouds"
              ? [cloud("west"), cloud("east"), cloud("remote", false)]
              : cloud(path.split("/").at(-1)!),
    );
    const queries = new QueryClient({
        defaultOptions: { queries: { retry: false } },
    });
    const router = createMemoryRouter(
        [
            {
                element: <AdministrationLayout />,
                children: [
                    {
                        path: "/administration/*",
                        element: <div>Cloud content</div>,
                    },
                    { path: "/tasks/*", element: <div>Workspace history</div> },
                ],
            },
        ],
        { initialEntries: [path] },
    );
    render(
        <QueryClientProvider client={queries}>
            <RouterProvider router={router} />
        </QueryClientProvider>,
    );
    return { router, queries };
}
afterEach(() => {
    cleanup();
    mocks.request.mockReset();
    mocks.identity = { id: "one" };
});
it.each(["machines", "volumes", "images", "configuration", "hosts"])(
    "switches clouds within %s, preserving reusable filters and dropping resource placement",
    async (section) => {
        const { router } = setup(
            `/administration/clouds/west/${section}/selected/?namespace=workload&inventory_namespace=ops&q=ubuntu&sort=name&direction=desc&state=running&selected=vm&node=host&pool=ssd&disk_q=root`,
        );
        await screen.findByRole("navigation", { name: "Cloud administration" });
        await userEvent.click(
            screen.getByRole("combobox", { name: "Selected cloud" }),
        );
        await userEvent.click(
            await screen.findByRole("option", { name: "EAST" }),
        );
        await waitFor(() =>
            expect(router.state.location.pathname).toBe(
                `/administration/clouds/east/${section}/`,
            ),
        );
        const params = new URLSearchParams(router.state.location.search);
        expect(Object.fromEntries(params)).toEqual({
            q: "ubuntu",
            sort: "name",
            direction: "desc",
            inventory_namespace: "ops",
            state: "running",
            namespace: "workload",
        });
    },
);
it("falls back to cloud overview when the new cloud has no hosts", async () => {
    const { router } = setup(
        "/administration/clouds/west/hosts/host-one/?namespace=ops&selected=host-one",
    );
    await screen.findByRole("navigation", { name: "Cloud administration" });
    await userEvent.click(
        screen.getByRole("combobox", { name: "Selected cloud" }),
    );
    await userEvent.click(
        await screen.findByRole("option", { name: "REMOTE" }),
    );
    expect(router.state.location.pathname).toBe(
        "/administration/clouds/remote/",
    );
    expect(router.state.location.search).toBe("?namespace=ops");
});
it("separates administrative operations from workload tasks and retains workspace context", async () => {
    const { router } = setup("/administration/clouds/west/?namespace=workload");
    const nav = screen.getByRole("navigation", {
        name: "Operations navigation",
    });
    expect(
        within(nav)
            .getByRole("link", { name: "Task history" })
            .getAttribute("href"),
    ).toBe("/administration/tasks/?namespace=workload");
    expect(
        within(nav)
            .getByRole("link", { name: "Workspace tasks" })
            .getAttribute("href"),
    ).toBe("/tasks/?namespace=workload");
    await userEvent.click(
        within(nav).getByRole("link", { name: "Workspace tasks" }),
    );
    expect(router.state.location.pathname).toBe("/tasks/");
    expect(await screen.findByText("Workspace history")).toBeTruthy();
    expect(
        within(nav)
            .getByRole("link", { name: "Workspace tasks" })
            .getAttribute("aria-current"),
    ).toBe("page");
});
it("hides old cloud configuration while a new principal loads", async () => {
    const { queries } = setup("/administration/clouds/west/");
    await screen.findByRole("navigation", { name: "Cloud administration" });
    mocks.request.mockImplementation(() => new Promise(() => {}));
    act(() => {
        mocks.identity = { id: "two" };
        mocks.listeners.forEach((fn) => fn());
    });
    expect(
        screen.queryByRole("navigation", { name: "Cloud administration" }),
    ).toBeNull();
    await waitFor(() =>
        expect(
            queries
                .getQueryCache()
                .getAll()
                .some((q) => q.queryKey[1] === "two"),
        ).toBe(true),
    );
});
