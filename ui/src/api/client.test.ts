import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";

const response = (body: unknown, status = 200) =>
    new Response(status === 204 ? null : JSON.stringify(body), {
        status,
        headers: { "Content-Type": "application/json" },
    });
let fetchMock: ReturnType<typeof vi.fn>;
let client: typeof import("./client");
beforeEach(async () => {
    vi.resetModules();
    vi.stubGlobal("BroadcastChannel", undefined);
    Object.defineProperty(navigator, "locks", {
        configurable: true,
        value: {
            request: async (_: string, work: () => Promise<void>) => work(),
        },
    });
    fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    client = await import("./client");
});
afterEach(() => {
    vi.unstubAllGlobals();
});
async function loggedIn() {
    fetchMock.mockResolvedValueOnce(
        response({ access_token: "web-secret", expires_in: 600 }),
    );
    await client.login("admin", "password");
    fetchMock.mockClear();
}

describe("credential and mutation behavior", () => {
    it("requires a new login after losing a refresh response", async () => {
        const signedOut = vi.fn();
        const stop = client.onLogout(signedOut);
        fetchMock.mockRejectedValueOnce(new TypeError("Connection lost"));
        await expect(client.refresh()).rejects.toMatchObject({
            problem: { code: "refresh_response_lost" },
        });
        expect(signedOut).toHaveBeenCalledTimes(1);
        expect(fetchMock).toHaveBeenCalledTimes(1);
        stop();
    });
    it("requires a new login when rotation returns an incomplete credential response", async () => {
        fetchMock.mockResolvedValueOnce(response({ expires_in: 600 }));
        await expect(client.refresh()).rejects.toMatchObject({
            problem: { code: "refresh_response_lost" },
        });
        expect(fetchMock).toHaveBeenCalledTimes(1);
    });
    it("serializes concurrent in-tab refreshes without browser persistence", async () => {
        const setItem = vi.spyOn(Storage.prototype, "setItem");
        fetchMock.mockImplementation(async () => {
            await new Promise((resolve) => setTimeout(resolve, 5));
            return response({ access_token: "new-secret", expires_in: 600 });
        });
        await Promise.all(Array.from({ length: 8 }, () => client.refresh()));
        expect(fetchMock).toHaveBeenCalledTimes(1);
        expect(setItem).not.toHaveBeenCalled();
    });
    it("replays a mutation once only after a proven pre-handler expired-token rejection", async () => {
        await loggedIn();
        fetchMock.mockResolvedValueOnce(
            response(
                {
                    code: "access_token_expired",
                    rejected_before_execution: true,
                },
                401,
            ),
        );
        fetchMock.mockResolvedValueOnce(
            response({ access_token: "new-secret", expires_in: 600 }),
        );
        fetchMock.mockResolvedValueOnce(response({}, 204));
        await client.request("http://localhost/api/v1/test", "POST", {
            action: "start",
        });
        expect(fetchMock).toHaveBeenCalledTimes(3);
        const first = fetchMock.mock.calls[0][0] as Request;
        const last = fetchMock.mock.calls[2][0] as Request;
        expect(first.headers.get("Authorization")).toBe("Bearer web-secret");
        expect(last.headers.get("Authorization")).toBe("Bearer new-secret");
        expect(first.credentials).toBe("omit");
        expect(await first.json()).toEqual(await last.json());
    });
    it.each([401, 403, 409, 422, 500, 503])(
        "does not refresh or retry a mutation on an unproven %s",
        async (status) => {
            await loggedIn();
            fetchMock.mockResolvedValueOnce(
                response({ detail: "Rejected" }, status),
            );
            await expect(
                client.request("http://localhost/api/v1/test", "POST", {}),
            ).rejects.toMatchObject({ status });
            expect(fetchMock).toHaveBeenCalledTimes(1);
        },
    );
    it("retains partial-provisioning details and never retries a disconnect", async () => {
        await loggedIn();
        fetchMock.mockRejectedValueOnce(new TypeError("Network error"));
        await expect(
            client.request("http://localhost/api/v1/test", "POST", {}),
        ).rejects.toMatchObject({ status: 0, uncertain: true });
        expect(fetchMock).toHaveBeenCalledTimes(1);
        fetchMock.mockResolvedValueOnce(
            response(
                {
                    detail: "Partial provisioning",
                    provisioning: {
                        stage: "created-event",
                        vm: "test",
                        vm_created: true,
                        volumes: ["opaque-volume"],
                        outcome_uncertain: true,
                    },
                },
                500,
            ),
        );
        await expect(
            client.request("http://localhost/api/v1/test", "POST", {}),
        ).rejects.toMatchObject({
            uncertain: true,
            problem: { provisioning: { stage: "created-event" } },
        });
    });
    it("treats a temporary refresh failure as a service error and a reused cookie as login required", async () => {
        const logout = vi.fn();
        const unsubscribe = client.onLogout(logout);
        fetchMock.mockResolvedValueOnce(
            response({ code: "auth_unavailable" }, 503),
        );
        await expect(client.refresh()).rejects.toMatchObject({ status: 503 });
        expect(logout).not.toHaveBeenCalled();
        fetchMock.mockResolvedValueOnce(
            response({ code: "invalid_credential" }, 401),
        );
        await expect(client.refresh()).rejects.toMatchObject({ status: 401 });
        expect(logout).toHaveBeenCalledTimes(1);
        unsubscribe();
    });
});
