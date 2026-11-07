import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
    cleanup,
    fireEvent,
    render,
    screen,
    waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Installer } from "./Installer";
import { connectSession, InstallError, readState, request } from "./client";
import type { InstallState } from "./types";

vi.mock("./client", async (original) => ({
    ...(await original<typeof import("./client")>()),
    connectSession: vi.fn(),
    readState: vi.fn(),
    request: vi.fn(),
}));

let state: InstallState;
beforeEach(() => {
    state = {
        revision: 4,
        configPath: "/tmp/setup/vmango.conf",
        exists: true,
        controller: { os: "darwin", arch: "arm64", hostname: "controller" },
        phase: "hosts",
        busy: false,
        needsAdmin: false,
        allowedStages: ["hosts"],
        hosts: [
            {
                id: "test-a",
                cloud: "default",
                zone: "default",
                address: "test-a",
                user: "deploy",
                port: 22,
                local: false,
                sudo: true,
                selected: true,
                network: "default",
                subnet: "192.168.122.0/24",
                dhcpStart: "192.168.122.2",
                dhcpEnd: "192.168.122.254",
                pool: "default",
                poolPath: "/var/lib/libvirt/images",
            },
        ],
        api: {
            listen: "127.0.0.1:8080",
            baseURL: "http://127.0.0.1:8080",
            tlsCertFile: "",
            tlsKeyFile: "",
            adminUser: "admin",
        },
        clouds: [
            {
                id: "default",
                name: "Default",
                zones: [{ id: "default", name: "Default zone" }],
            },
        ],
        results: [],
        logs: [],
    };
    vi.mocked(connectSession).mockResolvedValue();
    vi.mocked(readState).mockImplementation(async () => structuredClone(state));
    vi.mocked(request).mockResolvedValue({});
});
afterEach(() => {
    cleanup();
    vi.resetAllMocks();
});

it("loads an existing configuration without requesting an admin password or offering a local macOS host", async () => {
    render(<Installer />);
    await screen.findByText("Set up your virtualization hosts");
    expect(connectSession).toHaveBeenCalled();
    expect(screen.getByText("/tmp/setup/vmango.conf")).toBeTruthy();
    expect(
        screen.queryByLabelText("Initial administrator password"),
    ).toBeNull();
    expect(
        screen.queryByRole("button", { name: "Add this Linux host" }),
    ).toBeNull();
    expect(
        (screen.getByLabelText("Host ID") as HTMLInputElement).disabled,
    ).toBe(true);
    expect(
        (screen.getByLabelText("SSH hostname or alias") as HTMLInputElement)
            .disabled,
    ).toBe(true);
    expect(
        (
            screen.getByRole("button", {
                name: "Review resource changes",
            }) as HTMLButtonElement
        ).disabled,
    ).toBe(true);
});

it("allows Linux local selection and explicitly requests sudo instead of enabling it", async () => {
    state.controller.os = "linux";
    state.hosts = [];
    state.exists = false;
    state.needsAdmin = true;
    render(<Installer />);
    await userEvent.click(
        await screen.findByRole("button", { name: "Add this Linux host" }),
    );
    expect(screen.getByText("Local Linux")).toBeTruthy();
    expect(
        (screen.getByLabelText(/Use noninteractive sudo/) as HTMLInputElement)
            .checked,
    ).toBe(false);
    expect(
        screen.getByLabelText("Initial administrator password"),
    ).toBeTruthy();
    expect(screen.queryByLabelText("SSH hostname or alias")).toBeNull();
});

