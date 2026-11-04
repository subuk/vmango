import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
    AdministrationInventory,
    AdministrationTasks,
} from "./AdministrationInventory";
import { TaskDetail } from "./Tasks";
const request = vi.hoisted(() => vi.fn());
vi.mock("../api/client", async (original) => ({
    ...(await original<typeof import("../api/client")>()),
    request,
}));
const vm = {
    id: "vm",
    name: "Existing machine",
    cloud: "local",
    zone: "a",
    namespace_id: "retired",
    state: "stopped",
    managed: true,
    node: "node",
    vcpus: 1,
    memory_bytes: 1024,
    arch: "x86_64",
    ip_addresses: [],
    disks: [],
    interfaces: [],
};
const task = {
    id: "saved",
    kind: "machine.create",
    state: "interrupted",
    namespace_id: "retired",
    cloud_id: "local",
    blocks_resources: true,
    outcome_uncertain: true,
    created_at: "2026-01-01T00:00:00Z",
    initiator: { id: "operator" },
    resources: [],
    stages: [],
    effects: [],
    transitions: [],
};
afterEach(() => {
    cleanup();
    request.mockReset();
});
function show(
    path: string,
    element: React.ReactNode,
    route = "/administration/clouds/:cloud/machines/:id/",
) {
    const router = createMemoryRouter([{ path: route, element }], {
        initialEntries: [path],
    });
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
}
it("shows assigned IDs normally after configuration removal without workload requests", async () => {
    request.mockImplementation(async (path) =>
        path === "/api/v1/namespaces" ? { items: [] } : vm,
    );
    show(
        "/administration/clouds/local/machines/vm/?namespace=unrelated",
        <AdministrationInventory kind="machines" />,
    );
    expect(await screen.findByText("retired")).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.queryByRole("combobox")).toBeNull();
    expect(
        request.mock.calls.some(([path]) => path.startsWith("/api/v1/clouds/")),
    ).toBe(false);
});
it("links an unassigned resource to an explicit destination without mutating Administration", async () => {
    request.mockImplementation(async (path) =>
        path === "/api/v1/namespaces"
            ? {
                  items: [
                      { id: "platform", name: "Platform", clouds: ["local"] },
                      { id: "other", name: "Other", clouds: ["elsewhere"] },
                  ],
              }
            : { ...vm, namespace_id: undefined },
    );
    show(
        "/administration/clouds/local/machines/vm/",
        <AdministrationInventory kind="machines" />,
    );
    await userEvent.click(
        await screen.findByRole("combobox", { name: "Destination namespace" }),
    );
    expect(screen.queryByRole("option", { name: "Other" })).toBeNull();
    await userEvent.click(screen.getByRole("option", { name: "Platform" }));
    expect(
        screen
            .getByRole("link", { name: "Open assignment preview" })
            .getAttribute("href"),
    ).toBe("/machines/local/vm/assign/?namespace=platform");
    expect(request.mock.calls.every(([, method]) => method === "GET")).toBe(
        true,
    );
});
it("keeps cross-namespace task inspection read-only including uncertain locks", async () => {
    request.mockResolvedValue(task);
    show(
        "/administration/tasks/saved/?namespace=unrelated",
        <TaskDetail administration />,
        "/administration/tasks/:id/",
    );
    await screen.findByText("retired");
    expect(
        screen.queryByRole("button", {
            name: /Cancel queued|Release resource/,
        }),
    ).toBeNull();
    expect(
        screen.getByRole("link", { name: "All tasks" }).getAttribute("href"),
    ).toBe("/administration/tasks/");
    expect(
        request.mock.calls.every(([path]) =>
            path.startsWith("/api/v1/administration/"),
        ),
    ).toBe(true);
});
it("shows namespace-free legacy history and paginates independently of workspace query", async () => {
    request.mockResolvedValue({
        items: [{ ...task, namespace_id: undefined }],
        active: 0,
        next: "cursor",
    });
    show(
        "/administration/tasks/?namespace=unrelated",
        <AdministrationTasks />,
        "/administration/tasks/",
    );
    await screen.findByText("Unassigned");
    await userEvent.click(screen.getByRole("button", { name: "Older tasks" }));
    await waitFor(() =>
        expect(
            request.mock.calls.some(([path]) => path.includes("before=cursor")),
        ).toBe(true),
    );
    expect(
        request.mock.calls.every(([path]) => !path.includes("namespace=")),
    ).toBe(true);
});
