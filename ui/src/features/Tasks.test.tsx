import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { TaskDetail, Tasks, TaskMonitor } from "./Tasks";
import { APIError } from "../api/client";
import type { Task } from "../api/tasks";

const request = vi.hoisted(() => vi.fn());
vi.mock("../api/client", async (original) => ({
    ...(await original<typeof import("../api/client")>()),
    request,
}));
afterEach(() => {
    cleanup();
    request.mockReset();
    localStorage.clear();
});
const base: Task = {
    id: "persisted-task",
    kind: "machine.create",
    state: "running",
    created_at: "2026-10-30T10:00:00Z",
    started_at: "2026-10-30T10:00:01Z",
    initiator: { id: "admin" },
    resources: [{ kind: "machine", scope: "local", id: "pending-vm" }],
    effects: [
        {
            resource: { kind: "volume", scope: "local", id: "/pool/root" },
            action: "created",
            at: "2026-10-30T10:00:02Z",
        },
    ],
    stages: [
        {
            name: "clone-volume",
            state: "succeeded",
            started_at: "2026-10-30T10:00:01Z",
            finished_at: "2026-10-30T10:00:02Z",
        },
        {
            name: "define-vm",
            state: "running",
            started_at: "2026-10-30T10:00:03Z",
        },
    ],
    transitions: [],
    outcome_uncertain: false,
    blocks_resources: true,
};
function show(path = "/tasks/persisted-task/") {
    const queries = new QueryClient({
        defaultOptions: { queries: { retry: false } },
    });
    const router = createMemoryRouter(
        [
            { path: "/tasks/", element: <Tasks /> },
            { path: "/tasks/:id/", element: <TaskDetail /> },
        ],
        { initialEntries: [path] },
    );
    const view = render(
        <QueryClientProvider client={queries}>
            <RouterProvider router={router} />
        </QueryClientProvider>,
    );
    return { ...view, queries, router };
}
it("restores persisted running progress after navigation/reload and retains it during a polling failure", async () => {
    request.mockResolvedValue(base);
    const first = show();
    await screen.findByText(
        "Operation in progress. You can leave this page and return later.",
    );
    expect(screen.queryByRole("link", { name: "Open machine" })).toBeNull();
    expect(screen.getByText("local//pool/root")).toBeTruthy();
    first.unmount();
    const second = show();
    await screen.findByRole("table", { name: "Execution stages" });
    request.mockRejectedValue(
        new APIError(503, { detail: "Temporary network failure" }),
    );
    await second.queries.invalidateQueries();
    await screen.findByText(/Refresh failed. Showing last-known data/);
    expect(
        screen.getByText(
            "Operation in progress. You can leave this page and return later.",
        ),
    ).toBeTruthy();
    request.mockResolvedValue({
        ...base,
        state: "succeeded",
        blocks_resources: false,
        finished_at: "2026-10-30T10:00:05Z",
    });
    await second.queries.invalidateQueries();
    await screen.findByText(
        "Command completed. Resource state is refreshed from the backend.",
    );
});
it("shows interrupted uncertainty, last stage and persisted backend diagnostics without replay", async () => {
    request.mockResolvedValue({
        ...base,
        state: "interrupted",
        outcome_uncertain: true,
        error: {
            message: "Process stopped; actual backend outcome unknown",
            operation: "process interruption",
        },
    });
    show();
    await screen.findByText(/Execution was interrupted/);
    expect(
        screen.getByText("In progress when interrupted; outcome unknown"),
    ).toBeTruthy();
    expect(
        screen.getByRole("button", { name: "Release resource locks…" }),
    ).toBeTruthy();
    expect(
        request.mock.calls.every(
            ([, method]) => method === undefined || method === "GET",
        ),
    ).toBe(true);
});
it("loads filtered paginated history and reopens useful backend failure details", async () => {
    const failed = {
        ...base,
        state: "failed",
        outcome_uncertain: true,
        error: {
            backend: "libvirt",
            message: "QEMU: disk image is locked",
            code: "38",
            domain: "10",
            operation: "virDomainCreate",
        },
    };
    request.mockImplementation(async (path: string) =>
        path.startsWith("/api/v1/tasks?")
            ? {
                  items: [failed],
                  next: path.includes("before=") ? undefined : "cursor-next",
                  active: 0,
              }
            : failed,
    );
    const user = userEvent.setup();
    show(
        "/tasks/?resource_kind=machine&resource_scope=local&resource_id=pending-vm&state=failed",
    );
    await screen.findByRole("table", { name: "Task history" });
    await user.click(screen.getByRole("button", { name: "Older tasks" }));
    await waitFor(() =>
        expect(
            request.mock.calls.some(
                ([path]) =>
                    path.includes("before=cursor-next") &&
                    path.includes("state=failed") &&
                    path.includes("resource_id=pending-vm"),
            ),
        ).toBe(true),
    );
    await user.click(screen.getByRole("link", { name: "Create machine" }));
    await screen.findByText("QEMU: disk image is locked");
    expect(screen.getByText(/Code 38/)).toBeTruthy();
    expect(screen.getByText(/Domain 10/)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Copy task ID" })).toBeTruthy();
    expect(screen.getByText("local//pool/root")).toBeTruthy();
});

it("preserves typed resource filters until explicitly applied", async () => {
    request.mockResolvedValue({ items: [], active: 0 });
    const user = userEvent.setup();
    const view = show("/tasks/");
    await screen.findByRole("table", { name: "Task history" });
    await user.type(
        screen.getByRole("textbox", { name: "Resource scope" }),
        "local",
    );
    await user.type(
        screen.getByRole("textbox", { name: "Resource identifier" }),
        "pending-vm",
    );
    expect(
        (
            screen.getByRole("textbox", {
                name: "Resource scope",
            }) as HTMLInputElement
        ).value,
    ).toBe("local");
    expect(
        (
            screen.getByRole("textbox", {
                name: "Resource identifier",
            }) as HTMLInputElement
        ).value,
    ).toBe("pending-vm");
    expect(view.router.state.location.search).toBe("");
    await user.click(screen.getByRole("button", { name: "Apply" }));
    await waitFor(() =>
        expect(
            new URLSearchParams(view.router.state.location.search).get(
                "resource_id",
            ),
        ).toBe("pending-vm"),
    );
    expect(
        new URLSearchParams(view.router.state.location.search).get(
            "resource_scope",
        ),
    ).toBe("local");
});

it("shows overlapping image transfer progress with speed and remaining time after reload", async () => {
    request.mockResolvedValue({
        ...base,
        stages: [
            {
                name: "stream-image",
                state: "running",
                started_at: new Date().toISOString(),
                transfer: {
                    bytes: 524288,
                    total_bytes: 1048576,
                    bytes_per_second: 65536,
                    remaining_seconds: 8,
                    updated_at: new Date().toISOString(),
                },
            },
        ],
    });
    show();
    await screen.findByText("Download and upload image");
    expect(
        screen
            .getByRole("progressbar", { name: "Image transfer" })
            .getAttribute("aria-valuenow"),
    ).toBe("50");
    expect(screen.getByText(/50.0%/)).toBeTruthy();
    expect(
        screen.getByText(/64 KiB\/s.*8s.*remaining in transfer/),
    ).toBeTruthy();
});

it("does not turn placement requests and cloud capacity into volume links", async () => {
    request.mockResolvedValue({
        ...base,
        resources: [
            { kind: "machine-request", scope: "local", id: "new-machine" },
            { kind: "cloud", scope: "local", id: "capacity" },
        ],
    });
    show();
    expect(
        await screen.findByText("Requested machine: new-machine"),
    ).toBeTruthy();
    expect(
        screen.queryByRole("link", { name: "local/new-machine" }),
    ).toBeNull();
    expect(
        screen.getByRole("link", { name: "Cloud local" }).getAttribute("href"),
    ).toBe("/machines/?cloud=local");
    expect(screen.queryByRole("link", { name: "local/capacity" })).toBeNull();
});

it("hides released failures from the global monitor", async () => {
    const released = {
        ...base,
        state: "failed",
        blocks_resources: false,
        lock_release: {
            by: { id: "admin" },
            at: base.created_at,
            reason: "Inspected",
        },
    };
    request.mockResolvedValue({ items: [released], active: 0 });
    render(
        <QueryClientProvider client={new QueryClient()}>
            <RouterProvider
                router={createMemoryRouter([
                    { path: "/", element: <TaskMonitor /> },
                ])}
            />
        </QueryClientProvider>,
    );
    await screen.findByText("0 active");
    expect(screen.queryByRole("alert")).toBeNull();
});

it("keeps a dismissed failure hidden after remounting the monitor", async () => {
    request.mockImplementation(async (path: string) =>
        path.includes("state=")
            ? { items: [], active: 0 }
            : { items: [{ ...base, state: "failed" }], active: 0 },
    );
    function monitor() {
        return render(
            <QueryClientProvider client={new QueryClient()}>
                <RouterProvider
                    router={createMemoryRouter([
                        { path: "/", element: <TaskMonitor /> },
                    ])}
                />
            </QueryClientProvider>,
        );
    }
    const first = monitor();
    await screen.findByRole("alert");
    await userEvent
        .setup()
        .click(screen.getByRole("button", { name: "Close" }));
    expect(screen.queryByRole("alert")).toBeNull();
    first.unmount();
    monitor();
    await screen.findByText("0 active");
    expect(screen.queryByRole("alert")).toBeNull();
});
