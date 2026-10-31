import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { request } from "./client";
import type { components } from "./schema";

export type VM = components["schemas"]["ApiVM"];
export type Node = components["schemas"]["ApiNode"];
export type VolumeList = components["schemas"]["ApiVolumeList"];
export type VMDetail = components["schemas"]["ApiVMDetail"];
export type VolumeDetail = components["schemas"]["ApiVolumeDetail"];
export type Volume = components["schemas"]["ApiVolume"];
export type Network = components["schemas"]["ApiNetwork"];
export type Key = components["schemas"]["ApiKey"];
export type Pool = components["schemas"]["ApiPool"];
export type Token = components["schemas"]["TokenMetadata"];
export type Disk = components["schemas"]["ApiDisk"];
export type Interface = components["schemas"]["ApiInterface"];
export const esc = encodeURIComponent;
export const vmPath = (cloud: string, id: string) =>
    `/api/v1/clouds/${esc(cloud)}/machines/${esc(id)}`;
export const volumePath = (cloud: string, id: string) =>
    `/api/v1/clouds/${esc(cloud)}/volumes/${esc(id)}`;
export const machineURL = (cloud: string, id: string) =>
    `/machines/${esc(cloud)}/${esc(id)}/`;
export function useResource<T>(path: string, enabled = true, poll = true) {
    return useQuery({
        queryKey: [path],
        queryFn: ({ signal }) => request<T>(path, "GET", undefined, signal),
        enabled,
        refetchInterval: poll ? 15000 : false,
    });
}
export function useMachine(
    cloud: string,
    id: string,
    enabled = true,
    poll = true,
) {
    return useQuery<VMDetail>({
        queryKey: ["machine", cloud, id],
        enabled,
        queryFn: ({ signal }) =>
            request<VMDetail>(vmPath(cloud, id), "GET", undefined, signal),
        refetchInterval: poll ? 15000 : false,
    });
}
export function useMachines(cloud?: string) {
    return useResource<components["schemas"]["ApiMachineList"]>(
        `/api/v1/clouds/${esc(cloud || "")}/machines`,
        !!cloud,
    );
}
export function useVolumes(cloud: string, filters = "") {
    return useResource<VolumeList>(
        `/api/v1/clouds/${esc(cloud)}/volumes${filters ? `?${filters}` : ""}`,
        !!cloud,
    );
}
export function useOperation<T>(execute: (input: T) => Promise<unknown>) {
    const queries = useQueryClient();
    return useMutation({
        mutationFn: execute,
        retry: false,
        onSuccess: () => queries.invalidateQueries(),
    });
}
