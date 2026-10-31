import {
    useQuery,
    useQueryClient,
    type UseQueryResult,
} from "@tanstack/react-query";
import { useMe } from "../app/Auth";
import { request } from "./client";
import type { components } from "./schema";

export type Installation = components["schemas"]["AdministrationInstallation"];
export type AdminCloud = components["schemas"]["AdministrationCloud"];
export type AdminHost = components["schemas"]["AdministrationHost"];
export type AdminImage = components["schemas"]["AdministrationImage"];
export type ImageAvailability =
    components["schemas"]["AdministrationImageAvailability"];
export type HostList = components["schemas"]["ApiAdministrationHostList"];
export const adminCloudPath = (cloud: string) =>
    `/api/v1/administration/clouds/${encodeURIComponent(cloud)}`;
export const adminCloudURL = (cloud: string) =>
    `/administration/clouds/${encodeURIComponent(cloud)}/`;

export function useAdministration<T>(
    path: string,
    enabled = true,
    snapshotID?: string,
): UseQueryResult<T, Error> {
    const me = useMe();
    const queries = useQueryClient();
    const installationPath = "/api/v1/administration/configuration";
    const installationKey = [
        "administration",
        me?.id,
        installationPath,
        undefined,
    ];
    const configuration =
        !path.includes("/availability") &&
        !/\/clouds\/[^/]+\/hosts(?:\/|$)/.test(path);
    const metadata = useQuery({
        queryKey: installationKey,
        queryFn: ({ signal }) =>
            request<Installation>(installationPath, "GET", undefined, signal),
        enabled: enabled && configuration && path !== installationPath,
        refetchInterval: false,
        refetchOnWindowFocus: false,
    });
    const acceptedSnapshot = metadata.data?.snapshot_id || snapshotID;
    const key = [
        "administration",
        me?.id,
        path,
        path === installationPath || !configuration
            ? undefined
            : acceptedSnapshot,
    ];
    const query = useQuery<T>({
        queryKey: key,
        queryFn: ({ signal }) => request<T>(path, "GET", undefined, signal),
        enabled:
            enabled &&
            (!configuration || path === installationPath || !!acceptedSnapshot),
        refetchInterval: false,
        refetchOnWindowFocus: false,
    });
    if (
        configuration &&
        path !== installationPath &&
        !acceptedSnapshot &&
        metadata.isError
    ) {
        return { ...metadata, data: undefined } as UseQueryResult<T, Error>;
    }
    return {
        ...query,
        refetch: async () => {
            if (configuration && path !== installationPath) {
                const current = await metadata.refetch();
                if (
                    current.data &&
                    current.data.snapshot_id !== acceptedSnapshot
                ) {
                    await queries.fetchQuery({
                        queryKey: [
                            "administration",
                            me?.id,
                            path,
                            current.data.snapshot_id,
                        ],
                        queryFn: ({ signal }) =>
                            request<T>(path, "GET", undefined, signal),
                    });
                    return query;
                }
            }
            return query.refetch();
        },
    };
}
