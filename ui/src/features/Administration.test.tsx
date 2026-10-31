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
import { createMemoryRouter, RouterProvider } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AdministrationLayout } from "./AdministrationLayout";
import { Tasks, TaskDetail } from "./Tasks";
import { Administration } from "./Administration";
import { APIError } from "../api/client";
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
const install = {
    snapshot_id: "accepted",
    loaded_at: "2026-10-31T12:00:00Z",
    version: "test",
    activation: "restart",
    log_level: "info",
    tasks: { workers: 2, retention_days: 30 },
    image_transfers: {
        max_bytes: 1024,
        timeout_seconds: 600,
        idle_timeout_seconds: 60,
    },
};
const cloud = {
    id: "remote.cloud",
    name: "Remote cloud",
    backend_kind: "fixture",
    capabilities: { host_inventory: false, image_availability: false },
    zones: [],
    instance_types: [],
    networks: [],
    storage_classes: [],
    images: [],
};
afterEach(() => {
    cleanup();
    mocks.request.mockReset();
    mocks.identity = { id: "one" };
});
function show(path: string) {
    const queries = new QueryClient({
        defaultOptions: { queries: { retry: false, refetchInterval: 15000 } },
    });
    const router = createMemoryRouter(
        [
            {
                element: <AdministrationLayout />,
                children: [
                    { path: "/administration/*", element: <Administration /> },
                    { path: "/tasks/", element: <Tasks /> },
                    { path: "/tasks/:id/", element: <TaskDetail /> },
                ],
            },
        ],
        { initialEntries: [path] },
    );
    const tree = () => (
        <QueryClientProvider client={queries}>
            <RouterProvider router={router} />
        </QueryClientProvider>
    );
    const view = render(tree());
    return { queries, router, rerender: () => view.rerender(tree()) };
}
function base() {
    mocks.request.mockImplementation(async (path: string) =>
        path.endsWith("/configuration") && !path.includes("remote.cloud")
            ? install
            : path.endsWith("/clouds")
              ? [cloud]
              : cloud,
    );
}
it("renders hostless clouds without host navigation or diagnostic requests", async () => {
    base();
    const { queries } = show("/administration/clouds/remote.cloud/");
    expect(
        await screen.findByRole("heading", { name: "Remote cloud" }),
    ).toBeTruthy();
    expect(screen.queryByRole("link", { name: "Hosts" })).toBeNull();
    expect(await screen.findByRole("link", { name: "Images" })).toBeTruthy();
    expect(
        mocks.request.mock.calls.every(([p]) => !String(p).includes("/hosts")),
    ).toBe(true);
    expect(
        queries
            .getQueryCache()
            .getAll()
            .every(
                (q) =>
                    (q.options as { refetchInterval?: number | false })
                        .refetchInterval === false,
            ),
    ).toBe(true);
});
it("keeps a hostless configuration deep link complete and cloud scoped", async () => {
    base();
    show("/administration/clouds/remote.cloud/configuration/");
    expect(
        await screen.findByRole("heading", {
            name: "Remote cloud configuration",
        }),
    ).toBeTruthy();
    expect(
        await screen.findByRole("heading", { name: "Instance types" }),
    ).toBeTruthy();
    expect(
        screen.queryByRole("heading", { name: "Configured hosts" }),
    ).toBeNull();
});
it("replaces cached details after a denied refresh", async () => {
    base();
    show("/administration/clouds/remote.cloud/");
    await screen.findByRole("heading", { name: "Remote cloud" });
    mocks.request.mockRejectedValue(
        new APIError(403, { detail: "Access denied" }),
    );
    await userEvent.click(
        await screen.findByRole("button", { name: "Refresh" }),
    );
    expect(await screen.findByText("Access denied")).toBeTruthy();
    expect(screen.queryByRole("heading", { name: "Remote cloud" })).toBeNull();
});
it("keeps supported offline host navigation and explicit incomplete live inventory", async () => {
    mocks.request.mockImplementation(async (path: string) =>
        path.endsWith("/hosts")
            ? {
                  items: [],
                  unavailable_hosts: [
                      { id: "offline", code: "host_unavailable" },
                  ],
              }
            : path === "/api/v1/administration/configuration"
              ? install
              : {
                    ...cloud,
                    capabilities: {
                        host_inventory: true,
                        image_availability: true,
                    },
                    libvirt: {
                        hosts: [
                            { id: "offline", cloud_id: cloud.id, zone: "z" },
                        ],
                    },
                },
    );
    show("/administration/clouds/remote.cloud/hosts/");
    expect(await screen.findByText(/Unavailable hosts: offline/)).toBeTruthy();
    expect(screen.getByRole("link", { name: "Hosts" })).toBeTruthy();
    expect(screen.getByText("Live inventory is unavailable.")).toBeTruthy();
});
it("separates private configuration cache keys by principal", async () => {
    base();
    const view = show("/administration/clouds/remote.cloud/");
    await screen.findByRole("heading", { name: "Remote cloud" });
    act(() => {
        mocks.identity = { id: "two" };
        mocks.listeners.forEach((fn) => fn());
    });
    await waitFor(() =>
        expect(
            view.queries
                .getQueryCache()
                .getAll()
                .some((q) => q.queryKey[1] === "two"),
        ).toBe(true),
    );
    expect(
        view.queries
            .getQueryCache()
            .getAll()
            .every((q) => q.queryKey[0] === "administration"),
    ).toBe(true);
});

