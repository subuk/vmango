import createClient from "openapi-fetch";
import type { components, paths } from "./schema";

export type Problem = {
    status?: number;
    title?: string;
    detail?: string;
    code?: string;
    task_id?: string;
    task_url?: string;
    rejected_before_execution?: boolean;
    errors?: { location?: string; message?: string }[];
};
export class APIError extends Error {
    recover?: () => Promise<unknown>;
    constructor(
        public status: number,
        public problem: Problem = {},
    ) {
        super(problem.detail || problem.title || "Request failed.");
    }
    get uncertain() {
        return this.status === 0 || this.status >= 500;
    }
}

type Credentials = { access_token: string; expires_in: number };
let token: string | undefined;
let identityGeneration = 0;
let expiresAt = 0;
let refreshFlight: Promise<void> | undefined;
let refreshGeneration = -1;
function requireIdentity(generation: number) {
    if (generation !== identityGeneration)
        throw new DOMException("Identity changed", "AbortError");
}
const events = new EventTarget();
const channel =
    typeof BroadcastChannel !== "undefined"
        ? new BroadcastChannel("vmango.auth")
        : undefined;
channel?.addEventListener("message", (event) => {
    if (event.data?.type === "logout") {
        identityGeneration++;
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
    const generation = identityGeneration;
    if (refreshFlight && refreshGeneration === generation) return refreshFlight;
    const rotate = async () => {
        requireIdentity(generation);
        // Tabs serialize cookie rotation. Each tab obtains its own in-memory JWT.
        // A lost rotation response deliberately requires login; no token grace window.
        try {
            const credentials = await publicRequest<Credentials>(
                "/api/v1/auth/refresh",
                {},
            );
            requireIdentity(generation);
            accept(credentials);
        } catch (error) {
            requireIdentity(generation);
            if (error instanceof APIError && error.status === 0) {
                identityGeneration++;
                token = undefined;
                expiresAt = 0;
                events.dispatchEvent(new Event("logout"));
                throw new APIError(0, {
                    code: "refresh_response_lost",
                    detail: "Session refresh response was lost. Sign in again.",
                });
            }
            if (error instanceof APIError && error.status === 401) {
                identityGeneration++;
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
    const flight = coordinated().finally(() => {
        if (refreshFlight === flight) refreshFlight = undefined;
    });
    refreshGeneration = generation;
    refreshFlight = flight;
    return flight;
}
export async function login(username: string, password: string) {
    const generation = ++identityGeneration;
    const credentials = await publicRequest<Credentials>("/api/v1/auth/login", {
        username,
        password,
    });
    requireIdentity(generation);
    accept(credentials);
}
export async function authorization(): Promise<string> {
    const generation = identityGeneration;
    if (!token || Date.now() > expiresAt - 5000) await refresh();
    requireIdentity(generation);
    return `Bearer ${token}`;
}
export async function logout() {
    await publicRequest("/api/v1/auth/logout", {});
    identityGeneration++;
    token = undefined;
    expiresAt = 0;
    channel?.postMessage({ type: "logout" });
    events.dispatchEvent(new Event("logout"));
}

export async function authenticatedFetch(
    input: RequestInfo | URL,
    init?: RequestInit,
): Promise<Response> {
    const generation = identityGeneration;
    if (!token || Date.now() > expiresAt - 5000) await refresh();
    requireIdentity(generation);
    const request = new Request(input, init);
    const send = async () => {
        requireIdentity(generation);
        const headers = new Headers(request.headers);
        headers.set("Authorization", `Bearer ${token}`);
        try {
            const response = await fetch(
                new Request(request.clone(), { headers, credentials: "omit" }),
            );
            requireIdentity(generation);
            return response;
        } catch (error) {
            requireIdentity(generation);
            if (error instanceof DOMException && error.name === "AbortError")
                throw error;
            throw new APIError(0, {
                detail: "Connection lost. Refresh resource state before retrying; cancellation does not roll back an operation.",
            });
        }
    };
    let response = await send();
    if (response.status === 401) {
        const error = await responseProblem(response.clone());
        requireIdentity(generation);
        // Only the authentication guard proves no mutation handler ran.
        if (
            error.problem.code === "access_token_expired" &&
            error.problem.rejected_before_execution
        ) {
            await refresh();
            requireIdentity(generation);
            response = await send();
        }
    }
    return response;
}

// Generated OpenAPI request/response types remain the contract for resource hooks.
export const api = createClient<paths>({ fetch: authenticatedFetch });
export type TaskReference = components["schemas"]["ApiTaskReference"];
export function onTaskAccepted(fn: (task: TaskReference) => void) {
    const listener = (event: Event) =>
        fn((event as CustomEvent<TaskReference>).detail);
    events.addEventListener("task-accepted", listener);
    return () => events.removeEventListener("task-accepted", listener);
}
const submissionKeys = new Map<string, string>();
async function submissionKey(path: string, method: string, body: unknown) {
    const command = JSON.stringify([path, method, body]);
    const digest = await crypto.subtle.digest(
        "SHA-256",
        new TextEncoder().encode(command),
    );
    const name =
        "vmango.submission." +
        Array.from(new Uint8Array(digest), (b) =>
            b.toString(16).padStart(2, "0"),
        ).join("");
    let key = submissionKeys.get(name);
    try {
        key ||= sessionStorage.getItem(name) || undefined;
    } catch {
        /* Memory retains recovery when session storage is unavailable. */
    }
    if (!key) key = crypto.randomUUID();
    submissionKeys.set(name, key);
    try {
        sessionStorage.setItem(name, key);
    } catch {
        /* See above. */
    }
    return {
        key,
        clear: () => {
            submissionKeys.delete(name);
            try {
                sessionStorage.removeItem(name);
            } catch {
                /* See above. */
            }
        },
    };
}
export async function request<T>(
    path: string,
    method = "GET",
    body?: unknown,
    signal?: AbortSignal,
): Promise<T> {
    const generation = identityGeneration;
    const infrastructure =
        method !== "GET" &&
        !new URL(path, location.href).pathname.includes("/consoles/") &&
        /^\/api\/v1\/clouds\/[^/]+\/(?:machines|volumes)(?:\/|$)/.test(
            new URL(path, location.href).pathname,
        );
    const submission = infrastructure
        ? await submissionKey(path, method, body)
        : undefined;
    requireIdentity(generation);
    try {
        const headers = new Headers(
            body === undefined ? {} : { "Content-Type": "application/json" },
        );
        if (submission) headers.set("Idempotency-Key", submission.key);
        const response = await authenticatedFetch(path, {
            signal,
            method,
            headers,
            body: body === undefined ? undefined : JSON.stringify(body),
        });
        if (!response.ok) {
            const error = await responseProblem(response);
            requireIdentity(generation);
            if (response.status < 500) submission?.clear();
            throw error;
        }
        requireIdentity(generation);
        if (
            response.status === 204 ||
            response.headers.get("Content-Length") === "0"
        )
            return undefined as T;
        let result: T;
        try {
            result = await response.json();
            requireIdentity(generation);
        } catch (error) {
            if (error instanceof DOMException && error.name === "AbortError")
                throw error;
            throw new APIError(0, { detail: "Response could not be read." });
        }
        if (response.status === 202 && submission) {
            const accepted = result as TaskReference;
            if (!accepted?.task?.id || !accepted.status_url)
                throw new APIError(0, {
                    detail: "Task acceptance response could not be read.",
                });
            submission.clear();
            events.dispatchEvent(
                new CustomEvent("task-accepted", { detail: accepted }),
            );
        }
        return result;
    } catch (error) {
        if (submission && error instanceof APIError && error.uncertain) {
            const uncertain = new APIError(error.status, {
                ...error.problem,
                code: "task_submission_uncertain",
                detail: "Submission status is unknown. Recover using the same command and idempotency key to find the accepted task safely.",
            });
            uncertain.recover = () => request(path, method, body);
            throw uncertain;
        }
        throw error;
    }
}
export const transientReadRetry = (failures: number, error: Error) =>
    failures < 2 &&
    error instanceof APIError &&
    (error.status === 0 ||
        error.status === 502 ||
        error.status === 503 ||
        error.status === 504);
