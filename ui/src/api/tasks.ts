import { useQuery } from "@tanstack/react-query";
import { request } from "./client";
import type { components } from "./schema";

export type Task = components["schemas"]["Task"];
export type TaskPage = components["schemas"]["Page"];
export type ResourceRef = components["schemas"]["ResourceRef"];
export const taskURL = (id: string) => `/tasks/${encodeURIComponent(id)}/`;
export const terminal = (task: Task) =>
    !["queued", "running"].includes(task.state);
export const taskLabel = (kind: string) => {
    const [resource, action] = kind.split(".");
    const labels: Record<string, string> = {
        create: "Create",
        adopt: "Adopt",
        "change-type": "Change instance type for",
        "create-disk": "Create and attach disk to",
        update: "Update",
        delete: "Delete",
        start: "Start",
        reboot: "Reboot",
        shutdown: "Shutdown",
        poweroff: "Hard power-off",
        clone: "Clone",
        resize: "Resize",
        "attach-disk": "Attach disk to",
        "detach-disk": "Detach disk from",
        "attach-interface": "Attach interface to",
        "detach-interface": "Detach interface from",
    };
    return `${labels[action] || action || kind} ${resource === "machine" ? "machine" : resource === "volume" ? "volume" : resource || ""}`;
};
const stageNames: Record<string, string> = {
    "cloud-inventory": "Collect cloud inventory",
    "schedule-machine": "Choose machine placement",
    "admit-instance-type": "Check instance type capacity",
    "change-instance-type": "Apply instance type",
    "adopt-machine": "Adopt machine",
    "prepare-image": "Check image readiness",
    "wait-image": "Wait for shared image preparation",
    "inspect-image-source": "Inspect image source",
    "create-image-staging": "Prepare image storage",
    "stream-image": "Download and upload image",
    "verify-image": "Verify and publish image",
    "cleanup-image": "Clean up incomplete image",
    "validate-image-creation": "Check creation against verified image",
    "clone-volume": "Clone disk",
    "create-volume": "Create disk",
    "define-vm": "Create machine configuration",
    "generate-config-drive": "Prepare guest initialization",
    "create-config-drive": "Create initialization disk",
    "upload-config-drive": "Upload guest initialization",
    "attach-config-drive": "Attach initialization disk",
    "verify-vm": "Check machine configuration",
    "verify-machine": "Check machine configuration",
    "created-event": "Run creation hooks",
    "start-vm": "Request machine start",
    "start-machine": "Request machine start",
    "poweroff-machine": "Hard power-off machine",
    "reboot-machine": "Reboot machine",
    "shutdown-machine": "Request graceful shutdown",
    "delete-machine": "Delete machine",
    "verify-machine-deleted": "Confirm machine deletion",
    "verify-volume-deleted": "Confirm volume deletion",
    "verify-volume": "Check disk capacity",
    "update-configuration": "Update machine configuration",
};
export const stageLabel = (name: string) =>
    stageNames[name] ||
    name
        .split("-")
        .map((word, index) =>
            index === 0 ? word[0].toUpperCase() + word.slice(1) : word,
        )
        .join(" ");
export function useTaskPage(params: URLSearchParams, enabled = true) {
    const path = `/api/v1/tasks?${params}`;
    return useQuery({
        queryKey: [path],
        queryFn: ({ signal }) =>
            request<TaskPage>(path, "GET", undefined, signal),
        enabled,
        refetchInterval: 2000,
    });
}
export function useTask(id: string) {
    const path = `/api/v1/tasks/${encodeURIComponent(id)}`;
    return useQuery({
        queryKey: [path],
        queryFn: ({ signal }) => request<Task>(path, "GET", undefined, signal),
        refetchInterval: 2000,
    });
}
// Observe every active task, independently of the current history page.
export function useActiveTasks() {
    return useQuery({
        queryKey: ["/api/v1/tasks/active-observer"],
        queryFn: async () => {
            const items: Task[] = [];
            let before: string | undefined;
            do {
                const params = new URLSearchParams({
                    state: "queued,running",
                    limit: "100",
                });
                if (before) params.set("before", before);
                const page = await request<TaskPage>(`/api/v1/tasks?${params}`);
                items.push(...(page.items || []));
                before = page.next;
            } while (before);
            return items;
        },
        refetchInterval: 2000,
    });
}
export function useResourceTasks(resource: ResourceRef) {
    return useTaskPage(
        new URLSearchParams({
            resource_kind: resource.kind,
            resource_scope: resource.scope,
            resource_id: resource.id,
            limit: "100",
        }),
    );
}
export function useResourceBusy(resource?: ResourceRef) {
    const query = useTaskPage(
        new URLSearchParams({
            blocking: "true",
            resource_kind: resource?.kind || "",
            resource_scope: resource?.scope || "",
            resource_id: resource?.id || "",
            limit: "100",
        }),
        !!resource,
    );
    return {
        task: query.data?.items?.[0],
        unavailable: query.isError && !query.data,
    };
}
export function volumeResource(node: string, id: string): ResourceRef {
    let path = id;
    try {
        const parts = id.split(".");
        if (parts.length !== 2 || !parts.every(Boolean))
            throw new Error("Invalid volume address");
        const decode = (value: string) =>
            new TextDecoder("utf-8", { fatal: true }).decode(
                Uint8Array.from(
                    atob(value.replace(/-/g, "+").replace(/_/g, "/")),
                    (c) => c.charCodeAt(0),
                ),
            );
        node = decode(parts[0]);
        path = new TextDecoder().decode(
            Uint8Array.from(
                atob(parts[1].replace(/-/g, "+").replace(/_/g, "/")),
                (c) => c.charCodeAt(0),
            ),
        );
    } catch {
        /* Keep an unmatched ID for an invalid resource. */
    }
    return { kind: "volume", scope: node, id: path };
}
