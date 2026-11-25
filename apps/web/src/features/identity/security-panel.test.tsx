import { afterEach, expect, it, vi } from "vitest";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nextProvider } from "react-i18next";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { i18n, changeLanguage } from "@/i18n";
import { SecurityPanel } from "./security-panel";

vi.mock("./auth", () => ({
  useAuth: () => ({}),
  useSession: () => ({
    tenantId: "tenant-a",
    account: { id: 7 },

    permissions: [],
    authorizationVersion: "test",
  }),
}));
vi.mock("@/features/governance/runtime", () => ({
  useConfiguration: () => ({
    value: (_key: string, fallback: unknown) => fallback,
  }),
}));
afterEach(() => vi.unstubAllGlobals());

it.each(["devices", "external"])(
  "keeps the original UUID when submitting %s table actions",
  async (tab) => {
    await changeLanguage("en");
    const id = "fedcba98-7654-4321-9876-0123456789ab";
    const calls: { name: string; input: Record<string, unknown> }[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        if (url.endsWith("/federation/providers")) return Response.json([]);
        const name = decodeURIComponent(url.split("/")[3]);
        const payload = JSON.parse(String(init?.body));
        calls.push({ name, input: payload.input });
        let data: object = {};
        if (name === "identity.session.list")
          data = {
            items: [
              {
                id,
                device: "Other browser",
                tenantId: "tenant-a",
                current: false,
                createdAt: "2026-11-01T00:00:00Z",
                lastSeenAt: "2026-11-01T00:00:00Z",
                expiresAt: "2026-12-01T00:00:00Z",
              },
            ],
          };
        if (name === "identity.session.reauthenticate")
          data = { proof: "p".repeat(64) };
        if (name === "identity.externalReview.list")
          data = { items: [], nextAfter: "" };
        if (name === "identity.externalIdentity.list")
          data = {
            items: [
              {
                id,
                providerId: "fixture",
                linkedAt: "2026-11-01T00:00:00Z",
                revokedAt: "",
              },
            ],
          };
        return Response.json({ status: "succeeded", data });
      }),
    );
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <I18nextProvider i18n={i18n}>
        <QueryClientProvider client={client}>
          <MemoryRouter initialEntries={[`/account-security?tab=${tab}`]}>
            <SecurityPanel props={{}} readOnly={false} emit={vi.fn()} />
          </MemoryRouter>
        </QueryClientProvider>
      </I18nextProvider>,
    );
    const user = userEvent.setup();
    await screen.findByText(tab === "devices" ? "Other browser" : "fixture");
    await user.click(screen.getByRole("button", { name: "Actions" }));
    await user.click(
      await screen.findByRole("menuitem", {
        name: tab === "devices" ? "Sign out device" : "Unlink provider",
      }),
    );
    await user.type(
      screen.getByLabelText("Password", { exact: true }),
      "TestOnly!2026Secure",
    );
    await user.click(screen.getByRole("button", { name: "Confirm" }));
    const name =
      tab === "devices"
        ? "identity.session.revoke"
        : "identity.externalIdentity.unlink";
    await screen.findByText(tab === "devices" ? "Other browser" : "fixture");
    expect(calls.find((call) => call.name === name)?.input).toMatchObject({
      id,
    });
    if (tab === "devices")
      expect(
        calls.find((call) => call.name === "identity.session.reauthenticate")
          ?.input,
      ).toEqual({ id, password: "TestOnly!2026Secure" });
    client.clear();
  },
);

it("cancels reauthentication and does not revoke a device from an unmounted context", async () => {
  await changeLanguage("en");
  let finish: ((response: Response) => void) | undefined;
  let signal: AbortSignal | null | undefined;
  const calls: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const name = decodeURIComponent(url.split("/")[3]);
      calls.push(name);
      if (name === "identity.session.list")
        return Response.json({
          status: "succeeded",
          data: {
            items: [
              {
                id: "fedcba98-7654-4321-9876-0123456789ab",
                device: "Other browser",
                tenantId: "tenant-a",
                current: false,
                lastSeenAt: "2026-11-01T00:00:00Z",
                expiresAt: "2026-12-01T00:00:00Z",
              },
            ],
          },
        });
      if (name === "identity.session.reauthenticate") {
        signal = init?.signal;
        return new Promise<Response>((resolve) => {
          finish = resolve;
        });
      }
      throw new Error("Unexpected capability after context teardown");
    }),
  );
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const view = render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={["/account-security?tab=devices"]}>
          <SecurityPanel props={{}} readOnly={false} emit={vi.fn()} />
        </MemoryRouter>
      </QueryClientProvider>
    </I18nextProvider>,
  );
  const user = userEvent.setup();
  await screen.findByText("Other browser");
  await user.click(screen.getByRole("button", { name: "Actions" }));
  await user.click(
    await screen.findByRole("menuitem", { name: "Sign out device" }),
  );
  await user.type(
    screen.getByLabelText("Password", { exact: true }),
    "TestOnly!2026Secure",
  );
  await user.click(screen.getByRole("button", { name: "Confirm" }));
  await waitFor(() => expect(finish).toBeDefined());
  view.unmount();
  expect(signal?.aborted).toBe(true);
  await act(async () => {
    finish?.(
      Response.json({ status: "succeeded", data: { proof: "p".repeat(64) } }),
    );
  });
  expect(calls).not.toContain("identity.session.revoke");
  client.clear();
});
