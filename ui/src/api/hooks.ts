import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, APIError, request } from "./client";
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
export type CreateVM = components["schemas"]["ApiCreateVM"];
export type PatchVM = components["schemas"]["ApiPatchVM"];
export type Disk = components["schemas"]["ApiDisk"];
export type Interface = components["schemas"]["ApiInterface"];
export const esc = encodeURIComponent;
export const vmPath = (node: string, id: string) =>
    `/api/v1/nodes/${esc(node)}/vms/${esc(id)}`;
export const volumePath = (node: string, id: string) =>
    `/api/v1/nodes/${esc(node)}/volumes/${esc(id)}`;
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
async function unwrap<T>(result: {
    data?: T;
    error?: unknown;
    response: Response;
}): Promise<T> {
    if (result.error || !result.response.ok)
        throw new APIError(result.response.status, result.error as object);
    return result.data as T;
}
export function useNodes(enabled = true) {
    return useQuery({
        queryKey: ["/api/v1/nodes"],
        enabled,
        queryFn: async () => unwrap(await api.GET("/api/v1/nodes")),
    });
}
export function useMachines() {
    return useQuery({
        queryKey: ["/api/v1/vms"],
        queryFn: async () => unwrap(await api.GET("/api/v1/vms")),
    });
}
export function useVolumes() {
    return useQuery({
        queryKey: ["/api/v1/volumes"],
        queryFn: async () => unwrap(await api.GET("/api/v1/volumes")),
    });
}
export function useNetworks() {
    return useQuery({
        queryKey: ["/api/v1/networks"],
        queryFn: async () => unwrap(await api.GET("/api/v1/networks")),
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
