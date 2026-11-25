import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nextProvider } from "react-i18next";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { i18n, changeLanguage } from "@/i18n";
import { SecurityWorkspaceMembers } from "./security-workspace-members";

const actor = vi.hoisted(() => ({
  permissions: ["access:manage"],
  identityBindingId: "",
}));
vi.mock("./auth", () => ({
  useSession: () => ({ ...actor, account: { id: 7 } }),
}));
vi.mock("@/features/governance/runtime", () => ({
  useConfiguration: () => ({
    value: (_key: string, fallback: unknown) => fallback,
  }),
}));
beforeEach(async () => {
  vi.clearAllMocks();
  actor.permissions = ["access:manage"];
  actor.identityBindingId = "";
  await changeLanguage("en");
});
afterEach(() => vi.unstubAllGlobals());

function setup(options: { failFirst?: boolean; delayed?: boolean } = {}) {
  const calls: {
    name: string;
    input: Record<string, unknown>;
    key: string;
    signal?: AbortSignal | null;
  }[] = [];
  let revoked = false;
  let finish: ((response: Response) => void) | undefined;
  const row = (id: number) => ({
    id,
    username: id === 105 ? "partner.login" : "next.login",

    membershipStatus: revoked && id === 105 ? "revoked" : "active",
    accountStatus: "active",
    loginStatus: "active",

    protected: false,
    expiresAt: "",
  });
  const fetch = vi.fn(async (url: string, init?: RequestInit) => {
    const name = decodeURIComponent(url.split("/")[3]);
    const payload = JSON.parse(String(init?.body));
    calls.push({
      name,
      input: payload.input,
      key: payload.idempotencyKey,
      signal: init?.signal,
    });
    if (name === "identity.membership.list")
      return Response.json({
        status: "succeeded",
        data: {
          items: payload.input.after
            ? [row(200)]
            : [
                {
                  ...row(1),
                  username: "admin.login",

                  protected: true,
                },
                row(105),
              ],
          nextAfter: payload.input.after ? 0 : 105,
        },
      });
    if (name === "identity.membership.revoke") {
      if (options.delayed)
        return new Promise<Response>((resolve) => {
          finish = resolve;
        });
      if (
        options.failFirst &&
        calls.filter((call) => call.name === name).length === 1
      )
        return Response.json({ status: "failed", errorCode: "unavailable" });
      revoked = true;
      return Response.json({ status: "succeeded", data: { id: 105 } });
    }
    throw new Error("Unexpected capability");
  });
  vi.stubGlobal("fetch", fetch);
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const view = render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <MemoryRouter
          initialEntries={["/account-security?tab=workspace-members"]}
        >
          <SecurityWorkspaceMembers
            queryKey={["iam.security", "tenant-a", 7, 3, "v1"]}
          />
        </MemoryRouter>
      </QueryClientProvider>
    </I18nextProvider>,
  );
  return {
    ...view,
    calls,
    client,
    fetch,
    finish: () =>
      finish?.(Response.json({ status: "succeeded", data: { id: 105 } })),
  };
}

async function openRevoke() {
  const user = userEvent.setup();
  await screen.findByText("partner.login");
  await user.click(screen.getByRole("button", { name: "Actions" }));
  await user.click(
    await screen.findByRole("menuitem", { name: "Revoke membership" }),
  );
  return {
    user,
    confirm: within(screen.getByRole("dialog")).getByRole("button", {
      name: "Confirm",
    }),
  };
}

it.each(["permission", "binding"])(
  "does not query member data when the %s gate is closed",
  async (gate) => {
    if (gate === "permission") actor.permissions = [];
    else actor.identityBindingId = "bound-context";
    const view = setup();
    expect(view.fetch).not.toHaveBeenCalled();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    view.unmount();
    view.client.clear();
  },
);

it("shows authorized names, protects the administrator and requires explicit revoke confirmation", async () => {
  const view = setup();
  const { user, confirm } = await openRevoke();
  expect(screen.getByText("Protected administrator")).toBeVisible();
  expect(screen.getByText("admin.login")).toBeVisible();
  expect(
    view.calls.filter((call) => call.name === "identity.membership.revoke"),
  ).toHaveLength(0);
  await user.click(confirm);
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
  expect(
    view.calls.find((call) => call.name === "identity.membership.revoke")
      ?.input,
  ).toEqual({ accountId: 105 });
  expect(await screen.findByText("Revoked")).toBeVisible();
  view.unmount();
  view.client.clear();
});

it("uses server cursors for pagination and can return to the first page", async () => {
  const view = setup();
  const user = userEvent.setup();
  await screen.findByText("partner.login");
  await user.click(screen.getByRole("button", { name: "Next" }));
  await screen.findByText("next.login");
  expect(view.calls.at(-1)?.input).toEqual({ after: 105 });
  expect(screen.getByRole("button", { name: "Next" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "First page" }));
  expect(await screen.findByText("partner.login")).toBeVisible();
  view.unmount();
  view.client.clear();
});

it("keeps a stable key for the same failed revocation", async () => {
  const view = setup({ failFirst: true });
  const { user, confirm } = await openRevoke();
  await user.click(confirm);
  await waitFor(() => expect(confirm).toBeEnabled());
  await user.click(confirm);
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
  const attempts = view.calls.filter(
    (call) => call.name === "identity.membership.revoke",
  );
  expect(attempts).toHaveLength(2);
  expect(attempts[1].key).toBe(attempts[0].key);
  view.unmount();
  view.client.clear();
});

it("aborts an outstanding revocation and ignores its old callback after unmount", async () => {
  const view = setup({ delayed: true });
  const { user, confirm } = await openRevoke();
  await user.click(confirm);
  await waitFor(() =>
    expect(
      view.calls.some((call) => call.name === "identity.membership.revoke"),
    ).toBe(true),
  );
  const attempt = view.calls.find(
    (call) => call.name === "identity.membership.revoke",
  );
  const reads = view.calls.filter(
    (call) => call.name === "identity.membership.list",
  ).length;
  view.unmount();
  expect(attempt?.signal?.aborted).toBe(true);
  await act(async () => {
    view.finish();
  });
  expect(
    view.calls.filter((call) => call.name === "identity.membership.list"),
  ).toHaveLength(reads);
  view.client.clear();
});
