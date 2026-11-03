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
const templates = vi.hoisted(() => [
    {
        id: "setup",
        name: "Server setup",
        description: "Install tools",
        revision: "r1",
    },
]);
const catalog = vi.hoisted(() => ({
    id: "local",
    name: "Local cloud",
    zones: [{ id: "z", name: "Zone" }],
    images: [{ id: "ubuntu", name: "Ubuntu" }] as {
        id: string;
        name: string;
        data_disks?: { name: string; size_bytes: number }[];
        defaults?: { initialization: string };
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
                : path === "/api/v1/cloud-init-templates"
                  ? templates
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

async function chooseSource(name: string) {
    await userEvent.click(
        screen.getByRole("combobox", { name: "Cloud-init user-data source" }),
    );
    await userEvent.click(screen.getByRole("option", { name }));
}
async function nameAndSubmit() {
    await userEvent.type(
        screen.getByRole("textbox", { name: "Machine name" }),
        "configured",
    );
    await userEvent.click(
        screen.getByRole("button", { name: "Create machine" }),
    );
}
it("submits exactly one viewed template revision without copying user-data into the request", async () => {
    request.mockImplementation(async (path: string) =>
        path.endsWith("/setup")
            ? {
                  ...templates[0],
                  user_data: "#cloud-config\npackages: [curl]\n",
              }
            : { task: { id: "created" } },
    );
    show();
    await screen.findByText("2 vCPU");
    await chooseSource("Server setup");
    expect(
        (
            (await screen.findByRole("textbox", {
                name: "Template preview",
            })) as HTMLTextAreaElement
        ).value,
    ).toBe("#cloud-config\npackages: [curl]\n");
    await nameAndSubmit();
    const body = request.mock.calls.find(
        ([, method]) => method === "POST",
    )?.[2];
    expect(body.user_data_template).toEqual({ id: "setup", revision: "r1" });
    expect(body).not.toHaveProperty("user_data");
});
it("customizes a template for one machine and confirms replacement of custom text", async () => {
    request.mockResolvedValue({
        ...templates[0],
        user_data: "#cloud-config\n",
    });
    show();
    await screen.findByText("2 vCPU");
    await chooseSource("Server setup");
    await userEvent.click(
        await screen.findByRole("button", {
            name: "Customize for this machine",
        }),
    );
    const editor = screen.getByRole("textbox", {
        name: "Cloud-init user-data",
    });
    expect((editor as HTMLTextAreaElement).value).toBe("#cloud-config\n");
    await userEvent.type(editor, "hostname: demo");
    await chooseSource("None");
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect((editor as HTMLTextAreaElement).value).toContain("hostname: demo");
    request.mockResolvedValue({ task: { id: "custom" } });
    await nameAndSubmit();
    const body = request.mock.calls.find(
        ([, method]) => method === "POST",
    )?.[2];
    expect(body.user_data).toBe("#cloud-config\nhostname: demo");
    expect(body).not.toHaveProperty("user_data_template");
    expect(request.mock.calls.every(([, method]) => method !== "PUT")).toBe(
        true,
    );
});
it("discards custom text only after confirmation", async () => {
    show();
    await screen.findByText("2 vCPU");
    await chooseSource("Custom");
    await userEvent.type(
        screen.getByRole("textbox", { name: "Cloud-init user-data" }),
        "secret draft",
    );
    await chooseSource("None");
    await userEvent.click(
        screen.getByRole("button", { name: "Discard changes" }),
    );
    await waitFor(() =>
        expect(
            screen.queryByRole("textbox", { name: "Cloud-init user-data" }),
        ).toBeNull(),
    );
    await chooseSource("Custom");
    expect(
        (
            screen.getByRole("textbox", {
                name: "Cloud-init user-data",
            }) as HTMLTextAreaElement
        ).value,
    ).toBe("");
});
it("omits user-data when switching to an image with initialization disabled", async () => {
    addAndroid();
    catalog.images[1].defaults = { initialization: "none" };
    show();
    await screen.findByText("2 vCPU");
    await chooseSource("Custom");
    await userEvent.type(
        screen.getByRole("textbox", { name: "Cloud-init user-data" }),
        "#cloud-config",
    );
    await chooseImage("Android");
    expect(
        await screen.findByText(/This image has initialization disabled/),
    ).toBeTruthy();
    request.mockResolvedValue({ task: { id: "no-init" } });
    await nameAndSubmit();
    const body = request.mock.calls.find(
        ([, method]) => method === "POST",
    )?.[2];
    expect(body).not.toHaveProperty("user_data");
    expect(body).not.toHaveProperty("user_data_template");
});
it("blocks creation when a selected template cannot be loaded", async () => {
    request.mockRejectedValue(
        new APIError(404, { detail: "Template not found." }),
    );
    show();
    await screen.findByText("2 vCPU");
    await chooseSource("Server setup");
    await screen.findByText("Template not found.");
    expect(
        (
            screen.getByRole("button", {
                name: "Create machine",
            }) as HTMLButtonElement
        ).disabled,
    ).toBe(true);
    await chooseSource("None");
    await waitFor(() =>
        expect(
            (
                screen.getByRole("button", {
                    name: "Create machine",
                }) as HTMLButtonElement
            ).disabled,
        ).toBe(false),
    );
});
