import { afterEach, expect, it, vi } from "vitest";
import {
    cleanup,
    render,
    screen,
    waitFor,
    within,
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
const catalog = vi.hoisted(() => ({
    id: "local",
    name: "Local cloud",
    zones: [{ id: "z", name: "Zone" }],
    images: [{ id: "ubuntu", name: "Ubuntu" }] as {
        id: string;
        name: string;
        data_disks?: { name: string; size_bytes: number }[];
    }[],
    instance_types: [
        {
            id: "small",
            name: "Small",
            vcpus: 2,
            memory_bytes: 2 ** 31,
            boot_disk_bytes: 20 * 2 ** 30,
        },
        {
            id: "large",
            name: "Large",
            vcpus: 4,
            memory_bytes: 4 * 2 ** 30,
            boot_disk_bytes: 40 * 2 ** 30,
        },
    ],
    networks: [{ id: "lan", name: "LAN" }],
    storage_classes: [{ id: "fast", name: "Fast" }],
    placement: {
        attributes: ["rack"],
        modes: ["required", "preferred"],
        operators: ["equals"],
    },
    offers: [
        {
            zone: "z",
            image: "ubuntu",
            instance_type: "small",
            networks: ["lan"],
            storage_classes: ["fast"],
        },
    ],
}));
vi.mock("../api/hooks", async (original) => ({
    ...(await original<typeof import("../api/hooks")>()),
    useResource: (path: string) => ({
        data:
            path === "/api/v1/clouds"
                ? [catalog]
                : path.includes("/volumes")
                  ? {
                        items: [
                            {
                                id: "one",
                                node: "a",
                                name: "Disk A",
                                cloud: "local",
                                zone: "z",
                                size_bytes: 2 ** 30,
                            },
                            {
                                id: "two",
                                node: "b",
                                name: "Disk B",
                                cloud: "local",
                                zone: "z",
                                size_bytes: 2 ** 30,
                            },
                        ],
                        unavailable_hosts: [],
                    }
                  : [],
        isError: false,
        isPending: false,
        error: null,
    }),
}));
vi.mock("../api/tasks", () => ({
    useResourceTasks: () => ({ data: { items: [] }, isError: false }),
}));
afterEach(() => {
    cleanup();
    request.mockReset();
    catalog.offers.splice(1);
    catalog.images.splice(1);
    catalog.storage_classes.splice(1);
    catalog.offers[0].storage_classes = ["fast"];
});
function show() {
    render(
        <QueryClientProvider client={new QueryClient()}>
            <RouterProvider
                router={createMemoryRouter(
                    [{ path: "*", element: <CreateMachine /> }],
                    { initialEntries: ["/machines/add/"] },
                )}
            />
        </QueryClientProvider>,
    );
}
it("uses compatible catalog defaults without low-level host or disk controls", async () => {
    show();
    await screen.findByText(/2 vCPU/);
    expect(screen.queryByRole("combobox", { name: "Node" })).toBeNull();
    expect(screen.queryByRole("spinbutton", { name: "vCPUs" })).toBeNull();
    expect(screen.queryByText("Storage pool")).toBeNull();
    expect(screen.queryByText("Large")).toBeNull();
    const summary = screen.getByRole("region", {
        name: "Selected instance type resources",
    });
    expect(within(summary).getByText("2 GiB")).toBeTruthy();
    expect(within(summary).getByText("Boot disk")).toBeTruthy();
    expect(within(summary).getByText("20 GiB")).toBeTruthy();
    request.mockResolvedValueOnce({ task: { id: "task1" } });
    await userEvent.type(
        screen.getByRole("textbox", { name: "Machine name" }),
        "demo",
    );
    await userEvent.click(screen.getByText("Placement (optional)"));
    await userEvent.type(
        screen.getByRole("textbox", { name: "Preferred: rack" }),
        "two",
    );
    await userEvent.click(
        screen.getByRole("button", { name: "Create machine" }),
    );
    await waitFor(() =>
        expect(request).toHaveBeenCalledWith(
            "/api/v1/clouds/local/machines",
            "POST",
            expect.objectContaining({
                name: "demo",
                image: "ubuntu",
                instance_type: "small",
                zone: "z",
                network: "lan",
                storage_class: "fast",
                placement: { required: {}, preferred: { rack: "two" } },
            }),
        ),
    );
    expect(
        await screen.findByRole("link", { name: "View progress and results" }),
    ).toBeTruthy();
    expect(screen.queryByText(/created successfully/i)).toBeNull();
});
it("recovers an uncertain submission without sending another create", async () => {
    show();
    await screen.findByText(/2 vCPU/);
    await userEvent.type(
        screen.getByRole("textbox", { name: "Machine name" }),
        "draft",
    );
    const recovery = vi.fn().mockResolvedValue({ task: { id: "original" } });
    const error = new APIError(0, { detail: "Connection lost" });
    error.recover = recovery;
    request.mockRejectedValueOnce(error);
    await userEvent.click(
        screen.getByRole("button", { name: "Create machine" }),
    );
    await screen.findByText("Connection lost");
    expect(
        (
            screen.getByRole("textbox", {
                name: "Machine name",
            }) as HTMLInputElement
        ).value,
    ).toBe("draft");
    await userEvent.click(
        screen.getByRole("button", { name: "Recover submission" }),
    );
    await waitFor(() => expect(recovery).toHaveBeenCalledOnce());
    expect(request).toHaveBeenCalledOnce();
    expect(
        await screen.findByRole("link", { name: "View progress and results" }),
    ).toBeTruthy();
});

it("filters existing disks by the owning host after the first selection", async () => {
    show();
    await screen.findByText(/2 vCPU/);
    await userEvent.click(
        screen.getByRole("button", { name: "Additional disks" }),
    );
    await userEvent.click(
        screen.getByRole("combobox", { name: "Existing local disks" }),
    );
    await userEvent.click(screen.getByRole("option", { name: /Disk A/ }));
    await waitFor(() =>
        expect(screen.queryByRole("option", { name: /Disk B/ })).toBeNull(),
    );
    expect(screen.getByRole("option", { name: /Disk A/ })).toBeTruthy();
});

it("updates the included resources when the instance type changes", async () => {
    catalog.offers.push({ ...catalog.offers[0], instance_type: "large" });
    show();
    await screen.findByText("2 vCPU");
    await userEvent.click(
        screen.getByRole("combobox", { name: "Instance type" }),
    );
    await userEvent.click(screen.getByRole("option", { name: "Large" }));
    const summary = screen.getByRole("region", {
        name: "Selected instance type resources",
    });
    expect(within(summary).getByText("Included with Large")).toBeTruthy();
    expect(within(summary).getByText("4 vCPU")).toBeTruthy();
    expect(within(summary).getByText("4 GiB")).toBeTruthy();
    expect(within(summary).getByText("40 GiB")).toBeTruthy();
    expect(within(summary).queryByText("20 GiB")).toBeNull();
});

function addAndroid() {
    catalog.images.push({
        id: "android",
        name: "Android",
        data_disks: [{ name: "userdata", size_bytes: 16 * 2 ** 30 }],
    });
    catalog.offers.push({ ...catalog.offers[0], image: "android" });
}

async function chooseImage(name: string) {
    await userEvent.click(screen.getByRole("combobox", { name: "Image" }));
    await userEvent.click(screen.getByRole("option", { name }));
}

it("shows automatic image disks without including them in the create request", async () => {
    addAndroid();
    show();
    await screen.findByText("2 vCPU");
    await chooseImage("Android");
    const required = screen.getByRole("region", {
        name: "Required image disks",
    });
    expect(within(required).getByText("userdata · 16 GiB · Fast")).toBeTruthy();
    expect(within(required).queryByRole("textbox")).toBeNull();
    expect(screen.getByText("Total new storage: 36 GiB")).toBeTruthy();
    request.mockResolvedValueOnce({ task: { id: "android-task" } });
    await userEvent.type(
        screen.getByRole("textbox", { name: "Machine name" }),
        "android",
    );
    await userEvent.click(
        screen.getByRole("button", { name: "Create machine" }),
    );
    await waitFor(() =>
        expect(request).toHaveBeenCalledWith(
            "/api/v1/clouds/local/machines",
            "POST",
            expect.objectContaining({ image: "android", new_disks: [] }),
        ),
    );
});

it("keeps user disks separate while image disks follow image and storage choices", async () => {
    catalog.storage_classes.push({ id: "bulk", name: "Bulk" });
    catalog.offers[0].storage_classes.push("bulk");
    addAndroid();
    show();
    await screen.findByText("2 vCPU");
    await chooseImage("Android");
    await userEvent.click(
        screen.getByRole("combobox", { name: "Storage class" }),
    );
    await userEvent.click(screen.getByRole("option", { name: "Bulk" }));
    expect(screen.getByText("userdata · 16 GiB · Bulk")).toBeTruthy();
    await userEvent.click(
        screen.getByRole("button", { name: "Additional disks" }),
    );
    await userEvent.click(screen.getByRole("button", { name: "Add disk" }));
    expect(screen.getByText("Total new storage: 46 GiB")).toBeTruthy();
    const diskName = screen.getByRole("textbox", { name: "Disk 1 name" });
    await userEvent.clear(diskName);
    await userEvent.type(diskName, "userdata");
    expect(
        screen.getByText(/This name is reserved for a required image disk/),
    ).toBeTruthy();
    expect(
        (
            screen.getByRole("button", {
                name: "Create machine",
            }) as HTMLButtonElement
        ).disabled,
    ).toBe(true);
    await chooseImage("Ubuntu");
    expect(
        screen.queryByRole("region", { name: "Required image disks" }),
    ).toBeNull();
    expect(screen.getByText("Total new storage: 30 GiB")).toBeTruthy();
    expect((diskName as HTMLInputElement).value).toBe("userdata");
    await chooseImage("Android");
    expect(screen.getAllByText("userdata · 16 GiB · Bulk")).toHaveLength(1);
    expect(screen.getByText("Total new storage: 46 GiB")).toBeTruthy();
    await userEvent.clear(diskName);
    await userEvent.type(diskName, "extra");
    request.mockResolvedValueOnce({ task: { id: "android-task" } });
    await userEvent.type(
        screen.getByRole("textbox", { name: "Machine name" }),
        "android",
    );
    await userEvent.click(
        screen.getByRole("button", { name: "Create machine" }),
    );
    await waitFor(() =>
        expect(request).toHaveBeenCalledWith(
            "/api/v1/clouds/local/machines",
            "POST",
            expect.objectContaining({
                image: "android",
                storage_class: "bulk",
                new_disks: [
                    {
                        name: "extra",
                        size_bytes: 10 * 2 ** 30,
                        storage_class: "bulk",
                    },
                ],
            }),
        ),
    );
});