it("refreshes configuration under the new accepted snapshot after a restart", async () => {
    base();
    const { queries } = show("/administration/clouds/remote.cloud/");
    await screen.findByRole("heading", { name: "Remote cloud" });
    mocks.request.mockImplementation(async (path: string) =>
        path === "/api/v1/administration/configuration"
            ? { ...install, snapshot_id: "after-restart" }
            : cloud,
    );
    await userEvent.click(
        await screen.findByRole("button", { name: "Refresh" }),
    );
    await waitFor(() =>
        expect(
            queries
                .getQueryCache()
                .getAll()
                .some(
                    (q) =>
                        q.queryKey[3] === "after-restart" &&
                        q.state.status === "success",
                ),
        ).toBe(true),
    );
});
it("shows metadata access errors instead of an indefinite configuration spinner", async () => {
    mocks.request.mockRejectedValue(
        new APIError(403, { detail: "Configuration access denied" }),
    );
    show("/administration/clouds/");
    expect(await screen.findByText("Configuration access denied")).toBeTruthy();
    expect(screen.queryByRole("status")).toBeNull();
});

it("shows a concise overview with catalog totals and separate installation settings", async () => {
    base();
    show("/administration/");
    expect(
        await screen.findByRole("heading", { level: 1, name: "Overview" }),
    ).toBeTruthy();
    await screen.findByRole("heading", { name: "Cloud infrastructure" });
    const summary = screen.getByLabelText("Administration summary");
    expect(summary.textContent).toContain("Clouds");
    expect(
        screen.queryByRole("heading", { name: "Configured installation" }),
    ).toBeNull();
    expect(
        screen
            .getByRole("link", { name: "View configuration" })
            .getAttribute("href"),
    ).toBe("/administration/configuration/");
    expect(screen.getByRole("link", { name: "Remote cloud" })).toBeTruthy();
});
it("keeps runtime configuration on its own page", async () => {
    base();
    show("/administration/configuration/");
    expect(
        await screen.findByRole("heading", {
            level: 1,
            name: "Installation configuration",
        }),
    ).toBeTruthy();
    expect(await screen.findByText("accepted")).toBeTruthy();
    const nav = screen.getByRole("navigation", {
        name: "Administration navigation",
    });
    expect(
        within(nav)
            .getByRole("link", { name: "Configuration" })
            .getAttribute("aria-current"),
    ).toBe("page");
});
it.each(["/tasks/", "/tasks/saved-task/"])(
    "retains Administration navigation on %s",
    async (path) => {
        mocks.request.mockImplementation(async (url: string) =>
            url.includes("/tasks?")
                ? { items: [], active: 0 }
                : url.includes("/tasks/")
                  ? {
                        id: "saved-task",
                        kind: "volume.create",
                        state: "succeeded",
                        created_at: "2026-10-31T12:00:00Z",
                        initiator: { id: "one", name: "Operator" },
                        resources: [],
                        stages: [],
                        effects: [],
                        transitions: [],
                        outcome_uncertain: false,
                        blocks_resources: false,
                    }
                  : install,
        );
        const { router } = show(path);
        const nav = screen.getByRole("navigation", {
            name: "Administration navigation",
        });
        expect(
            within(nav)
                .getByRole("link", { name: "Tasks" })
                .getAttribute("aria-current"),
        ).toBe("page");
        expect(
            within(nav)
                .getByRole("link", { name: "Clouds" })
                .getAttribute("href"),
        ).toBe("/administration/clouds/");
        mocks.request.mockImplementation(async (url: string) =>
            url.endsWith("/clouds") ? [cloud] : install,
        );
        await userEvent.click(
            within(nav).getByRole("link", { name: "Overview" }),
        );
        await waitFor(() =>
            expect(router.state.location.pathname).toBe("/administration/"),
        );
    },
);
