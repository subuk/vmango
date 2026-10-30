import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Keep Host/Origin from the browser: Go validates the configured external URL.
const proxy = {
    target: process.env.VMANGO_BACKEND || "http://127.0.0.1:8080",
    changeOrigin: false,
    ws: true,
};
export default defineConfig({
    plugins: [react()],
    server: {
        proxy: {
            "/api": proxy,
            "/login-oidc": proxy,
            "/login-oidc-callback": proxy,
            "/console": {
                ...proxy,
                bypass: (req) => (req.headers.upgrade ? undefined : req.url),
            },
        },
    },
    build: { outDir: "dist", emptyOutDir: true },
});
