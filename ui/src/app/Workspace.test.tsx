import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider, useLocation } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { WorkspaceBoundary, WorkspaceLink, NamespacePicker } from "./Workspace";
import { useResource } from "../api/hooks";
import { namespaceSelection, namespaceURL } from "../api/context";
const request = vi.hoisted(() => vi.fn());
vi.mock("../api/client", async (original) => ({
    ...(await original<typeof import("../api/client")>()),
    request,
}));
vi.mock("./Auth", () => ({ useMe: () => ({ id: "operator" }) }));
const namespaces = {
    items: [
        { id: "platform", name: "Platform", clouds: ["local"] },
        { id: "analytics", name: "Analytics", clouds: ["local"] },
    ],
};
beforeEach(() =>
    request.mockImplementation(
        async (path, _method, _body, _signal, context) =>
            path === "/api/v1/namespaces"
                ? namespaces
                : { name: context.namespace },
    ),
);
afterEach(() => {
    cleanup();
    request.mockReset();
    localStorage.clear();
});
function Workload() {
    const query = useResource<{ name: string }>(
        "/api/v1/clouds/local/machines",
    );
    const route = useLocation();
    return (
        <>
            <p>{query.data?.name || "Loading workload"}</p>
            <WorkspaceLink to="/machines/local/example/">
                Machine link
            </WorkspaceLink>
            <span data-testid="url">
                {route.pathname}
                {route.search}
            </span>
        </>
    );
}
function show(url: string, form = false) {
    const client = new QueryClient({
        defaultOptions: { queries: { retry: false } },
    });
    const router = createMemoryRouter(
        [
            {
                path: "*",
                element: (
                    <WorkspaceBoundary>
                        {form ? (
                            <>
                                <NamespacePicker />
                                <form>
                                    <input aria-label="Name" />
                                </form>
                            </>
                        ) : (
                            <Workload />
                        )}
                    </WorkspaceBoundary>
                ),
            },
        ],
        { initialEntries: [url] },
    );
    render(
        <QueryClientProvider client={client}>
            <RouterProvider router={router} />
        </QueryClientProvider>,
    );
    return { router, client };
}
it("does not send workload requests before namespace selection", async () => {
    const { router } = show("/machines/local/example/");
    await screen.findByRole("heading", { name: "Choose a namespace" });
    expect(
        request.mock.calls.every(([path]) => path === "/api/v1/namespaces"),
    ).toBe(true);
    await userEvent.click(screen.getByRole("combobox", { name: "Namespace" }));
    await userEvent.click(
        await screen.findByRole("option", { name: "Platform" }),
    );
    await screen.findByText("platform");
    expect(router.state.location.pathname).toBe("/machines/local/example/");
    expect(request).toHaveBeenCalledWith(
        "/api/v1/clouds/local/machines",
        "GET",
        undefined,
        expect.any(AbortSignal),
        { namespace: "platform", principal: "operator" },
    );
    expect(
        screen.getByRole("link", { name: "Machine link" }).getAttribute("href"),
    ).toBe("/machines/local/example/?namespace=platform");
});
it.each([
    "?namespace=",
    "?namespace=a&namespace=b",
    "?namespace=missing",
    "?namespace=bad%20id",
])("rejects %s without choosing a fallback", async (search) => {
    show(`/machines/${search}`);
    await screen.findByText(/namespace in this URL is invalid or unavailable/);
    expect(
        request.mock.calls.every(([path]) => path === "/api/v1/namespaces"),
    ).toBe(true);
});
it("restores URL selection on Back and Forward regardless of shared storage", async () => {
    localStorage.setItem("vmango.namespace", "analytics");
    const { router } = show("/machines/?namespace=platform");
    await screen.findByText("platform");
    await act(async () => {
        await router.navigate("/machines/?namespace=analytics");
    });
    await screen.findByText("analytics");
    await act(async () => {
        await router.navigate(-1);
    });
    await screen.findByText("platform");
    await act(async () => {
        await router.navigate(1);
    });
    await screen.findByText("analytics");
});
it("does not display an old namespace response after switching", async () => {
    let resolve!: (value: { name: string }) => void;
    request.mockImplementation(
        async (path, _method, _body, _signal, context) => {
            if (path === "/api/v1/namespaces") return namespaces;
            if (context.namespace === "platform")
                return new Promise((done) => {
                    resolve = done;
                });
            return { name: "analytics" };
        },
    );
    const { router } = show("/machines/?namespace=platform");
    await waitFor(() => expect(resolve).toBeTypeOf("function"));
    await act(async () => {
        await router.navigate("/machines/?namespace=analytics");
    });
    await screen.findByText("analytics");
    await act(async () => {
        resolve({ name: "platform" });
    });
    expect(screen.queryByText("platform")).toBeNull();
});
it("blocks namespace switching while a workload form has unsaved edits", async () => {
    const { router } = show("/machines/?namespace=platform", true);
    await userEvent.type(
        await screen.findByRole("textbox", { name: "Name" }),
        "unsaved",
    );
    await act(async () => {
        void router.navigate("/machines/?namespace=analytics");
    });
    await screen.findByRole("dialog", { name: "Discard unsaved changes?" });
    await userEvent.click(screen.getByRole("button", { name: "Stay on page" }));
    expect(router.state.location.search).toBe("?namespace=platform");
    await act(async () => {
        void router.navigate("/machines/?namespace=analytics");
    });
    await userEvent.click(
        await screen.findByRole("button", { name: "Discard and leave" }),
    );
    await waitFor(() =>
        expect(router.state.location.search).toBe("?namespace=analytics"),
    );
});
it("keeps administrative URLs independent and explicit workload URLs intact", () => {
    expect(namespaceURL("/administration/clouds/local/", "platform")).toBe(
        "/administration/clouds/local/",
    );
    expect(namespaceURL("/tasks/saved/?namespace=analytics", "platform")).toBe(
        "/tasks/saved/?namespace=analytics",
    );
    expect(namespaceSelection("?namespace=one&namespace=one")).toEqual({
        kind: "invalid",
    });
});
