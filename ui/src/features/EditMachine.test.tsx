import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { EditMachine } from "./MachineForm";
const state = vi.hoisted(() => ({
    request: vi.fn(),
    vm: {
        node: "a",
        id: "demo",
        cloud: "local",
        zone: "z",
        instance_type: "small",
        managed: true,
        state: "stopped",
    },
}));
vi.mock("../api/hooks", async (original) => ({
    ...(await original<typeof import("../api/hooks")>()),
    useResource: (path: string) => ({
        data:
            path === "/api/v1/clouds"
                ? [
                      {
                          id: "local",
                          instance_types: [
                              { id: "small", name: "Small" },
                              { id: "large", name: "Large" },
                          ],
                      },
                  ]
                : state.vm,
        isError: false,
        isPending: false,
    }),
}));
vi.mock("../api/client", async (original) => ({
    ...(await original<typeof import("../api/client")>()),
    request: state.request,
}));
vi.mock("../api/tasks", () => ({
    useResourceBusy: () => ({ task: undefined, unavailable: false }),
    useResourceTasks: () => ({ data: { items: [] }, isError: false }),
}));
afterEach(() => {
    cleanup();
    state.request.mockReset();
    state.vm = { ...state.vm, state: "stopped", managed: true };
});
function show() {
    render(
        <QueryClientProvider client={new QueryClient()}>
            <RouterProvider
                router={createMemoryRouter(
                    [
                        {
                            path: "/machines/:node/:id/update/",
                            element: <EditMachine />,
                        },
                    ],
                    { initialEntries: ["/machines/a/demo/update/"] },
                )}
            />
        </QueryClientProvider>,
    );
}
it("submits an explicit type change and retains backend errors", async () => {
    state.request.mockRejectedValueOnce(
        new Error("Insufficient resources on host a"),
    );
    show();
    await userEvent.click(
        screen.getByRole("combobox", { name: "Instance type" }),
    );
    await userEvent.click(screen.getByRole("option", { name: "Large" }));
    await userEvent.click(
        screen.getByRole("button", { name: "Change instance type" }),
    );
    await userEvent.click(
        screen.getAllByRole("button", { name: "Change instance type" }).at(-1)!,
    );
    expect(
        await screen.findByText("Insufficient resources on host a"),
    ).toBeTruthy();
    expect(state.request).toHaveBeenCalledWith(
        "/api/v1/clouds/local/machines/a/demo/instance-type",
        "POST",
        { instance_type: "large" },
    );
    expect(screen.queryByRole("spinbutton", { name: "vCPUs" })).toBeNull();
});
it("requires a stopped machine", () => {
    state.vm.state = "running";
    show();
    expect(
        (
            screen.getByRole("button", {
                name: "Change instance type",
            }) as HTMLButtonElement
        ).disabled,
    ).toBe(true);
});
it("keeps unmanaged machines read-only", () => {
    state.vm.managed = false;
    show();
    expect(screen.getByText(/Adopt this machine/)).toBeTruthy();
    expect(
        screen.queryByRole("button", { name: "Change instance type" }),
    ).toBeNull();
});
