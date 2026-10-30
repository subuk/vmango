import { afterEach, expect, it } from "vitest";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router";
import { useDirty } from "./common";

afterEach(cleanup);
function Form() {
    const prompt = useDirty(true);
    return (
        <>
            {prompt}
            <p>Unsaved form</p>
        </>
    );
}
it("can cancel Back navigation without discarding a dirty form", async () => {
    const router = createMemoryRouter(
        [
            { path: "/list", element: <p>Resource list</p> },
            { path: "/edit", element: <Form /> },
        ],
        { initialEntries: ["/list", "/edit"], initialIndex: 1 },
    );
    render(<RouterProvider router={router} />);
    await act(async () => {
        await router.navigate(-1);
    });
    expect(
        await screen.findByRole("dialog", { name: "Discard unsaved changes?" }),
    ).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "Stay on page" }));
    expect(screen.getByText("Unsaved form")).toBeTruthy();
    expect(router.state.location.pathname).toBe("/edit");
    await act(async () => {
        await router.navigate(-1);
    });
    await userEvent.click(
        await screen.findByRole("button", { name: "Discard and leave" }),
    );
    await waitFor(() => expect(screen.getByText("Resource list")).toBeTruthy());
});
