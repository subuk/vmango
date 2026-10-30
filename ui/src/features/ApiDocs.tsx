import { useEffect, useRef, useState } from "react";
import { Box, Button, Link, Stack, Typography } from "@mui/material";
import { ErrorMessage, PageTitle } from "../components/common";
import { authenticatedFetch, authorization } from "../api/client";

type Swagger = (options: object) => { destroy?: () => void };
declare global {
    interface Window {
        SwaggerUIBundle?: Swagger;
    }
}
let load: Promise<void> | undefined;
function loadSwagger() {
    if (!load)
        load = new Promise((resolve, reject) => {
            const style = document.createElement("link");
            style.rel = "stylesheet";
            style.href = "/vendor/swagger-ui-5.34.0/swagger-ui.css";
            document.head.append(style);
            const script = document.createElement("script");
            script.src = "/vendor/swagger-ui-5.34.0/swagger-ui-bundle.js";
            script.onload = () => resolve();
            script.onerror = () => {
                load = undefined;
                reject(
                    new Error("API documentation assets could not be loaded."),
                );
            };
            document.head.append(script);
        });
    return load;
}
export function ApiDocs() {
    const root = useRef<HTMLDivElement>(null);
    const [error, setError] = useState<unknown>();
    const [attempt, setAttempt] = useState(0);
    useEffect(() => {
        let active = true;
        let swagger: ReturnType<Swagger>;
        void (async () => {
            try {
                await loadSwagger();
                const response = await authenticatedFetch("/api/openapi.json");
                if (!response.ok)
                    throw new Error(
                        "Could not load authenticated API specification.",
                    );
                const spec = await response.json();
                if (!active) return;
                swagger = window.SwaggerUIBundle!({
                    spec,
                    domNode: root.current,
                    validatorUrl: null,
                    persistAuthorization: false,
                    withCredentials: false,
                    requestInterceptor: async (req: {
                        url: string;
                        headers: Record<string, string>;
                        credentials?: string;
                    }) => {
                        const target = new URL(req.url, location.origin);
                        if (target.origin !== location.origin)
                            throw new Error(
                                "Only this installation’s API is available.",
                            );
                        // Swagger requests use the same in-memory access credential as resource views.
                        if (
                            !Object.keys(req.headers).some(
                                (key) => key.toLowerCase() === "authorization",
                            )
                        )
                            req.headers.Authorization = await authorization();
                        req.credentials = "omit";
                        return req;
                    },
                });
            } catch (e) {
                if (active) setError(e);
            }
        })();
        return () => {
            active = false;
            swagger?.destroy?.();
            root.current?.replaceChildren();
        };
    }, [attempt]);
    return (
        <>
            <PageTitle title="API documentation" />
            <Stack direction="row" spacing={2} sx={{ mb: 3 }}>
                <Typography color="text.secondary">
                    Authenticated with your browser session. Personal tokens use
                    Authorization: Bearer for CLI/CI.
                </Typography>
                <Link
                    href="/api/console-protocol"
                    onClick={async (e) => {
                        e.preventDefault();
                        const response = await authenticatedFetch(
                            "/api/console-protocol",
                        );
                        const text = await response.text();
                        const file = new Blob([text], { type: "text/plain" });
                        const url = URL.createObjectURL(file);
                        const a = document.createElement("a");
                        a.href = url;
                        a.download = "vmango-console-protocol.txt";
                        a.click();
                        setTimeout(() => URL.revokeObjectURL(url), 1000);
                    }}
                >
                    Console protocol
                </Link>
            </Stack>
            <ErrorMessage error={error} />
            {error && (
                <Button
                    onClick={() => {
                        setError(undefined);
                        setAttempt((n) => n + 1);
                    }}
                >
                    Retry
                </Button>
            )}
            <Box className="api-docs" ref={root} />
        </>
    );
}