it("saves changed answers before preparing a plan using the refreshed revision", async () => {
    vi.mocked(request).mockImplementation(async (path) => {
        if (path === "answers") state.revision = 5;
        if (path === "prepare")
            state.plan = {
                id: "plan-5",
                stage: "hosts",
                summary: ["Install libvirt on test-a"],
            };
        return {};
    });
    render(<Installer />);
    fireEvent.change(await screen.findByLabelText("NAT network name"), {
        target: { value: "testing" },
    });
    await userEvent.click(
        screen.getByRole("button", { name: "Review host changes" }),
    );
    await screen.findByRole("region", { name: "Prepared plan" });
    const calls = vi.mocked(request).mock.calls;
    expect(calls[0][0]).toBe("answers");
    expect(calls[0][1]).toMatchObject({
        revision: 4,
        hosts: [{ network: "testing" }],
    });
    expect(calls[1]).toEqual(["prepare", { revision: 5, stage: "hosts" }]);
    expect(request).not.toHaveBeenCalledWith("apply", expect.anything());
});

it("approves exactly the displayed plan and treats subprocess output as plain text", async () => {
    state.plan = {
        id: "reviewed-digest",
        stage: "hosts",
        summary: ["Install selected packages"],
        output: '<img src=x onerror="alert(1)">',
    };
    render(<Installer />);
    await screen.findByRole("region", { name: "Prepared plan" });
    expect(screen.getByLabelText("Plan output").textContent).toContain("<img");
    expect(
        screen.getByLabelText("Plan output").querySelector("img"),
    ).toBeNull();
    await userEvent.click(
        screen.getByRole("button", { name: "Approve and apply changes" }),
    );
    await waitFor(() =>
        expect(request).toHaveBeenCalledWith("apply", {
            revision: 4,
            planID: "reviewed-digest",
        }),
    );
});

it("disables approval immediately when a reviewed answer changes", async () => {
    state.plan = {
        id: "old-plan",
        stage: "config",
        summary: ["Save configuration"],
    };
    render(<Installer />);
    fireEvent.change(
        await screen.findByLabelText("Pool directory on this host"),
        { target: { value: "/srv/vms" } },
    );
    expect(
        (
            screen.getByRole("button", {
                name: "Approve and save configuration",
            }) as HTMLButtonElement
        ).disabled,
    ).toBe(true);
    expect(
        screen.getByText(
            "Answers have changed. Prepare and review a new plan before applying.",
        ),
    ).toBeTruthy();
    expect(request).not.toHaveBeenCalled();
});

it("preserves unselected existing hosts in submitted answers", async () => {
    render(<Installer />);
    await userEvent.click(
        await screen.findByRole("checkbox", { name: "test-a" }),
    );
    expect(
        screen.getByText(
            "Not selected for this run. Any existing configuration entry is preserved.",
        ),
    ).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "Save answers" }));
    await waitFor(() =>
        expect(request).toHaveBeenCalledWith(
            "answers",
            expect.objectContaining({
                hosts: [
                    expect.objectContaining({ id: "test-a", selected: false }),
                ],
            }),
        ),
    );
});

it("reconnects to running work and only cancels after an explicit request", async () => {
    state.busy = true;
    state.results = [
        { id: "test-a", state: "running", message: "Installing packages" },
    ];
    state.logs = ["Preparing packages"];
    const first = render(<Installer />);
    await screen.findByText("Installing packages");
    first.unmount();
    render(<Installer />);
    await screen.findByText("Installing packages");
    expect(request).not.toHaveBeenCalled();
    await userEvent.click(
        screen.getByRole("button", { name: "Request cancellation" }),
    );
    await waitFor(() => expect(request).toHaveBeenCalledWith("cancel", {}));
});

it("reports an unavailable session without rendering a usable form", async () => {
    vi.mocked(readState).mockRejectedValue(
        new InstallError("unauthorized", 401),
    );
    render(<Installer />);
    await screen.findByText(/Open the session URL printed by vmango install/);
    expect(screen.queryByRole("button", { name: "Add SSH host" })).toBeNull();
});

