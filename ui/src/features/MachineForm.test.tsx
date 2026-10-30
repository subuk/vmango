import { afterEach, expect, it, vi } from "vitest";
import {
    cleanup,
    fireEvent,
    render,
    screen,
    waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { CreateMachine } from "./MachineForm";
import { APIError } from "../api/client";

const request = vi.hoisted(() => vi.fn());
vi.mock("../api/client", async (original) => ({
    ...(await original<typeof import("../api/client")>()),
    request,
}));

vi.mock("../api/hooks", async (original) => ({
    ...(await original<typeof import("../api/hooks")>()),
    useNodes: () => ({ data: [{ id: "n1" }, { id: "n2" }], isError: false }),
    useResource: (path: string) => {
        let data: unknown = [];
        if (path.includes("/images") && path.includes("/n1/"))
            data = [
                {
                    id: "ubuntu",
                    name: "Ubuntu",
                    os_name: "Ubuntu",
                    os_version: "24.04",
                    source_mode: "download",
                    state: "downloadable",
                    arch: "aarch64",
                    minimum_memory_bytes: 1024 ** 3,
                    minimum_disk_bytes: 8 * 1024 ** 3,
                    defaults: {},
                },
            ];
        if (path.includes("/capabilities"))
            data = {
                max_vcpus: 8,
                disk_buses: ["virtio"],
                firmwares: ["auto", "efi"],
                disk_controllers: [],
                nic_models: ["virtio"],
                network_models: { "special-mode": ["virtio"] },
                graphics: ["vnc", "none"],
                video_models: ["none"],
            };
        if (path.includes("/pools"))
            data = [
                {
                    name: path.includes("/n1/") ? "pool1" : "pool2",
                    free_bytes: 10 * 1024 ** 3,
                },
            ];
        if (path.includes("/networks"))
            data = [{ name: path.includes("/n1/") ? "net1" : "net2" }];
        return { data, isError: false, isPending: false, refetch: vi.fn() };
    },
}));
afterEach(() => {
    cleanup();
    request.mockReset();
});
it("preserves entered values and flags an incompatible image after changing node", async () => {
    const router = createMemoryRouter(
        [{ path: "*", element: <CreateMachine /> }],
        { initialEntries: ["/machines/add/?node=n1"] },
    );
    render(
        <QueryClientProvider client={new QueryClient()}>
            <RouterProvider router={router} />
        </QueryClientProvider>,
    );
    const user = userEvent.setup();
    await user.type(
        screen.getByRole("textbox", { name: "Machine name" }),
        "preserved-vm",
    );
    await user.click(
        screen.getByRole("textbox", { name: "Cloud-init user-data" }),
    );
    await user.paste("#cloud-config\npackages: [curl]");
    await user.click(screen.getByRole("combobox", { name: "Image" }));
    await user.click(
        screen.getByRole("option", {
            name: /Ubuntu.*24.04.*Download on first use/,
        }),
    );
    expect(
        screen.getByText(/downloaded and verified automatically/),
    ).toBeTruthy();
    await user.click(screen.getByRole("combobox", { name: "Node" }));
    await user.click(screen.getByRole("option", { name: "n2" }));
    expect(
        (
            screen.getByRole("textbox", {
                name: "Machine name",
            }) as HTMLInputElement
        ).value,
    ).toBe("preserved-vm");
    expect(
        (
            screen.getByRole("textbox", {
                name: "Cloud-init user-data",
            }) as HTMLInputElement
        ).value,
    ).toContain("packages: [curl]");
    expect(screen.getByText(/This image is unavailable/)).toBeTruthy();
    expect(
        screen.getByRole("combobox", { name: "Image" }).textContent,
    ).toContain("ubuntu (unavailable)");
    await user.click(screen.getByRole("combobox", { name: "Network" }));
    expect(screen.getByRole("option", { name: "special-mode" })).toBeTruthy();
});

it("keeps an uncertain submission and draft visible without submitting twice", async () => {
    const router = createMemoryRouter(
        [{ path: "*", element: <CreateMachine /> }],
        { initialEntries: ["/machines/add/?node=n1"] },
    );
    render(
        <QueryClientProvider client={new QueryClient()}>
            <RouterProvider router={router} />
        </QueryClientProvider>,
    );
    const user = userEvent.setup();
    await user.type(
        screen.getByRole("textbox", { name: "Machine name" }),
        "partial-vm",
    );
    for (const [label, option] of [
        ["Image", /Ubuntu.*24.04/],
        ["Storage pool", "pool1"],
        ["Network", "net1"],
    ] as const) {
        await user.click(screen.getByRole("combobox", { name: label }));
        await user.click(screen.getByRole("option", { name: option }));
    }
    let reject: (error: Error) => void = () => {};
    request.mockImplementationOnce(
        () =>
            new Promise((_, fail) => {
                reject = fail;
            }),
    );
    await user.click(screen.getByRole("button", { name: "Create machine" }));
    expect(
        screen
            .getByRole("button", { name: "Submitting…" })
            .hasAttribute("disabled"),
    ).toBe(true);
    fireEvent.submit(
        screen.getByRole("textbox", { name: "Machine name" }).closest("form")!,
    );
    expect(request).toHaveBeenCalledTimes(1);
    reject(new APIError(0, { detail: "Submission status is unknown" }));
    await waitFor(() =>
        expect(screen.getByText("Submission status is unknown")).toBeTruthy(),
    );
    expect(
        (
            screen.getByRole("textbox", {
                name: "Machine name",
            }) as HTMLInputElement
        ).value,
    ).toBe("partial-vm");
    expect(
        screen
            .getByRole("button", { name: "Create machine" })
            .hasAttribute("disabled"),
    ).toBe(true);
    expect(request).toHaveBeenCalledTimes(1);
});

it("opens accepted creation progress before the machine exists", async () => {
    const router = createMemoryRouter(
        [
            { path: "/machines/add/", element: <CreateMachine /> },
            { path: "/tasks/:id/", element: <p>Accepted task progress</p> },
        ],
        { initialEntries: ["/machines/add/?node=n1"] },
    );
    render(
        <QueryClientProvider client={new QueryClient()}>
            <RouterProvider router={router} />
        </QueryClientProvider>,
    );
    const user = userEvent.setup();
    await user.type(
        screen.getByRole("textbox", { name: "Machine name" }),
        "pending-vm",
    );
    for (const [label, option] of [
        ["Image", /Ubuntu.*24.04/],
        ["Storage pool", "pool1"],
        ["Network", "net1"],
    ] as const) {
        await user.click(screen.getByRole("combobox", { name: label }));
        await user.click(screen.getByRole("option", { name: option }));
    }
    request.mockResolvedValueOnce({
        task: { id: "accepted-creation", state: "queued" },
        status_url: "/api/v1/tasks/accepted-creation",
    });
    await user.click(screen.getByRole("button", { name: "Create machine" }));
    await screen.findByText("Accepted task progress");
    expect(router.state.location.pathname).toBe("/tasks/accepted-creation/");
    expect(request).toHaveBeenCalledTimes(1);
    expect(screen.queryByText(/machine created/i)).toBeNull();
});
