import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Machines } from "./Lists";

const state = vi.hoisted(() => ({ empty: false }));
vi.mock("../api/hooks", async (original) => ({
    ...(await original<typeof import("../api/hooks")>()),
    useResource: () => ({
        data: [{ id: "cloud", name: "Cloud", unavailable_hosts: [] }],
        isSuccess: true,
    }),
    useMachines: () => ({
        isPending: false,
        isError: false,
        data: {
            unavailable_hosts: ["offline"],
            items: state.empty
                ? []
                : ["uuid-a", "uuid-b"].map((id, n) => ({
                      id,
                      name: "same-name",
                      cloud: "cloud",
                      node: `host-${n}`,
                      state: "stopped",
                      managed: true,
                      ip_addresses: [],
                      vcpus: 2,
                      memory_bytes: 2 ** 30,
                  })),
        },
    }),
}));
vi.mock("../api/tasks", () => ({
    useResourceBusy: () => ({ unavailable: false }),
    useResourceTasks: () => ({ data: { items: [] }, isError: false }),
}));
afterEach(() => {
    cleanup();
    state.empty = false;
});
function show() {
    render(
        <QueryClientProvider client={new QueryClient()}>
            <MemoryRouter>
                <Machines />
            </MemoryRouter>
        </QueryClientProvider>,
    );
}
it("links same-named machines by cloud and ID and reports incomplete inventory", () => {
    show();
    expect(
        screen
            .getAllByRole("link", { name: "same-name" })
            .map((a) => a.getAttribute("href")),
    ).toEqual(["/machines/cloud/uuid-a/", "/machines/cloud/uuid-b/"]);
    expect(screen.getByText(/The list is incomplete.*offline/)).toBeTruthy();
});
it("does not describe an unavailable inventory as a new empty cloud", () => {
    state.empty = true;
    show();
    expect(
        screen.getByText("No machines returned. Some hosts are unavailable."),
    ).toBeTruthy();
    expect(screen.queryByText(/Create your first machine/)).toBeNull();
});
