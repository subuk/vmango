import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryRouter, RouterProvider } from "react-router";
import {
    AdministrationResourceTasks,
    AdministrationTasks,
} from "./AdministrationOperations";
import { TaskDetail } from "./Tasks";
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
const task = {
    id: "task-one",
    kind: "machine.create",
    state: "interrupted",
    namespace_id: "ops",
    cloud_id: "west",
    blocks_resources: true,
    outcome_uncertain: true,
    created_at: "2026-11-01T00:00:00Z",
    initiator: { id: "operator" },
    resources: [{ kind: "machine", scope: "host", id: "vm-one" }],
    stages: [],
    effects: [],
    transitions: [],
};
function base() {
    mocks.request.mockImplementation(async (path: string) =>
        path === "/api/v1/administration/configuration"
            ? { snapshot_id: "accepted" }
            : path === "/api/v1/administration/clouds"
              ? [{ id: "west", name: "West" }]
              : path.endsWith("/configuration/namespaces")
                ? { items: [{ id: "ops", name: "Operations" }] }
                : path.includes("/tasks?")
                  ? { items: [task], next: "older" }
                  : path.endsWith("/tasks/task-one")
                    ? task
                    : Promise.reject(new Error(path)),
    );
}
function setup(
    path = "/administration/tasks/",
    element: React.ReactNode = <AdministrationTasks />,
) {
    const queries = new QueryClient({
        defaultOptions: { queries: { retry: false } },
    });
    const router = createMemoryRouter(
        [
            { path: "/administration/tasks/", element },
            {
                path: "/administration/tasks/:id/",
                element: <TaskDetail administration />,
            },
        ],
        { initialEntries: [path] },
    );
    render(
        <QueryClientProvider client={queries}>
            <RouterProvider router={router} />
        </QueryClientProvider>,
    );
    return { queries, router };
}
afterEach(() => {
    cleanup();
    mocks.request.mockReset();
    mocks.identity = { id: "one" };
});
it("keeps API scope through pagination and detail return without treating workload namespace as a filter", async () => {
    base();
    const query =
        "namespace=workload&cloud_id=west&namespace_id=ops&host=host&resource_kind=machine&resource_scope=host&resource_id=vm-one&state=interrupted&blocking=true";
    const { router, queries } = setup(`/administration/tasks/?${query}`);
    await screen.findByRole("link", { name: "Create machine" });
    const first = mocks.request.mock.calls.find(([p]) =>
        p.includes("/tasks?"),
    )![0];
    const api = new URLSearchParams(first.split("?")[1]);
    expect(api.has("namespace")).toBe(false);
    expect(Object.fromEntries(api)).toEqual({
        limit: "25",
        state: "interrupted",
        cloud_id: "west",
        namespace_id: "ops",
        host: "host",
        resource_kind: "machine",
        resource_scope: "host",
        resource_id: "vm-one",
        blocking: "true",
    });
    await userEvent.click(screen.getByRole("button", { name: "Older tasks" }));
    await waitFor(() =>
        expect(
            mocks.request.mock.calls.some(([p]) => p.includes("before=older")),
        ).toBe(true),
    );
    const older = router.state.location.search;
    expect(older).toBe(`?${query}&before=older`);
    const page = queries
        .getQueryCache()
        .getAll()
        .find((q) => String(q.queryKey[2]).includes("before=older"));
    expect(
        (page?.options as { refetchInterval: unknown }).refetchInterval,
    ).toBe(false);
    await userEvent.click(
        await screen.findByRole("link", { name: "Create machine" }),
    );
    expect(
        (await screen.findByRole("link", { name: "All tasks" })).getAttribute(
            "href",
        ),
    ).toBe(`/administration/tasks/${older}`);
    expect(
        screen.queryByRole("button", {
            name: /Cancel queued|Release resource/,
        }),
    ).toBeNull();
    await userEvent.click(screen.getByRole("link", { name: "All tasks" }));
    await userEvent.click(
        await screen.findByRole("button", { name: "Newest tasks" }),
    );
    expect(router.state.location.search).toBe(`?${query}`);
});
it("keeps page search separate from server filters and explicitly supports Unassigned", async () => {
    base();
    const { router } = setup(
        "/administration/tasks/?namespace=workload&namespace_id=ops&before=older&state=interrupted",
    );
    await screen.findByRole("link", { name: "Create machine" });
    await userEvent.type(
        screen.getByRole("textbox", { name: "Search" }),
        "missing",
    );
    expect(
        new URLSearchParams(router.state.location.search).get("page_q"),
    ).toBe("missing");
    await userEvent.click(screen.getByRole("combobox", { name: "Namespace" }));
    await userEvent.click(screen.getByRole("option", { name: "Unassigned" }));
    const params = new URLSearchParams(router.state.location.search);
    expect(params.get("unassigned")).toBe("true");
    expect(params.get("namespace")).toBe("workload");
    expect(params.has("namespace_id")).toBe(false);
    expect(params.has("before")).toBe(false);
    await waitFor(() =>
        expect(
            mocks.request.mock.calls.some(([p]) =>
                p.includes("unassigned=true"),
            ),
        ).toBe(true),
    );
    await userEvent.click(
        screen.getByRole("button", { name: "Reset filters" }),
    );
    expect(router.state.location.search).toBe("?namespace=workload");
});
it("requests and polls only the selected resource scope and preserves it in task links", async () => {
    base();
    const { queries } = setup(
        "/administration/tasks/",
        <AdministrationResourceTasks
            cloud="west"
            resource={{ kind: "machine", scope: "host", id: "vm-one" }}
        />,
    );
    const link = await screen.findByRole("link", { name: "Create machine" });
    expect(mocks.request).toHaveBeenCalledTimes(1);
    expect(mocks.request.mock.calls[0][0]).toBe(
        "/api/v1/administration/tasks?limit=5&cloud_id=west&resource_kind=machine&resource_scope=host&resource_id=vm-one",
    );
    expect(link.getAttribute("href")).toBe(
        "/administration/tasks/task-one/?limit=5&cloud_id=west&resource_kind=machine&resource_scope=host&resource_id=vm-one",
    );
    expect(
        (
            queries.getQueryCache().getAll()[0].options as {
                refetchInterval: unknown;
            }
        ).refetchInterval,
    ).toBe(5000);
});
it("does not request global history when no resource has been selected", () => {
    base();
    setup(
        "/administration/tasks/",
        <AdministrationResourceTasks cloud="west" />,
    );
    expect(mocks.request).not.toHaveBeenCalled();
    expect(screen.queryByText("Recent operations")).toBeNull();
});
it("does not show another principal's task history while loading", async () => {
    base();
    const { queries } = setup();
    await screen.findByRole("link", { name: "Create machine" });
    mocks.request.mockImplementation(() => new Promise(() => {}));
    act(() => {
        mocks.identity = { id: "two" };
        mocks.listeners.forEach((fn) => fn());
    });
    expect(screen.queryByRole("link", { name: "Create machine" })).toBeNull();
    await waitFor(() =>
        expect(
            queries
                .getQueryCache()
                .getAll()
                .some(
                    (q) =>
                        q.queryKey[1] === "two" &&
                        String(q.queryKey[2]).includes("/tasks?"),
                ),
        ).toBe(true),
    );
});
