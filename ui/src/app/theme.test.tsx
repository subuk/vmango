import { beforeEach, afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Appearance, useAppearance } from "./theme";

let osDark = false;
let listeners: Set<() => void>;
beforeEach(() => {
    localStorage.clear();
    osDark = false;
    listeners = new Set();
    vi.stubGlobal("matchMedia", () => ({
        get matches() {
            return osDark;
        },
        addEventListener: (_: string, fn: () => void) => listeners.add(fn),
        removeEventListener: (_: string, fn: () => void) =>
            listeners.delete(fn),
    }));
});
afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
});
function Control() {
    const { mode, preference, setPreference } = useAppearance();
    return (
        <>
            <span>
                {mode}/{preference}
            </span>
            <button onClick={() => setPreference("dark")}>Dark</button>
            <button onClick={() => setPreference("light")}>Light</button>
            <button onClick={() => setPreference("system")}>System</button>
        </>
    );
}
it("follows OS changes only in System mode and retains an explicit preference after remount", async () => {
    const user = userEvent.setup();
    const first = render(
        <Appearance>
            <Control />
        </Appearance>,
    );
    expect(screen.getByText("light/system")).toBeTruthy();
    act(() => {
        osDark = true;
        listeners.forEach((fn) => fn());
    });
    expect(screen.getByText("dark/system")).toBeTruthy();
    await user.click(screen.getByText("Light"));
    act(() => {
        listeners.forEach((fn) => fn());
    });
    expect(screen.getByText("light/light")).toBeTruthy();
    first.unmount();
    render(
        <Appearance>
            <Control />
        </Appearance>,
    );
    expect(screen.getByText("light/light")).toBeTruthy();
    expect(document.documentElement.style.colorScheme).toBe("light");
    expect(localStorage.getItem("vmango.theme")).toBe("light");
});
it("uses System safely when browser storage is unavailable", async () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
        throw new Error("Storage denied");
    });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
        throw new Error("Storage denied");
    });
    render(
        <Appearance>
            <Control />
        </Appearance>,
    );
    expect(screen.getByText("light/system")).toBeTruthy();
    await userEvent.click(screen.getByText("Dark"));
    expect(screen.getByText("dark/dark")).toBeTruthy();
});
