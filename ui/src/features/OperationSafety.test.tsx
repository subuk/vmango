import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MachineActions } from "./Lists";
import { Console } from "../consoles/Console";
import type { VM } from "../api/hooks";

const request = vi.hoisted(() => vi.fn().mockResolvedValue(undefined));
vi.mock("../api/client", async (original) => ({
    ...(await original<typeof import("../api/client")>()),
    request,
}));
vi.mock("../api/hooks", async (original) => ({
    ...(await original<typeof import("../api/hooks")>()),
    useResource: () => ({ data: { graphics: "vnc" } }),
}));
afterEach(() => {
    cleanup();
    request.mockClear();
});
function show(element: React.ReactNode, path = "/machines/node-a/example/") {
    const router = createMemoryRouter(
        [{ path: "/machines/:node/:id/*", element }],
        { initialEntries: [path] },
    );
    render(
        <QueryClientProvider client={new QueryClient()}>
            <RouterProvider router={router} />
        </QueryClientProvider>,
    );
}
it.each([false, true])(
    "keeps attached disk deletion opt-in (%s)",
    async (deleteDisks) => {
        show(
            <MachineActions
                vm={{ id: "example", node: "node-a", state: "stopped" } as VM}
            />,
        );
        await userEvent.click(
            screen.getByRole("button", { name: "More actions for example" }),
        );
        await userEvent.click(
            screen.getByRole("menuitem", { name: "Delete…" }),
        );
        const choice = screen.getByRole("checkbox", {
            name: "Also delete attached disks (permanent)",
        }) as HTMLInputElement;
        expect(choice.checked).toBe(false);
        expect(request).not.toHaveBeenCalled();
        expect(
            screen.getByText(/Permanently remove node-a\/example/),
        ).toBeTruthy();
        if (deleteDisks) await userEvent.click(choice);
        await userEvent.click(
            screen.getByRole("button", { name: "Delete machine" }),
        );
        await waitFor(() =>
            expect(request).toHaveBeenCalledWith(
                `/api/v1/nodes/node-a/vms/example?delete_volumes=${deleteDisks}`,
                "DELETE",
            ),
        );
    },
);
it("requires explicit serial takeover and permits cancelling without issuing a link", async () => {
    show(<Console />, "/machines/node-a/example/console/");
    await userEvent.click(
        screen.getByRole("button", { name: "Force takeover…" }),
    );
    expect(
        screen.getByRole("dialog", {
            name: "Take over example serial console?",
        }),
    ).toBeTruthy();
    expect(
        screen.getByText(
            /Disconnect any existing serial client for node-a\/example/,
        ),
    ).toBeTruthy();
    expect(request).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(request).not.toHaveBeenCalled();
});
