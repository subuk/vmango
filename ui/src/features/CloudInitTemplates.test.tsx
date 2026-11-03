import { afterEach, expect, it, vi } from "vitest";
import {
    cleanup,
    render,
    screen,
    waitFor,
    within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { CloudInitTemplates } from "./CloudInitTemplates";
import { APIError } from "../api/client";

const request = vi.hoisted(() => vi.fn());
vi.mock("../api/client", async (original) => ({
    ...(await original<typeof import("../api/client")>()),
    request,
}));
const path = "/api/v1/cloud-init-templates";
const template = {
    id: "setup",
    name: "Server setup",
    description: "Install tools",
    revision: "r1",
    user_data: "#cloud-config\npackages: [curl]\n",
};
afterEach(() => {
    cleanup();
    request.mockReset();
});
function show() {
    const client = new QueryClient({
        defaultOptions: { queries: { retry: false } },
    });
    render(
        <QueryClientProvider client={client}>
            <MemoryRouter>
                <CloudInitTemplates />
            </MemoryRouter>
        </QueryClientProvider>,
    );
    return client;
}
function list() {
    const { user_data: _, ...summary } = template;
    return [summary];
}
it("creates a template preserving user-data exactly", async () => {
    request.mockImplementation(async (_url, method) =>
        method === "GET" ? [] : template,
    );
    show();
    await userEvent.click(screen.getByRole("button", { name: "Add template" }));
    const dialog = screen.getByRole("dialog");
    await userEvent.type(
        within(dialog).getByRole("textbox", { name: "Name" }),
        "Server setup",
    );
    await userEvent.type(
        within(dialog).getByRole("textbox", { name: "Description" }),
        "Install tools",
    );
    await userEvent.click(
        within(dialog).getByRole("textbox", { name: "Cloud-init user-data" }),
    );
    await userEvent.paste(template.user_data);
    await userEvent.click(
        within(dialog).getByRole("button", { name: "Add template" }),
    );
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(request).toHaveBeenCalledWith(path, "POST", {
        name: template.name,
        description: template.description,
        user_data: template.user_data,
    });
});
it("loads content only for an opened template and removes its cache on close", async () => {
    request.mockImplementation(async (url) =>
        url === path ? list() : template,
    );
    const client = show();
    await screen.findByRole("button", { name: "Server setup" });
    expect(request.mock.calls.some(([url]) => url === `${path}/setup`)).toBe(
        false,
    );
    await userEvent.click(screen.getByRole("button", { name: "View / edit" }));
    expect(
        (
            (await screen.findByRole("textbox", {
                name: "Cloud-init user-data",
            })) as HTMLTextAreaElement
        ).value,
    ).toBe(template.user_data);
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() =>
        expect(
            client.getQueryData(["cloud-init-template", "setup"]),
        ).toBeUndefined(),
    );
    expect(
        request.mock.calls.filter(([url]) => url === `${path}/setup`),
    ).toHaveLength(1);
});
it("preserves edits on conflict and explicitly reloads the newer revision", async () => {
    let latest = template;
    request.mockImplementation(async (url, method) => {
        if (method === "PUT")
            throw new APIError(409, { detail: "Template revision changed." });
        return url === path ? list() : latest;
    });
    show();
    await userEvent.click(
        await screen.findByRole("button", { name: "View / edit" }),
    );
    const editor = await screen.findByRole("textbox", {
        name: "Cloud-init user-data",
    });
    await userEvent.type(editor, "# my edit");
    await userEvent.click(
        screen.getByRole("button", { name: "Save template" }),
    );
    await screen.findByText("Template revision changed.");
    expect((editor as HTMLTextAreaElement).value).toContain("# my edit");
    expect(request).toHaveBeenCalledWith(
        `${path}/setup`,
        "PUT",
        expect.objectContaining({ revision: "r1" }),
    );
    latest = {
        ...template,
        revision: "r2",
        user_data: "#cloud-config\nhostname: latest\n",
    };
    await userEvent.click(
        screen.getByRole("button", { name: "Reload latest" }),
    );
    await userEvent.click(
        screen.getByRole("button", { name: "Discard changes" }),
    );
    await waitFor(() =>
        expect(
            (
                screen.getByRole("textbox", {
                    name: "Cloud-init user-data",
                }) as HTMLTextAreaElement
            ).value,
        ).toBe(latest.user_data),
    );
    expect(screen.queryByText("Template revision changed.")).toBeNull();
});
it("deletes with the listed revision and refreshes the catalog", async () => {
    let deleted = false;
    request.mockImplementation(async (_url, method) => {
        if (method === "DELETE") {
            deleted = true;
            return undefined;
        }
        return deleted ? [] : list();
    });
    show();
    await userEvent.click(
        await screen.findByRole("button", { name: "Delete" }),
    );
    await userEvent.click(
        screen.getByRole("button", { name: "Delete template" }),
    );
    await screen.findByText("No cloud-init templates have been added.");
    expect(request).toHaveBeenCalledWith(`${path}/setup`, "DELETE", {
        revision: "r1",
    });
});
