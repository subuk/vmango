import { afterEach, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { StorageClassDescription } from "./StorageClassDescription";

afterEach(cleanup);

it("explains the selected dependent copy and its source lifetime", () => {
    render(
        <StorageClassDescription
            cloning
            storageClass={{
                id: "efficient",
                name: "Space-efficient",
                description: "Shared source disks",
                clone_mode: "overlay",
            }}
        />,
    );
    expect(screen.getByText("Shared source disks")).toBeTruthy();
    expect(
        screen.getByText(
            /Creates a space-saving copy that depends on its source/,
        ).textContent,
    ).toContain("even when the machine is stopped");
    expect(screen.queryByText(/qcow2/)).toBeNull();
});

it("updates the explanation when the selected class changes", () => {
    const view = render(
        <StorageClassDescription
            cloning
            storageClass={{
                id: "efficient",
                name: "Space-efficient",
                clone_mode: "overlay",
            }}
        />,
    );
    view.rerender(
        <StorageClassDescription
            cloning
            storageClass={{
                id: "standard",
                name: "Standard",
                clone_mode: "copy",
            }}
        />,
    );
    expect(screen.getByText(/Creates an independent copy/)).toBeTruthy();
    expect(screen.queryByText(/must not be changed or deleted/)).toBeNull();
});

it("does not describe blank volumes as depending on a source", () => {
    render(
        <StorageClassDescription
            storageClass={{
                id: "efficient",
                name: "Space-efficient",
                description: "Shared source disks",
                clone_mode: "overlay",
            }}
        />,
    );
    expect(screen.getByText("Shared source disks")).toBeTruthy();
    expect(screen.queryByText(/Creates a space-saving copy/)).toBeNull();
});
