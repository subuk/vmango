import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { EditMachine } from "./MachineForm";
import { APIError } from "../api/client";

const state = vi.hoisted(() => ({
    vm: {
        node: "node-a",
        id: "example",
        vcpus: 2,
        memory_bytes: 2048 * 1024 * 1024,
        graphics: "none",
        graphics_listen: "127.0.0.1",
        video_model: "none",
        autostart: false,
        guest_agent: false,
        hugepages: false,
    },
    request: vi.fn(),
}));
vi.mock("../api/hooks", async (original) => ({
    ...(await original<typeof import("../api/hooks")>()),
    useResource: () => ({ data: state.vm, isError: false, isPending: false }),
}));
vi.mock("../api/client", async (original) => ({
    ...(await original<typeof import("../api/client")>()),
    request: state.request,
}));
vi.mock("../api/tasks", async (original) => ({
    ...(await original<typeof import("../api/tasks")>()),
    useResourceBusy: () => ({ task: undefined, unavailable: false }),
    useResourceTasks: () => ({
        data: { items: [], active: 0 },
        isError: false,
    }),
}));
afterEach(cleanup);
it("preserves edits across resource refreshes, patches only user changes and focuses server field errors", async () => {
    const router = createMemoryRouter(
        [{ path: "/machines/:node/:id/update/", element: <EditMachine /> }],
        { initialEntries: ["/machines/node-a/example/update/"] },
    );
    const queries = new QueryClient();
    const tree = () => (
        <QueryClientProvider client={queries}>
            <RouterProvider router={router} />
        </QueryClientProvider>
    );
    const view = render(tree());
    const user = userEvent.setup();
    await user.clear(screen.getByRole("spinbutton", { name: "vCPUs" }));
    await user.type(screen.getByRole("spinbutton", { name: "vCPUs" }), "4");
    state.vm = { ...state.vm, memory_bytes: 4096 * 1024 * 1024 };
    view.rerender(tree());
    expect(
        (
            screen.getByRole("spinbutton", {
                name: "Memory (MiB)",
            }) as HTMLInputElement
        ).value,
    ).toBe("2048");
    state.request.mockRejectedValueOnce(
        new APIError(422, {
            detail: "Invalid CPU allocation",
            errors: [
                {
                    location: "body.vcpus",
                    message: "CPU allocation is unavailable",
                },
            ],
        }),
    );
    await user.click(screen.getByRole("button", { name: "Save changes" }));
    await waitFor(() =>
        expect(state.request).toHaveBeenCalledWith(
            "/api/v1/nodes/node-a/vms/example",
            "PATCH",
            { vcpus: 4 },
        ),
    );
    await waitFor(() =>
        expect(document.activeElement).toBe(
            screen.getByRole("spinbutton", { name: "vCPUs" }),
        ),
    );
    expect(
        screen
            .getByRole("spinbutton", { name: "vCPUs" })
            .getAttribute("aria-invalid"),
    ).toBe("true");
    expect(
        (screen.getByRole("spinbutton", { name: "vCPUs" }) as HTMLInputElement)
            .value,
    ).toBe("4");
});
