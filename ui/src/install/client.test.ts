import { afterEach, expect, it, vi } from "vitest";

afterEach(() => {
    vi.unstubAllGlobals();
    window.history.replaceState(null, "", "/");
});

it("exchanges the URL fragment once and removes it only after successful session creation", async () => {
    vi.resetModules();
    window.history.replaceState(null, "", "/install/#token=private-session");
    const fetcher = vi
        .fn()
        .mockResolvedValue(new Response("{}", { status: 200 }));
    vi.stubGlobal("fetch", fetcher);
    const { connectSession } = await import("./client");
    await Promise.all([connectSession(), connectSession()]);
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(fetcher).toHaveBeenCalledWith(
        "/install/api/session",
        expect.objectContaining({
            credentials: "same-origin",
            cache: "no-store",
            method: "POST",
            body: '{"token":"private-session"}',
        }),
    );
    expect(window.location.hash).toBe("");
});

it("keeps an unexchanged token available after a connection failure", async () => {
    vi.resetModules();
    window.history.replaceState(null, "", "/install/#token=retry-session");
    vi.stubGlobal(
        "fetch",
        vi.fn().mockRejectedValue(new Error("network unavailable")),
    );
    const { connectSession } = await import("./client");
    await expect(connectSession()).rejects.toThrow("network unavailable");
    expect(window.location.hash).toBe("#token=retry-session");
});

it("reconnects through the existing cookie without sending a token in a query or storage", async () => {
    vi.resetModules();
    window.history.replaceState(null, "", "/install/");
    const fetcher = vi
        .fn()
        .mockResolvedValue(new Response('{"revision":7}', { status: 200 }));
    vi.stubGlobal("fetch", fetcher);
    const { connectSession, readState } = await import("./client");
    await connectSession();
    expect(fetcher).not.toHaveBeenCalled();
    expect(await readState()).toEqual({ revision: 7 });
    expect(fetcher).toHaveBeenCalledWith(
        "/install/api/state",
        expect.objectContaining({ method: "GET", credentials: "same-origin" }),
    );
});

it("preserves actionable API errors", async () => {
    vi.resetModules();
    vi.stubGlobal(
        "fetch",
        vi
            .fn()
            .mockResolvedValue(
                new Response('{"error":"Configuration changed on disk."}', {
                    status: 409,
                }),
            ),
    );
    const { request } = await import("./client");
    await expect(
        request("apply", { revision: 1, planID: "old" }),
    ).rejects.toMatchObject({
        status: 409,
        message: "Configuration changed on disk.",
    });
});
