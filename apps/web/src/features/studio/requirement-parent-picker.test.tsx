import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nextProvider } from "react-i18next";
import { i18n } from "@/i18n";
import { expect, it, vi } from "vitest";
import { studio } from "./api";
import { RequirementParentPicker } from "./requirement-parent-picker";

vi.mock("@/features/identity/auth", () => ({
  useSession: () => ({
    tenantId: "tenant",
    account: { id: 1 },
    activeDepartmentId: undefined,
  }),
}));
vi.mock("./api", async (load) => ({
  ...(await load<typeof import("./api")>()),
  studio: {
    searchRequirements: vi.fn(async () => [
      { id: 2, title: "Second root" },
      { id: 3, title: "Third root" },
    ]),
  },
}));

it("searches top-level requirements remotely and selects one", async () => {
  const user = userEvent.setup();
  const onChange = vi.fn();
  const query = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={query}>
        <RequirementParentPicker
          id="parent"
          project={7}
          value={0}
          exclude={12}
          onChange={onChange}
        />
      </QueryClientProvider>
    </I18nextProvider>,
  );
  const input = screen.getByLabelText("Parent requirement");
  expect(input).toHaveValue("None (top-level requirement)");
  await user.click(input);
  await user.type(input, "Second");
  await waitFor(() =>
    expect(studio.searchRequirements).toHaveBeenLastCalledWith(
      7,
      "Second",
      12,
      expect.anything(),
    ),
  );
  const option = await screen.findByRole("option", { name: "Second root" });
  await user.click(option);
  expect(onChange).toHaveBeenCalledWith(2);
});

it("shows the authorized title of an existing parent without a query", () => {
  const query = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={query}>
        <RequirementParentPicker
          id="parent"
          project={7}
          value={2}
          valueTitle="Second root"
          exclude={12}
          onChange={() => {}}
        />
      </QueryClientProvider>
    </I18nextProvider>,
  );
  expect(screen.getByLabelText("Parent requirement")).toHaveValue(
    "Second root",
  );
});
