export type Stage = "hosts" | "resources" | "config";

export interface InstallHost {
    id: string;
    cloud: string;
    zone: string;
    address: string;
    user: string;
    port: number;
    local: boolean;
    sudo: boolean;
    network: string;
    subnet: string;
    dhcpStart: string;
    dhcpEnd: string;
    pool: string;
    poolPath: string;
    selected: boolean;
    status?: string;
    message?: string;
}

export interface APISettings {
    listen: string;
    baseURL: string;
    tlsCertFile: string;
    tlsKeyFile: string;
    adminUser: string;
    password?: string;
}

export interface InstallState {
    revision: number;
    configPath: string;
    exists: boolean;
    controller: { os: string; arch: string; hostname: string; user?: string };
    phase: string;
    busy: boolean;
    error?: string;
    hosts: InstallHost[];
    api: APISettings;
    needsAdmin?: boolean;
    canResume?: boolean;
    allowedStages?: Stage[];
    clouds: {
        id: string;
        name: string;
        zones: { id: string; name: string }[];
    }[];
    plan?: { id: string; stage: Stage; summary: string[]; output?: string };
    results: { id: string; state: string; message: string }[];
    logs: string[];
    backupPath?: string;
}

export const stageLabels: Record<Stage, string> = {
    hosts: "Prepare Linux hosts",
    resources: "Configure networks and storage",
    config: "Save Vmango configuration",
};
