import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router";
import { ResourceTable } from "./ResourceTable";

vi.mock("../api/hooks", () => ({ useNodes: () => ({ data: [] }) }));
afterEach(cleanup);

it("resets table filters and sorting while retaining the cloud and detail tab", async () => {
    const router = createMemoryRouter(
        [
            {
                path: "/",
                element: (
                    <ResourceTable
                        rows={[{ name: "Disk", size: 20 }]}
                        rowKey={(r) => r.name}
                        search={(r) => r.name}
                        columns={[
                            { id: "name", label: "Name", value: (r) => r.name },
                        ]}
                    />
                ),
            },
        ],
        {
            initialEntries: [
                "/?cloud=west&tab=disks&q=missing&node=host&state=stopped&pool=ssd&sort=name&direction=desc",
            ],
        },
    );
    render(<RouterProvider router={router} />);
    expect(screen.getByText(/No matches/)).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "Reset" }));
    expect(router.state.location.search).toBe("?cloud=west&tab=disks");
    expect(screen.getByRole("cell", { name: "Disk" })).toBeTruthy();
    expect(screen.getByText("Showing 1 of 1 resources")).toBeTruthy();
});

it("isolates nested filters and resets without changing inventory context", async () => {
    const router = createMemoryRouter(
        [
            {
                path: "/",
                element: (
                    <ResourceTable
                        paramPrefix="disk_"
                        rows={[
                            { name: "root", size: 2 },
                            { name: "data", size: 10 },
                        ]}
                        rowKey={(r) => r.name}
                        search={(r) => r.name}
                        columns={[
                            {
                                id: "size",
                                label: "Size",
                                value: (r) => r.size,
                                numeric: true,
                            },
                        ]}
                    />
                ),
            },
        ],
        {
            initialEntries: [
                "/?namespace=work&inventory_namespace=ops&selected=vm&node=host&q=machine&disk_q=missing",
            ],
        },
    );
    render(<RouterProvider router={router} />);
    await userEvent.click(screen.getByRole("button", { name: "Reset" }));
    expect(router.state.location.search).toBe(
        "?namespace=work&inventory_namespace=ops&selected=vm&node=host&q=machine",
    );
    expect(screen.getAllByRole("cell").map((cell) => cell.textContent)).toEqual(
        ["2", "10"],
    );
    await userEvent.click(screen.getByText("Size"));
    expect(screen.getAllByRole("cell").map((cell) => cell.textContent)).toEqual(
        ["10", "2"],
    );
    expect(router.state.location.search).toContain("disk_sort=size");
});

it("persists optional columns per table and principal and supports keyboard row selection", async () => {
    localStorage.clear();
    const select = vi.fn();
    const table = (principal: string) => (
        <ResourceTable
            preferenceKey={`${principal}:machines`}
            rows={[{ name: "Machine", id: "id-1" }]}
            rowKey={(r) => r.id}
            search={(r) => r.name}
            onSelect={select}
            selectedKey="id-1"
            columns={[
                { id: "name", label: "Name", value: (r) => r.name },
                {
                    id: "id",
                    label: "ID",
                    value: (r) => r.id,
                    defaultHidden: true,
                },
            ]}
        />
    );
    const router = createMemoryRouter(
        [
            { path: "/one", element: table("one") },
            { path: "/two", element: table("two") },
        ],
        { initialEntries: ["/one"] },
    );
    render(<RouterProvider router={router} />);
    expect(screen.queryByRole("columnheader", { name: "ID" })).toBeNull();
    const row = screen.getByRole("row", { name: "Machine", selected: true });
    row.focus();
    await userEvent.keyboard("{Enter}");
    expect(select).toHaveBeenCalledWith({ name: "Machine", id: "id-1" });
    await userEvent.click(screen.getByRole("button", { name: "Columns" }));
    await userEvent.click(screen.getByRole("menuitemcheckbox", { name: "ID" }));
    await userEvent.keyboard("{Escape}");
    expect(screen.getByRole("columnheader", { name: "ID" })).toBeTruthy();
    expect(
        JSON.parse(localStorage.getItem("vmango.columns.one:machines")!),
    ).toEqual(["name", "id"]);
    await act(async () => {
        await router.navigate("/two");
    });
    expect(screen.queryByRole("columnheader", { name: "ID" })).toBeNull();
    await act(async () => {
        await router.navigate("/one");
    });
    expect(screen.getByRole("columnheader", { name: "ID" })).toBeTruthy();
});

it("toggles column menu options with Enter and Space while keeping a visible column", async () => {
    localStorage.clear();
    const router = createMemoryRouter([
        {
            path: "/",
            element: (
                <ResourceTable
                    preferenceKey="keyboard:columns"
                    rows={[{ name: "Disk", id: "disk-id" }]}
                    rowKey={(r) => r.id}
                    search={(r) => r.name}
                    columns={[
                        { id: "name", label: "Name", value: (r) => r.name },
                        {
                            id: "id",
                            label: "ID",
                            value: (r) => r.id,
                            defaultHidden: true,
                        },
                    ]}
                />
            ),
        },
    ]);
    render(<RouterProvider router={router} />);
    const button = screen.getByRole("button", { name: "Columns" });
    button.focus();
    await userEvent.keyboard("{Enter}");
    const idOption = screen.getByRole("menuitemcheckbox", { name: "ID" });
    expect(document.activeElement).toBe(idOption);
    expect(
        screen
            .getByRole("menuitemcheckbox", { name: "Name" })
            .getAttribute("aria-disabled"),
    ).toBe("true");
    await userEvent.keyboard("{Enter}");
    expect(idOption.getAttribute("aria-checked")).toBe("true");
    await userEvent.keyboard(" ");
    expect(idOption.getAttribute("aria-checked")).toBe("false");
    await userEvent.keyboard("{Enter}{Escape}");
    expect(screen.getByRole("columnheader", { name: "ID" })).toBeTruthy();
    expect(document.activeElement).toBe(button);
});
