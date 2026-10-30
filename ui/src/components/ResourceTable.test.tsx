import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
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
