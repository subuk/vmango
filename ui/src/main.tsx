import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createBrowserRouter, RouterProvider, useLocation } from "react-router";
import { Appearance } from "./app/theme";
import { Auth } from "./app/Auth";
import { transientReadRetry } from "./api/client";
import { App } from "./app/App";
import "./style.css";

const queries = new QueryClient({
    defaultOptions: {
        queries: {
            retry: transientReadRetry,
            staleTime: 5000,
            refetchInterval: 15000,
            refetchIntervalInBackground: false,
        },
        mutations: { retry: false },
    },
});
function Root() {
    const standalone = useLocation().pathname.startsWith("/console/");
    return standalone ? (
        <App standalone />
    ) : (
        <Auth>
            <App />
        </Auth>
    );
}
const router = createBrowserRouter([{ path: "*", element: <Root /> }]);
createRoot(document.getElementById("root")!).render(
    <StrictMode>
        <Appearance>
            <QueryClientProvider client={queries}>
                <RouterProvider router={router} />
            </QueryClientProvider>
        </Appearance>
    </StrictMode>,
);
