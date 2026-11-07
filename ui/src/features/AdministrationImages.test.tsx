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
import {
    Images,
    ImageDetail,
    loadImageAvailability,
} from "./AdministrationImages";
import type { AdminCloud, AdminImage } from "../api/administration";
const mocks = vi.hoisted(() => ({ request: vi.fn() }));
vi.mock("../api/client", async (original) => ({
    ...(await original<typeof import("../api/client")>()),
    request: mocks.request,
}));
vi.mock("../app/Auth", () => ({ useMe: () => ({ id: "operator" }) }));
const cloud = {
    id: "local",
    name: "Local",
    backend_kind: "libvirt",
    capabilities: { host_inventory: true, image_availability: true },
    zones: [],
    instance_types: [],
    images: [],
    networks: [],
    storage_classes: [],
    libvirt: {
        hosts: [
            { id: "alpha", cloud_id: "local", zone: "z" },
            { id: "beta", cloud_id: "local", zone: "z" },
        ],
    },
} as AdminCloud;
const images = Array.from({ length: 10 }, (_, i) => ({
    id: `image-${i}`,
    cloud_id: "local",
    name: `Image ${i}`,
    arch: "x86_64",
    os_name: "Linux",
    os_version: "1",
    minimum_memory_bytes: 1024,
    minimum_disk_bytes: 1024,
    source: { mode: "download", format: "qcow2", sha256: "", size_bytes: 0 },
    protected: false,
    hidden: false,
    defaults: {
        disk_bus: "virtio",
        disk_controller: "",
        firmware: "",
        graphics: "",
        guest_agent: null,
        initialization: "cloud-init",
        nic_model: "virtio",
        video_model: "",
    },
    settings: [],
})) as AdminImage[];
function availability(image: string) {
    return {
        cloud_id: "local",
        image_id: image,
        libvirt: {
            hosts: [
                {
                    host_id: "alpha",
                    observation: "observed",
                    compatibility: "supported",
                    source_state: "downloadable",
                },
                {
                    host_id: "beta",
                    observation: "unavailable",
                    compatibility: "unknown",
                    source_state: "unknown",
                    code: "host_unavailable",
                },
            ],
        },
    };
}
function base() {
    mocks.request.mockImplementation(async (path: string) => {
        if (path.endsWith("/configuration")) return { snapshot_id: "snapshot" };
        if (path.endsWith("/images")) return images;
        if (path.endsWith("/availability"))
            return availability(path.split("/").at(-2)!);
        const image = images.find((i) => path.endsWith(`/images/${i.id}`));
        if (image) return image;
        throw new Error(`Unexpected request ${path}`);
    });
}
function show(path = "/?view=availability", c = cloud) {
    const queries = new QueryClient({
        defaultOptions: { queries: { retry: false } },
    });
    const router = createMemoryRouter(
        [
            { path: "/", element: <Images cloud={c} snapshot="snapshot" /> },
            {
                path: "/administration/clouds/local/images/",
                element: <Images cloud={c} snapshot="snapshot" />,
            },
            {
                path: "/administration/clouds/local/images/:image/",
                element: <ImageDetail cloud={c} snapshot="snapshot" />,
            },
        ],
        { initialEntries: [path] },
    );
    render(
        <QueryClientProvider client={queries}>
            <RouterProvider router={router} />
        </QueryClientProvider>,
    );
    return { router, queries };
}
afterEach(() => {
    cleanup();
    mocks.request.mockReset();
});
it("loads availability only for the current eight image rows and reuses the catalog on paging", async () => {
    base();
    show();
    const matrix = await screen.findByRole("table", {
        name: "Image availability matrix",
    });
    await waitFor(() =>
        expect(
            within(matrix).getAllByText("Source: downloadable"),
        ).toHaveLength(8),
    );
    expect(
        within(matrix).getAllByText("Compatibility: supported"),
    ).toHaveLength(8);
    expect(within(matrix).getAllByText("Compatibility: unknown")).toHaveLength(
        8,
    );
    expect(within(matrix).getAllByText("Source: unknown")).toHaveLength(8);
    const observations = () =>
        mocks.request.mock.calls.filter(([p]) => p.endsWith("/availability"));
    expect(observations()).toHaveLength(8);
    await userEvent.click(screen.getByRole("button", { name: "Next images" }));
    await waitFor(() =>
        expect(
            within(matrix).getAllByText("Source: downloadable"),
        ).toHaveLength(2),
    );
    expect(observations()).toHaveLength(10);
    expect(
        mocks.request.mock.calls.filter(([p]) => p.endsWith("/images")),
    ).toHaveLength(1);
    expect(
        mocks.request.mock.calls.every(
            ([p]) =>
                p.endsWith("/images") ||
                p.endsWith("/configuration") ||
                p.endsWith("/availability"),
        ),
    ).toBe(true);
});
it("gates availability requests by capability even for a deep link", async () => {
    base();
    show("/?view=availability", {
        ...cloud,
        capabilities: { host_inventory: false, image_availability: false },
        libvirt: undefined,
    });
    await screen.findByRole("link", { name: "Image 0" });
    expect(screen.queryByRole("tab", { name: "Host availability" })).toBeNull();
    expect(
        mocks.request.mock.calls.some(([p]) => p.endsWith("/availability")),
    ).toBe(false);
});
it("keeps catalog and availability filters separate in the URL", async () => {
    base();
    const { router } = show(
        "/?view=availability&q=catalog-only&availabilitySearch=image-9",
    );
    await waitFor(() =>
        expect(screen.getByText("Source: downloadable")).toBeTruthy(),
    );
    expect(
        screen.getByText("Showing 1 of 1 images · Page 1 of 1"),
    ).toBeTruthy();
    await userEvent.click(screen.getByRole("tab", { name: "Catalog" }));
    expect(new URLSearchParams(router.state.location.search).get("q")).toBe(
        "catalog-only",
    );
    expect(
        new URLSearchParams(router.state.location.search).get(
            "availabilitySearch",
        ),
    ).toBe("image-9");
});
it("limits concurrent observation requests to three and isolates individual failures", async () => {
    let active = 0;
    let maximum = 0;
    const releases: (() => void)[] = [];
    mocks.request.mockImplementation(
        (path: string) =>
            new Promise((resolve, reject) => {
                active++;
                maximum = Math.max(maximum, active);
                releases.push(() => {
                    active--;
                    path.includes("image-4/")
                        ? reject(new Error("Host observation denied"))
                        : resolve(availability(path.split("/").at(-2)!));
                });
            }),
    );
    const result = loadImageAvailability(
        "local",
        images.slice(0, 8).map((i) => i.id),
        new AbortController().signal,
    );
    expect(mocks.request).toHaveBeenCalledTimes(3);
    for (let i = 0; i < 8; i++) {
        releases[i]();
        await Promise.resolve();
        await Promise.resolve();
    }
    const data = await result;
    expect(maximum).toBe(3);
    expect(data).toHaveLength(8);
    expect(data.find((r) => r.image === "image-4")?.error).toBe(
        "Host observation denied",
    );
    expect(data.filter((r) => r.data)).toHaveLength(7);
});
it("does not begin queued observation requests after cancellation", async () => {
    const releases: (() => void)[] = [];
    mocks.request.mockImplementation(
        () =>
            new Promise((resolve) =>
                releases.push(() => resolve(availability("image"))),
            ),
    );
    const controller = new AbortController();
    const result = loadImageAvailability(
        "local",
        images.map((i) => i.id),
        controller.signal,
    );
    controller.abort();
    releases.forEach((release) => release());
    await result;
    expect(mocks.request).toHaveBeenCalledTimes(3);
});

