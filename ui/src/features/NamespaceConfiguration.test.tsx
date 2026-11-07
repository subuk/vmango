import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { NamespaceConfiguration } from "./AdministrationInventory";

const request = vi.hoisted(() => vi.fn());
vi.mock("../api/client", async (original) => ({
    ...(await original<typeof import("../api/client")>()),
    request,
}));
vi.mock("../app/Auth", () => ({ useMe: () => ({ id: "administrator" }) }));
const namespaces = [
    {
        id: "team-a",
        name: "Team A",
        description: "Production services",
        clouds: ["west"],
    },
    {
        id: "team-b",
        name: "Team B",
        description: "Development",
        clouds: ["east"],
    },
];
afterEach(() => {
    cleanup();
    request.mockReset();
});
function show(path: string) {
    request.mockImplementation(async (path: string) =>
        path === "/api/v1/administration/configuration"
            ? { snapshot_id: "accepted" }
            : { items: namespaces },
    );
    const router = createMemoryRouter(
        [
            {
                path: "/administration/configuration/namespaces/",
                element: <NamespaceConfiguration />,
            },
            {
                path: "/administration/configuration/namespaces/:namespace/",
                element: <NamespaceConfiguration />,
            },
        ],
        { initialEntries: [path] },
    );
    render(
        <QueryClientProvider
            client={
                new QueryClient({
                    defaultOptions: { queries: { retry: false } },
                })
            }
        >
            <RouterProvider router={router} />
        </QueryClientProvider>,
    );
    return router;
}
it("links namespace resources through Administration filters without changing workload scope", async () => {
    show(
        "/administration/configuration/namespaces/team-a/?namespace=unrelated",
    );
    const detail = await screen.findByRole("region", { name: "Team A" });
    for (const [label, path] of [
        ["Machine inventory", "machines"],
        ["Volume inventory", "volumes"],
    ]) {
        const target = within(detail)
            .getByRole("link", { name: label })
            .getAttribute("href")!;
        const url = new URL(target, "http://vmango.test");
        expect(url.pathname).toBe(`/administration/clouds/west/${path}/`);
        expect(url.searchParams.get("inventory_namespace")).toBe("team-a");
        expect(url.searchParams.has("namespace")).toBe(false);
    }
    expect(
        within(detail)
            .getByRole("link", { name: "Namespace task history" })
            .getAttribute("href"),
    ).toBe("/administration/tasks/?namespace_id=team-a");
    expect(
        request.mock.calls.every(([path]) =>
            String(path).startsWith("/api/v1/administration/"),
        ),
    ).toBe(true);
});
it("reports a missing selected namespace while retaining the accepted namespace inventory", async () => {
    show(
        "/administration/configuration/namespaces/?namespace_selected=removed",
    );
    expect(await screen.findByRole("alert")).toHaveProperty(
        "textContent",
        "Namespace not found in accepted configuration.",
    );
    expect(screen.getByText("Team A")).toBeTruthy();
    expect(screen.getByText("Team B")).toBeTruthy();
    expect(
        screen.queryByRole("link", { name: "Machine inventory" }),
    ).toBeNull();
});
it("keeps search, selection and sorting on a round trip through full namespace details", async () => {
    const router = show(
        "/administration/configuration/namespaces/?q=Production&sort=id&direction=desc",
    );
    const row = (await screen.findByText("Team A")).closest("tr")!;
    await userEvent.click(row);
    await userEvent.click(
        await screen.findByRole("link", { name: "Full details" }),
    );
    expect(router.state.location.pathname).toBe(
        "/administration/configuration/namespaces/team-a/",
    );
    expect(screen.queryByRole("table")).toBeNull();
    await userEvent.click(screen.getByRole("link", { name: "All namespaces" }));
    const params = new URLSearchParams(router.state.location.search);
    expect(params.get("q")).toBe("Production");
    expect(params.get("sort")).toBe("id");
    expect(params.get("direction")).toBe("desc");
    expect(params.get("namespace_selected")).toBe("team-a");
    expect(await screen.findByRole("region", { name: "Team A" })).toBeTruthy();
    expect(screen.queryByText("Team B")).toBeNull();
});
