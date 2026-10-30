import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { TaskDetail, Tasks } from "./Tasks";
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
    expect(request.mock.calls.every(([, method]) => method === undefined)).toBe(
        true,
    );
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
    await user.click(
        screen.getByRole("button", { name: "Apply resource filters" }),
    );
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
