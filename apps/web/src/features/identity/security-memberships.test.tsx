import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nextProvider } from "react-i18next";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { i18n, changeLanguage } from "@/i18n";
import { SecurityMemberships } from "./security-memberships";
import { SecurityPanel } from "./security-panel";

const switchContext = vi.hoisted(() => vi.fn());
const actor = vi.hoisted(() => ({ permissions: [] as string[] }));
vi.mock("./auth", () => ({
  useAuth: () => ({ selectSignInIdentity: switchContext }),
  useSession: () => ({
    tenantId: "tenant-a",
    account: {
      id: 7,
      organizationId: 22,
      memberships: [{ departmentId: 3, organizationId: 44 }],
    },

    identityBindingId: "",
    permissions: actor.permissions,
    authorizationVersion: "v1",
  }),
}));
vi.mock("@/features/governance/runtime", () => ({
  useConfiguration: () => ({
    value: (_key: string, fallback: unknown) => fallback,
  }),
}));
beforeEach(async () => {
  vi.clearAllMocks();
  actor.permissions = [];
  switchContext.mockResolvedValue({ portal: "supplier" });
  await changeLanguage("en");
});
afterEach(() => vi.unstubAllGlobals());

function setup(options: { failFirst?: boolean; delayed?: boolean } = {}) {
  const calls: { input: unknown; key: string }[] = [];
  let finish: ((response: Response) => void) | undefined;
  let reads = 0;
  const contexts = [
    {},
    {
      identityBindingId: "binding-original",
      identityType: "supplier",
      actingOrganizationName: "Purchasing",
    },
  ];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("/api/auth/identities")) {
        reads++;
        return Response.json(contexts);
      }
      if (decodeURIComponent(url).includes("identity.membership.invite")) {
        const payload = JSON.parse(String(init?.body));
        calls.push({ input: payload.input, key: payload.idempotencyKey });
        return Response.json({
          status: "succeeded",
          data: {
            code: "d".repeat(64),
            expiresAt: "2026-11-21T05:00:00Z",
            id: "invitation-id",
          },
        });
      }
      if (decodeURIComponent(url).includes("identity.membership.accept")) {
        const payload = JSON.parse(String(init?.body));
        calls.push({ input: payload.input, key: payload.idempotencyKey });
        if (options.delayed)
          return new Promise<Response>((resolve) => {
            finish = resolve;
          });
        if (options.failFirst && calls.length === 1)
          return Response.json({ status: "failed", errorCode: "unavailable" });
        return Response.json({
          status: "succeeded",
          data: { tenantId: "tenant-b", departmentId: 91 },
        });
      }
      throw new Error("Unexpected endpoint");
    }),
  );
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const view = render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <QueryClientProvider client={client}>
          <SecurityMemberships
            queryKey={["iam.security", "tenant-a", 7, 3, "v1"]}
          />
        </QueryClientProvider>
      </MemoryRouter>
    </I18nextProvider>,
  );
  return {
    ...view,
    client,
    calls,
    reads: () => reads,
    finish: () =>
      finish?.(
        Response.json({
          status: "succeeded",
          data: { tenantId: "tenant-b", departmentId: 91 },
        }),
      ),
  };
}

async function invitation() {
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "Accept invitation" }));
  const dialog = screen.getByRole("dialog");
  return {
    user,
    dialog,
    input: within(dialog).getByLabelText("Invitation code", { exact: true }),
    submit: within(dialog).getByRole("button", { name: "Accept invitation" }),
  };
}

it("lists authorized names and selects the bound identity without sending a tenant selector", async () => {
  const view = setup();
  await screen.findByText("Purchasing");
  expect(screen.getByText("Purchasing")).toBeVisible();
  expect(screen.queryByText("tenant-b")).not.toBeInTheDocument();
  expect(screen.queryByText("binding-original")).not.toBeInTheDocument();
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "Actions" }));
  await user.click(
    await screen.findByRole("menuitem", { name: "Use this identity" }),
  );
  expect(switchContext).toHaveBeenCalledExactlyOnceWith("binding-original");
  view.unmount();
  view.client.clear();
});

