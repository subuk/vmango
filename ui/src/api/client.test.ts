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
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
});
function deferred<T>() {
    let resolve!: (value: T) => void;
    const promise = new Promise<T>((done) => {
        resolve = done;
    });
    return { promise, resolve };
}
async function switchIdentity() {
    fetchMock.mockResolvedValueOnce(response({}, 204));
    await client.logout();
    fetchMock.mockResolvedValueOnce(
        response({ access_token: "different-user", expires_in: 600 }),
    );
    await client.login("other", "password");
}
async function loggedIn() {
    fetchMock.mockResolvedValueOnce(
        response({ access_token: "web-secret", expires_in: 600 }),
    );
    await client.login("admin", "password");
    fetchMock.mockClear();
}

describe("credential and mutation behavior", () => {
    it("keeps console link issuance synchronous without task submission keys", async () => {
        await loggedIn();
        fetchMock.mockResolvedValueOnce(
            response(
                {
                    url: "/console/local/v/vnc#test",
                    expires_at: "2026-10-30T16:00:00Z",
                },
                201,
            ),
        );
        const result = await client.request<{ url: string }>(
            "http://localhost/api/v1/clouds/local/machines/v/consoles/vnc/links",
            "POST",
            {},
        );
        expect(result.url).toContain("/console/");
        expect(
            (fetchMock.mock.calls[0][0] as Request).headers.get(
                "Idempotency-Key",
            ),
        ).toBeNull();
    });
    it.each([
        "/api/v1/clouds/local/machines",
        "/api/v1/clouds/local/volumes",
        "/api/v1/clouds/local/machines/v/instance-type",
        "/api/v1/clouds/local/volumes/bg.dm9s/clone",
    ])(
        "recovers an uncertain infrastructure submission at %s with the same key",
        async (route) => {
            await loggedIn();
            const accepted = vi.fn();
            const stop = client.onTaskAccepted(accepted);
            const path = "http://localhost" + route;
            const body = {
                id: "pending",
                cloud_init: { user_data: "private-bootstrap-secret" },
            };
            fetchMock.mockRejectedValueOnce(new TypeError("Response lost"));
            let failure: InstanceType<typeof client.APIError> | undefined;
            try {
                await client.request(path, "POST", body);
            } catch (error) {
                failure = error as typeof failure;
            }
            expect(failure?.problem.code).toBe("task_submission_uncertain");
            expect(fetchMock).toHaveBeenCalledTimes(1);
            const first = fetchMock.mock.calls[0][0] as Request;
            const key = first.headers.get("Idempotency-Key");
            expect(key).toBeTruthy();
            const stored = Array.from(
                { length: sessionStorage.length },
                (_, i) => {
                    const name = sessionStorage.key(i)!;
                    return name + sessionStorage.getItem(name);
                },
            ).join(" ");
            expect(stored).not.toContain("private-bootstrap-secret");
            expect(stored).not.toContain("cloud_init");
            fetchMock.mockResolvedValueOnce(
                response(
                    {
                        task: { id: "saved-task", state: "running" },
                        status_url: "/api/v1/tasks/saved-task",
                    },
                    202,
                ),
            );
            await failure?.recover?.();
            const recovered = fetchMock.mock.calls[1][0] as Request;
            expect(recovered.headers.get("Idempotency-Key")).toBe(key);
            expect(await recovered.json()).toEqual(body);
            expect(accepted).toHaveBeenCalledWith(
                expect.objectContaining({
                    task: expect.objectContaining({ id: "saved-task" }),
                }),
            );
            stop();
        },
    );
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
    it("retains service error details and never retries a disconnect", async () => {
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
                },
                500,
            ),
        );
        await expect(
            client.request("http://localhost/api/v1/test", "POST", {}),
        ).rejects.toMatchObject({
            uncertain: true,
            problem: { detail: "Partial provisioning" },
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

it("discards a private response that completes after logout and another login", async () => {
    await loggedIn();
    let finish!: (value: Response) => void;
    fetchMock.mockImplementationOnce(
        () =>
            new Promise<Response>((resolve) => {
                finish = resolve;
            }),
    );
    const read = client.request(
        "http://localhost/api/v1/administration/configuration",
    );
    const checked = expect(read).rejects.toMatchObject({ name: "AbortError" });
    fetchMock.mockResolvedValueOnce(response({}, 204));
    await client.logout();
    fetchMock.mockResolvedValueOnce(
        response({ access_token: "different-user", expires_in: 600 }),
    );
    await client.login("other", "password");
    finish(response({ snapshot_id: "old-private-data" }));
    await checked;
});

it("never sends a previous identity's mutation after delayed idempotency hashing", async () => {
    await loggedIn();
    const digest = deferred<ArrayBuffer>();
    vi.spyOn(crypto.subtle, "digest").mockReturnValueOnce(digest.promise);
    const mutation = client.request(
        "http://localhost/api/v1/clouds/local/volumes",
        "POST",
        { name: "old-command" },
    );
    const checked = expect(mutation).rejects.toMatchObject({
        name: "AbortError",
    });
    await switchIdentity();
    digest.resolve(new Uint8Array(32).buffer);
    await checked;
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(await client.authorization()).toBe("Bearer different-user");
});

it.each([200, 401])(
    "does not send an old request or clear the new identity after delayed refresh (%s)",
    async (status) => {
        const rotation = deferred<Response>();
        fetchMock.mockReturnValueOnce(rotation.promise);
        const mutation = client.request(
            "http://localhost/api/v1/test",
            "POST",
            { action: "old-command" },
        );
        const checked = expect(mutation).rejects.toMatchObject({
            name: "AbortError",
        });
        await switchIdentity();
        rotation.resolve(
            status === 200
                ? response({ access_token: "old-rotation", expires_in: 600 })
                : response({ code: "invalid_credential" }, 401),
        );
        await checked;
        expect(fetchMock).toHaveBeenCalledTimes(3);
        expect(await client.authorization()).toBe("Bearer different-user");
    },
);

it("does not refresh an old identity after waiting for another tab's rotation lock", async () => {
    const released = deferred<void>();
    Object.defineProperty(navigator, "locks", {
        configurable: true,
        value: {
            request: async (_: string, work: () => Promise<void>) => {
                await released.promise;
                return work();
            },
        },
    });
    const mutation = client.request("http://localhost/api/v1/test", "POST", {
        action: "old-command",
    });
    const checked = expect(mutation).rejects.toMatchObject({
        name: "AbortError",
    });
    await switchIdentity();
    released.resolve();
    await checked;
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(await client.authorization()).toBe("Bearer different-user");
});

it("never replays an old mutation after delayed expired-token error parsing", async () => {
    await loggedIn();
    const expired = deferred<unknown>();
    const parsing = deferred<void>();
    const rejected = response({}, 401);
    vi.spyOn(rejected, "clone").mockReturnValue({
        status: 401,
        json: () => {
            parsing.resolve();
            return expired.promise;
        },
    } as Response);
    fetchMock.mockResolvedValueOnce(rejected);
    const mutation = client.request("http://localhost/api/v1/test", "POST", {
        action: "old-command",
    });
    const checked = expect(mutation).rejects.toMatchObject({
        name: "AbortError",
    });
    await parsing.promise;
    await switchIdentity();
    expired.resolve({
        code: "access_token_expired",
        rejected_before_execution: true,
    });
    await checked;
    expect(fetchMock).toHaveBeenCalledTimes(3);
    expect(await client.authorization()).toBe("Bearer different-user");
});

it.each([200, 403])(
    "discards an old private response body parsed after identity change (%s)",
    async (status) => {
        await loggedIn();
        const body = deferred<unknown>();
        const parsing = deferred<void>();
        const pending = response({}, status);
        vi.spyOn(pending, "json").mockImplementation(() => {
            parsing.resolve();
            return body.promise;
        });
        fetchMock.mockResolvedValueOnce(pending);
        const read = client.request(
            "http://localhost/api/v1/administration/configuration",
        );
        const checked = expect(read).rejects.toMatchObject({
            name: "AbortError",
        });
        await parsing.promise;
        await switchIdentity();
        body.resolve({
            detail: "old-private-diagnostics",
            snapshot_id: "old-private-data",
        });
        await checked;
    },
);

it("does not accept a late login response over a newer identity", async () => {
    const oldLogin = deferred<Response>();
    fetchMock.mockReturnValueOnce(oldLogin.promise);
    const login = client.login("old-user", "password");
    const checked = expect(login).rejects.toMatchObject({ name: "AbortError" });
    await switchIdentity();
    oldLogin.resolve(response({ access_token: "old-user", expires_in: 600 }));
    await checked;
    expect(await client.authorization()).toBe("Bearer different-user");
});
