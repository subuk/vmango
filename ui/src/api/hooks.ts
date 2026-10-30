import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { request } from "./client";
import type { components } from "./schema";

export type VM = components["schemas"]["ApiVM"];
export type Node = components["schemas"]["ApiNode"];
export type Volume = components["schemas"]["ApiVolume"];
export type Network = components["schemas"]["ApiNetwork"];
export type Key = components["schemas"]["ApiKey"];
export type Pool = components["schemas"]["ApiPool"];
export type Image = components["schemas"]["ApiImage"];
export type Capabilities = components["schemas"]["ApiCapabilities"];
export type Token = components["schemas"]["TokenMetadata"];
export type PatchVM = components["schemas"]["ApiPatchVM"];
export type Disk = components["schemas"]["ApiDisk"];
export type Interface = components["schemas"]["ApiInterface"];
export const esc = encodeURIComponent;
export const vmPath = (node: string, id: string, cloud: string) =>
    `/api/v1/clouds/${esc(cloud)}/machines/${esc(node)}/${esc(id)}`;
export const volumePath = (node: string, id: string, cloud: string) =>
    `/api/v1/clouds/${esc(cloud)}/volumes/${esc(node)}/${esc(id)}`;
export const machineURL = (node: string, id: string) =>
    `/machines/${esc(node)}/${esc(id)}/`;
export function useResource<T>(path: string, enabled = true, poll = true) {
    return useQuery({
        queryKey: [path],
        queryFn: () => request<T>(path),
        enabled,
        refetchInterval: poll ? 15000 : false,
    });
}
export function useNodes(enabled = true) {
    return useResource<Node[]>("/api/v1/hosts", enabled);
}
export function useMachine(
    node: string,
    id: string,
    enabled = true,
    poll = true,
) {
    return useQuery<VM>({
        queryKey: ["machine", node, id],
        enabled,
        queryFn: async () => {
            const host = await request<{ cloud: string }>(
                `/api/v1/hosts/${esc(node)}/catalog`,
            );
            return request<VM>(vmPath(node, id, host.cloud));
        },
        refetchInterval: poll ? 15000 : false,
    });
}
export function useMachines(cloud?: string) {
    return useResource<VM[]>(
        `/api/v1/clouds/${esc(cloud || "")}/machines`,
        !!cloud,
    );
}
export function useVolumes() {
    return useQuery({
        queryKey: ["cloud-volumes"],
        queryFn: async () => {
            const clouds = await request<{ id: string }[]>("/api/v1/clouds");
            return (
                await Promise.all(
                    clouds.map((c) =>
                        request<Volume[]>(
                            `/api/v1/clouds/${esc(c.id)}/volumes`,
                        ),
                    ),
                )
            ).flat();
        },
    });
}
export function useNetworks() {
    return useQuery({
        queryKey: ["host-networks"],
        queryFn: async () => {
            const hosts = await request<Node[]>("/api/v1/hosts");
            return (
                await Promise.all(
                    hosts.map((h) =>
                        request<Network[]>(
                            `/api/v1/hosts/${esc(h.id)}/networks`,
                        ),
                    ),
                )
            ).flat();
        },
    });
}
export function useOperation<T>(execute: (input: T) => Promise<unknown>) {
    const queries = useQueryClient();
    return useMutation({
        mutationFn: execute,
        retry: false,
        onSuccess: () => queries.invalidateQueries(),
    });
}
