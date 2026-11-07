import type { InstallState } from "./types";

export class InstallError extends Error {
    constructor(
        message: string,
        public status: number,
    ) {
        super(message);
    }
}

export async function request<T>(path: string, body?: unknown): Promise<T> {
    const response = await fetch(`/install/api/${path}`, {
        method: body === undefined ? "GET" : "POST",
        credentials: "same-origin",
        cache: "no-store",
        headers:
            body === undefined
                ? undefined
                : { "Content-Type": "application/json" },
        body: body === undefined ? undefined : JSON.stringify(body),
    });
    const data = await response.json().catch(() => ({}));
    if (!response.ok) {
        throw new InstallError(
            typeof data.error === "string"
                ? data.error
                : `Request failed (${response.status}).`,
            response.status,
        );
    }
    return data as T;
}

let connection: Promise<void> | undefined;
export function connectSession(): Promise<void> {
    if (connection) return connection;
    connection = (async () => {
        const token = new URLSearchParams(window.location.hash.slice(1)).get(
            "token",
        );
        if (token) {
            await request("session", { token });
            window.history.replaceState(
                null,
                "",
                window.location.pathname + window.location.search,
            );
        }
    })().catch((error) => {
        connection = undefined;
        throw error;
    });
    return connection;
}

export const readState = () => request<InstallState>("state");
