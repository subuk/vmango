import createClient from "openapi-fetch";
import type { paths } from "./schema";

export type Problem = {
    status?: number;
    title?: string;
    detail?: string;
    code?: string;
    rejected_before_execution?: boolean;
    errors?: { location?: string; message?: string }[];
    provisioning?: {
        stage: string;
        node: string;
        vm: string;
        vm_created: boolean;
        volumes: string[];
        outcome_uncertain: boolean;
    };
};
export class APIError extends Error {
    constructor(
        public status: number,
        public problem: Problem = {},
    ) {
        super(problem.detail || problem.title || "Request failed.");
    }
    get uncertain() {
        return (
            this.status === 0 ||
            this.status >= 500 ||
            !!this.problem.provisioning?.outcome_uncertain
        );
    }
}

type Credentials = { access_token: string; expires_in: number };
let token: string | undefined;
let expiresAt = 0;
let refreshFlight: Promise<void> | undefined;
const events = new EventTarget();
const channel =
    typeof BroadcastChannel !== "undefined"
        ? new BroadcastChannel("vmango.auth")
        : undefined;
channel?.addEventListener("message", (event) => {
    if (event.data?.type === "logout") {
        token = undefined;
        expiresAt = 0;
        events.dispatchEvent(new Event("logout"));
    }
});
export function onLogout(fn: () => void) {
    events.addEventListener("logout", fn);
    return () => events.removeEventListener("logout", fn);
}
function accept(body: Credentials) {
    if (
        !body ||
        typeof body.access_token !== "string" ||
        !body.access_token ||
        !Number.isFinite(body.expires_in) ||
        body.expires_in <= 0
    ) {
        throw new APIError(0, {
            detail: "Authentication response could not be read. Sign in again.",
        });
    }
    token = body.access_token;
    expiresAt = Date.now() + body.expires_in * 1000;
}

async function responseProblem(response: Response): Promise<APIError> {
    let body: Problem;
    try {
        const value = await response.json();
        body =
            value && typeof value === "object"
                ? value
                : { detail: `Request failed (${response.status}).` };
    } catch {
        body = { detail: `Request failed (${response.status}).` };
    }
    return new APIError(response.status, body);
}
export async function publicRequest<T>(
    path: string,
    body?: unknown,
): Promise<T> {
    let response: Response;
    try {
        response = await fetch(path, {
            method: body === undefined ? "GET" : "POST",
            credentials: "same-origin",
            headers:
                body === undefined
                    ? {}
                    : { "Content-Type": "application/json" },
            body: body === undefined ? undefined : JSON.stringify(body),
        });
    } catch {
        throw new APIError(0, {
            detail: "Connection lost. The operation may have completed; refresh resource state before retrying.",
        });
    }
    if (!response.ok) throw await responseProblem(response);
    if (response.status === 204) return undefined as T;
    try {
        return await response.json();
    } catch {
        throw new APIError(0, {
            detail: "Response could not be read. Check the operation state before retrying.",
        });
    }
}

export async function refresh(): Promise<void> {
    if (refreshFlight) return refreshFlight;
    const rotate = async () => {
        // Tabs serialize cookie rotation. Each tab obtains its own in-memory JWT.
        // A lost rotation response deliberately requires login; no token grace window.
        try {
            accept(
                await publicRequest<Credentials>("/api/v1/auth/refresh", {}),
            );
        } catch (error) {
            if (error instanceof APIError && error.status === 0) {
                token = undefined;
                expiresAt = 0;
                events.dispatchEvent(new Event("logout"));
                throw new APIError(0, {
                    code: "refresh_response_lost",
                    detail: "Session refresh response was lost. Sign in again.",
                });
            }
            if (error instanceof APIError && error.status === 401) {
                token = undefined;
                expiresAt = 0;
                events.dispatchEvent(new Event("logout"));
            }
            throw error;
        }
    };
    const coordinated = async () => {
        if (navigator.locks)
            await navigator.locks.request("vmango.refresh", rotate);
        else await rotate();
    };
    refreshFlight = coordinated().finally(() => {
        refreshFlight = undefined;
    });
    return refreshFlight;
}
export async function login(username: string, password: string) {
    accept(
        await publicRequest<Credentials>("/api/v1/auth/login", {
            username,
            password,
        }),
    );
}
export async function authorization(): Promise<string> {
    if (!token || Date.now() > expiresAt - 5000) await refresh();
    return `Bearer ${token}`;
}
export async function logout() {
    await publicRequest("/api/v1/auth/logout", {});
    token = undefined;
    expiresAt = 0;
    channel?.postMessage({ type: "logout" });
    events.dispatchEvent(new Event("logout"));
}

export async function authenticatedFetch(
    input: RequestInfo | URL,
    init?: RequestInit,
): Promise<Response> {
    if (!token || Date.now() > expiresAt - 5000) await refresh();
    const request = new Request(input, init);
    const send = async () => {
        const headers = new Headers(request.headers);
        headers.set("Authorization", `Bearer ${token}`);
        try {
            return await fetch(
                new Request(request.clone(), { headers, credentials: "omit" }),
            );
        } catch {
            throw new APIError(0, {
                detail: "Connection lost. Refresh resource state before retrying; cancellation does not roll back an operation.",
            });
        }
    };
    let response = await send();
    if (response.status === 401) {
        const error = await responseProblem(response.clone());
        // Only the authentication guard proves no mutation handler ran.
        if (
            error.problem.code === "access_token_expired" &&
            error.problem.rejected_before_execution
        ) {
            await refresh();
            response = await send();
        }
    }
    return response;
}

// Generated OpenAPI request/response types remain the contract for resource hooks.
export const api = createClient<paths>({ fetch: authenticatedFetch });
export async function request<T>(
    path: string,
    method = "GET",
    body?: unknown,
): Promise<T> {
    const response = await authenticatedFetch(path, {
        method,
        headers:
            body === undefined ? {} : { "Content-Type": "application/json" },
        body: body === undefined ? undefined : JSON.stringify(body),
    });
    if (!response.ok) throw await responseProblem(response);
    if (
        response.status === 204 ||
        response.headers.get("Content-Length") === "0"
    )
        return undefined as T;
    try {
        return await response.json();
    } catch {
        throw new APIError(0, {
            detail: "Response could not be read. Refresh resource state before retrying.",
        });
    }
}
export const transientReadRetry = (failures: number, error: Error) =>
    failures < 2 &&
    error instanceof APIError &&
    (error.status === 0 ||
        error.status === 502 ||
        error.status === 503 ||
        error.status === 504);
