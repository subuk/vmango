import { afterEach, expect, it, vi } from "vitest";
import {
    cleanup,
    fireEvent,
    render,
    screen,
    waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { OperationDialog } from "./OperationDialog";
import { APIError } from "../api/client";

afterEach(cleanup);
it("keeps an uncertain operation visible and disables duplicate submissions and retries", async () => {
    let fail: (reason: Error) => void = () => {};
    const run = vi.fn(
        () =>
            new Promise<void>((_, reject) => {
                fail = reject;
            }),
    );
    const close = vi.fn();
    render(
        <QueryClientProvider client={new QueryClient()}>
            <OperationDialog
                title="Create volume"
                action="Create volume"
                run={run}
                onClose={close}
            >
                {() => <p>Volume settings</p>}
            </OperationDialog>
        </QueryClientProvider>,
    );
    const button = screen.getByRole("button", { name: "Create volume" });
    await userEvent.click(button);
    expect(button.hasAttribute("disabled")).toBe(true);
    fireEvent.submit(button.closest("form")!);
    expect(run).toHaveBeenCalledTimes(1);
    fail(
        new APIError(0, {
            detail: "Connection lost. Refresh resource state before retrying.",
        }),
    );
    await waitFor(() =>
        expect(
            screen
                .getByRole("button", { name: "Create volume" })
                .hasAttribute("disabled"),
        ).toBe(true),
    );
    expect(close).not.toHaveBeenCalled();
    expect(
        screen.getByText(
            "Connection lost. Refresh resource state before retrying.",
        ),
    ).toBeTruthy();
});
