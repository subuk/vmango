import { createContext, useContext } from "react";
import { afterEach, expect, it, vi } from "vitest";
import {
    cleanup,
    fireEvent,
    render,
    screen,
    within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { EditMachine } from "./MachineForm";
const state = vi.hoisted(() => ({
    request: vi.fn(),
    types: [
        { id: "small", name: "Small" },
        { id: "large", name: "Large" },
    ] as { id: string; name: string }[] | undefined,
    unavailable: false,
    busy: false,
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
    useMachine: () => ({ data: state.vm, isError: false, isPending: false }),
    useResource: (path: string) => ({
        data:
            path === "/api/v1/clouds"
                ? state.types && [{ id: "local", instance_types: state.types }]
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
    useResourceBusy: () => ({
        task: state.busy ? { id: "task" } : undefined,
        unavailable: state.unavailable,
    }),
    useResourceTasks: () => ({ data: { items: [] }, isError: false }),
}));
afterEach(() => {
    cleanup();
    state.request.mockReset();
    state.vm = {
        ...state.vm,
        instance_type: "small",
        state: "stopped",
        managed: true,
    };
    state.types = [
        { id: "small", name: "Small" },
        { id: "large", name: "Large" },
    ];
    state.busy = false;
    state.unavailable = false;
});
const RefreshContext = createContext(0);
function RefreshEditMachine() {
    useContext(RefreshContext);
    return <EditMachine />;
}
function show() {
    const client = new QueryClient();
    const router = createMemoryRouter(
        [
            {
                path: "/machines/:node/:id/update/",
                element: <RefreshEditMachine />,
            },
        ],
        { initialEntries: ["/machines/a/demo/update/"] },
    );
    let revision = 0;
    const tree = () => (
        <RefreshContext.Provider value={revision}>
            <QueryClientProvider client={client}>
                <RouterProvider router={router} />
            </QueryClientProvider>
        </RefreshContext.Provider>
    );
    const view = render(tree());
    return {
        refresh: () => {
            revision++;
            view.rerender(tree());
        },
    };
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

function changeButton() {
    return screen.getByRole("button", {
        name: "Change instance type",
    }) as HTMLButtonElement;
}
async function submitType(expected: string) {
    state.request.mockRejectedValueOnce(new Error("Fixture response"));
    await userEvent.click(changeButton());
    await userEvent.click(
        screen.getAllByRole("button", { name: "Change instance type" }).at(-1)!,
    );
    expect(state.request).toHaveBeenCalledWith(
        "/api/v1/clouds/local/machines/a/demo/instance-type",
        "POST",
        { instance_type: expected },
    );
}
it.each(["", "removed"])(
    "selects and submits the sole catalog type for applied ID %s",
    async (applied) => {
        state.vm.instance_type = applied;
        state.types = [{ id: "large", name: "Large" }];
        show();
        expect(screen.getByText("Instance type: Large")).toBeTruthy();
        expect(screen.queryByRole("combobox")).toBeNull();
        expect(changeButton().disabled).toBe(false);
        await submitType("large");
    },
);
it.each(["", "removed"])(
    "normalizes applied ID %s after asynchronous catalog loading",
    async (applied) => {
        state.vm.instance_type = applied;
        state.types = undefined;
        const view = show();
        expect(changeButton().disabled).toBe(true);
        state.types = [{ id: "large", name: "Large" }];
        view.refresh();
        expect(screen.getByText("Instance type: Large")).toBeTruthy();
        await submitType("large");
    },
);
it("updates removed selections and disables submission when no types remain", async () => {
    const view = show();
    state.types = [{ id: "large", name: "Large" }];
    view.refresh();
    expect(screen.getByText("Instance type: Large")).toBeTruthy();
    state.types = [];
    view.refresh();
    expect(changeButton().disabled).toBe(true);
    expect(state.request).not.toHaveBeenCalled();
});
it.each(["busy", "unavailable"] as const)(
    "keeps %s task guards with a sole type",
    (guard) => {
        state[guard] = true;
        state.vm.instance_type = "";
        state.types = [{ id: "small", name: "Small" }];
        show();
        expect(changeButton().disabled).toBe(true);
    },
);

it("preserves a valid user selection when the catalog changes", async () => {
    const view = show();
    await userEvent.click(
        screen.getByRole("combobox", { name: "Instance type" }),
    );
    await userEvent.click(screen.getByRole("option", { name: "Large" }));
    state.types = [
        { id: "new", name: "New" },
        { id: "large", name: "Large" },
    ];
    view.refresh();
    expect(
        screen.getByRole("combobox", { name: "Instance type" }).textContent,
    ).toBe("Large");
    await submitType("large");
});

it.each(["catalog", "running", "unmanaged", "busy", "unavailable"])(
    "blocks an open confirmation after %s changes",
    async (change) => {
        const view = show();
        await userEvent.click(changeButton());
        if (change === "catalog") state.types = [];
        else if (change === "running") state.vm.state = "running";
        else if (change === "unmanaged") state.vm.managed = false;
        else if (change === "busy") state.busy = true;
        else state.unavailable = true;
        view.refresh();
        const submit = within(screen.getByRole("dialog")).getByRole("button", {
            name: "Change instance type",
        }) as HTMLButtonElement;
        expect(submit.disabled).toBe(true);
        fireEvent.submit(submit.closest("form")!);
        expect(state.request).not.toHaveBeenCalled();
    },
);