it("does not claim server activation after configuration publication", async () => {
    state.phase = "complete";
    state.backupPath = "/tmp/setup/vmango.conf.backup";
    render(<Installer />);
    await screen.findByText("Configuration saved");
    expect(
        screen.getByText(
            /A running server has not automatically loaded these changes/,
        ),
    ).toBeTruthy();
    expect(
        screen.getByText("Backup: /tmp/setup/vmango.conf.backup"),
    ).toBeTruthy();
    for (const name of [
        "Add SSH host",
        "Inspect selected hosts",
        "Load existing resource settings",
    ]) {
        const button = screen.getByRole("button", { name });
        expect(button.closest("fieldset")?.disabled).toBe(true);
        fireEvent.click(button);
    }
    expect(request).not.toHaveBeenCalled();
});

it("clears the first-run password after saving and reviews without resending it", async () => {
    state.exists = false;
    state.needsAdmin = true;
    vi.mocked(request).mockImplementation(async (path) => {
        if (path === "answers") state.revision++;
        if (path === "prepare")
            state.plan = {
                id: "first-plan",
                stage: "hosts",
                summary: ["Prepare test-a"],
            };
        return {};
    });
    render(<Installer />);
    fireEvent.change(
        await screen.findByLabelText("Initial administrator password"),
        { target: { value: "initial-private-password" } },
    );
    await userEvent.click(screen.getByRole("button", { name: "Save answers" }));
    await waitFor(() =>
        expect(
            (
                screen.getByLabelText(
                    "Initial administrator password",
                ) as HTMLInputElement
            ).value,
        ).toBe(""),
    );
    expect(request).toHaveBeenCalledWith(
        "answers",
        expect.objectContaining({
            api: expect.objectContaining({
                password: "initial-private-password",
            }),
        }),
    );
    await userEvent.click(
        screen.getByRole("button", { name: "Review host changes" }),
    );
    await screen.findByRole("region", { name: "Prepared plan" });
    expect(
        vi.mocked(request).mock.calls.filter(([path]) => path === "answers"),
    ).toHaveLength(1);
    expect(request).toHaveBeenCalledWith("prepare", {
        revision: 5,
        stage: "hosts",
    });
});

it("stops before preparation when initial credentials are rejected", async () => {
    state.exists = false;
    state.needsAdmin = true;
    vi.mocked(request).mockRejectedValue(
        new InstallError("Provide an initial administrator password.", 409),
    );
    render(<Installer />);
    fireEvent.change(await screen.findByLabelText("NAT network name"), {
        target: { value: "testing" },
    });
    await userEvent.click(
        screen.getByRole("button", { name: "Review host changes" }),
    );
    await screen.findByText("Provide an initial administrator password.");
    expect(vi.mocked(request).mock.calls.map(([path]) => path)).toEqual([
        "answers",
    ]);
    expect(screen.queryByRole("region", { name: "Prepared plan" })).toBeNull();
});

it("loads existing resource settings only after an explicit request and saves changed names first", async () => {
    vi.mocked(request).mockImplementation(async (path, body) => {
        if (path === "answers") {
            state.hosts = (body as { hosts: typeof state.hosts }).hosts;
            state.revision++;
        }
        if (path === "discover") {
            state.hosts[0].subnet = "10.48.3.0/24";
            state.hosts[0].poolPath = "/srv/existing-vms";
            state.revision++;
            state.plan = undefined;
        }
        return {};
    });
    render(<Installer />);
    const name = await screen.findByLabelText("NAT network name");
    expect(request).not.toHaveBeenCalled();
    fireEvent.change(name, { target: { value: "existing-nat" } });
    await userEvent.click(
        screen.getByRole("button", { name: "Load existing resource settings" }),
    );
    await waitFor(() =>
        expect(
            (
                screen.getByLabelText(
                    "Pool directory on this host",
                ) as HTMLInputElement
            ).value,
        ).toBe("/srv/existing-vms"),
    );
    expect(vi.mocked(request).mock.calls.map(([path]) => path)).toEqual([
        "answers",
        "discover",
    ]);
    expect(request).toHaveBeenCalledWith("discover", { revision: 5 });
    expect(
        (screen.getByLabelText("Network subnet") as HTMLInputElement).value,
    ).toBe("10.48.3.0/24");
    expect(request).not.toHaveBeenCalledWith("apply", expect.anything());
});
