export type RequestContext = Readonly<{
    namespace?: string;
    principal?: string;
}>;
export type NamespaceSelection =
    | { kind: "missing" }
    | { kind: "invalid" }
    | { kind: "selected"; id: string };

export function namespaceSelection(search: string): NamespaceSelection {
    const values = new URLSearchParams(search).getAll("namespace");
    if (!values.length) return { kind: "missing" };
    if (
        values.length !== 1 ||
        !/^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$/.test(values[0])
    )
        return { kind: "invalid" };
    return { kind: "selected", id: values[0] };
}

export function workloadAPI(path: string): boolean {
    const pathname = new URL(path, location.href).pathname;
    return (
        /^\/api\/v1\/(clouds|keys|cloud-init-templates|tasks)(\/|$)/.test(
            pathname,
        ) || /^\/api\/v1\/namespaces\//.test(pathname)
    );
}

export function workloadPage(path: string): boolean {
    return /^\/(machines|volumes|networks|keys|cloud-init-templates|tasks)(\/|$)/.test(
        path,
    );
}

export function namespaceURL(to: string, namespace?: string): string {
    if (!to.startsWith("/") || to.startsWith("//")) return to;
    const url = new URL(to, location.origin);
    if (!workloadPage(url.pathname)) return to;
    if (!url.searchParams.has("namespace") && namespace)
        url.searchParams.set("namespace", namespace);
    return url.pathname + url.search + url.hash;
}