it("restores matrix view, architecture, page and search after image details", async () => {
    base();
    const listQuery =
        "?view=availability&arch=x86_64&availabilitySearch=Image&availabilityPage=1&q=catalog-search";
    const { router } = show(`/administration/clouds/local/images/${listQuery}`);
    const image = await screen.findByRole("link", { name: "Image 8" });
    expect(image.getAttribute("href")).toBe(
        `/administration/clouds/local/images/image-8/${listQuery}`,
    );
    await userEvent.click(image);
    const settings = await screen.findByRole("region", {
        name: "Default and inheritance state",
    });
    await userEvent.type(
        within(settings).getByRole("textbox", { name: "Search" }),
        "detail-filter",
    );
    const observations = screen.getByRole("region", {
        name: "Live image availability",
    });
    await userEvent.type(
        await within(observations).findByRole("textbox", { name: "Search" }),
        "alpha",
    );
    expect(new URLSearchParams(router.state.location.search).get("q")).toBe(
        "catalog-search",
    );
    const back = screen.getByRole("link", { name: "All images" });
    expect(back.getAttribute("href")).toBe(
        `/administration/clouds/local/images/${listQuery}`,
    );
    await userEvent.click(back);
    expect(router.state.location.search).toBe(listQuery);
    expect(
        await screen.findByRole("table", { name: "Image availability matrix" }),
    ).toBeTruthy();
    expect(
        screen.getByText("Showing 2 of 10 images · Page 2 of 2"),
    ).toBeTruthy();
});

it("preserves catalog filters on image detail links", async () => {
    base();
    show(
        "/administration/clouds/local/images/?arch=x86_64&q=Image+2&sort=id&direction=desc",
    );
    expect(
        (await screen.findByRole("link", { name: "Image 2" })).getAttribute(
            "href",
        ),
    ).toBe(
        "/administration/clouds/local/images/image-2/?arch=x86_64&q=Image+2&sort=id&direction=desc",
    );
});
