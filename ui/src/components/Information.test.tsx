import { afterEach, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { Capacity } from "./Information";

afterEach(cleanup);
it("does not present missing storage capacity as zero usage", () => {
    render(<Capacity used={0} total={0} label="Pool usage" />);
    expect(screen.getByText("Capacity unavailable")).toBeTruthy();
    expect(screen.queryByRole("progressbar")).toBeNull();
});
it("keeps the storage meter within its accessible range for inconsistent backend totals", () => {
    render(<Capacity used={120} total={100} label="Pool usage" />);
    expect(
        screen
            .getByRole("progressbar", { name: "Pool usage" })
            .getAttribute("aria-valuenow"),
    ).toBe("100");
});