it("rejects an incomplete code before calling Gateway", async () => {
  const view = setup();
  const { user, input, submit } = await invitation();
  await user.type(input, "invalid");
  await user.click(submit);
  expect(
    await screen.findByText("Enter the complete 64-character invitation code."),
  ).toBeVisible();
  expect(view.calls).toHaveLength(0);
  view.unmount();
  view.client.clear();
});

it("accepts through Gateway, refreshes contexts and keeps the credential out of Query data", async () => {
  const view = setup();
  await screen.findByText("Purchasing");
  const { user, input, submit } = await invitation();
  const code = "a".repeat(64);
  await user.type(input, code);
  await user.click(submit);
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
  expect(view.calls).toHaveLength(1);
  expect(view.calls[0].input).toEqual({ code });
  expect(view.calls[0].key).toBeTruthy();
  expect(view.reads()).toBeGreaterThan(1);
  expect(switchContext).not.toHaveBeenCalled();
  expect(
    JSON.stringify(
      view.client
        .getQueryCache()
        .getAll()
        .map((query) => query.state.data),
    ),
  ).not.toContain(code);
  await user.click(screen.getByRole("button", { name: "Accept invitation" }));
  expect(screen.getByLabelText("Invitation code", { exact: true })).toHaveValue(
    "",
  );
  view.unmount();
  view.client.clear();
});

it("reuses the idempotency key when retrying the same invitation", async () => {
  const view = setup({ failFirst: true });
  const { user, input, submit } = await invitation();
  await user.type(input, "b".repeat(64));
  await user.click(submit);
  await waitFor(() => expect(submit).toBeEnabled());
  expect(view.calls).toHaveLength(1);
  await user.click(submit);
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
  expect(view.calls).toHaveLength(2);
  expect(view.calls[1].key).toBe(view.calls[0].key);
  view.unmount();
  view.client.clear();
});

it("discards an invitation response after its context unmounts", async () => {
  const view = setup({ delayed: true });
  const invalidate = vi.spyOn(view.client, "invalidateQueries");
  const { user, input, submit } = await invitation();
  await user.type(input, "c".repeat(64));
  await user.click(submit);
  await waitFor(() => expect(view.calls).toHaveLength(1));
  expect(submit).toBeDisabled();
  expect(
    within(screen.getByRole("dialog")).getByRole("button", { name: "Cancel" }),
  ).toBeDisabled();
  view.unmount();
  await act(async () => {
    view.finish();
  });
  expect(invalidate).not.toHaveBeenCalled();
  view.client.clear();
});

it("does not query memberships in Runtime preview", async () => {
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter initialEntries={["/account-security?tab=memberships"]}>
        <SecurityPanel props={{}} readOnly emit={vi.fn()} />
      </MemoryRouter>
    </I18nextProvider>,
  );
  expect(fetch).not.toHaveBeenCalled();
  expect(screen.queryByRole("button")).not.toBeInTheDocument();
});

it("does not offer invitation creation without account creation permission", async () => {
  const view = setup();
  await screen.findByText("Purchasing");
  expect(
    screen.queryByRole("button", { name: "Invite member" }),
  ).not.toBeInTheDocument();
  view.unmount();
  view.client.clear();
});

it("invites into the deployment tenant and discloses the code only in the dialog", async () => {
  actor.permissions = ["accounts:create"];
  const view = setup();
  const user = userEvent.setup();
  await user.click(
    await screen.findByRole("button", { name: "Invite member" }),
  );
  const dialog = screen.getByRole("dialog");
  await user.type(
    within(dialog).getByRole("textbox", { name: "Recipient login" }),
    "other.login",
  );
  await user.click(
    within(dialog).getByRole("button", { name: "Generate invitation" }),
  );
  expect(await screen.findByDisplayValue("d".repeat(64))).toHaveAttribute(
    "readonly",
  );
  expect(view.calls[0].input).toEqual({
    username: "other.login",
  });
  expect(
    JSON.stringify(
      view.client
        .getQueryCache()
        .getAll()
        .map((query) => query.state.data),
    ),
  ).not.toContain("d".repeat(64));
  await user.click(
    within(dialog).getByRole("button", { name: "I have saved the codes" }),
  );
  expect(screen.queryByDisplayValue("d".repeat(64))).not.toBeInTheDocument();
  expect(switchContext).not.toHaveBeenCalled();
  view.unmount();
  view.client.clear();
});
