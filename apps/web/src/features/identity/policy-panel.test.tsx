import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nextProvider } from "react-i18next";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { i18n, changeLanguage } from "@/i18n";
import { PolicyPanel } from "./policy-panel";

const actor = vi.hoisted(() => ({
  id: 7,
  permissions: ["access:manage", "example:create"],
}));
vi.mock("./auth", () => ({
  useSession: () => ({
    tenantId: "tenant-a",
    account: { id: actor.id },

    permissions: actor.permissions,
    authorizationVersion: "test",
    identityBindingId: "",
  }),
  usePermission: (permission: string) => actor.permissions.includes(permission),
}));
vi.mock("@/features/governance/runtime", () => ({
  useConfiguration: () => ({
    value: (_key: string, fallback: unknown) => fallback,
  }),
}));
beforeEach(async () => {
  actor.id = 7;
  actor.permissions = ["access:manage", "example:create"];
  vi.clearAllMocks();
  await changeLanguage("en");
});
afterEach(() => vi.unstubAllGlobals());
const rule = {
  action: "example.record.create",
  resource: "example_record",
  effect: "deny",
};

function setup(
  options: {
    id?: number;
    own?: boolean;
    stale?: boolean;
    published?: boolean;
    preview?: boolean;
    failDraft?: boolean;
    delayedDraft?: boolean;
  } = {},
) {
  const calls: {
    name: string;
    input: Record<string, unknown>;
    key?: string;
  }[] = [];
  let resolveDraft: ((value: Response) => void) | undefined;
  let currentVersion = 10;
  const mock = vi.fn(async (url: string, init?: RequestInit) => {
    const name = decodeURIComponent(url.split("/")[3]);
    const envelope = JSON.parse(String(init?.body));
    calls.push({ name, input: envelope.input, key: envelope.idempotencyKey });
    if (name === "identity.policy.draft" && options.delayedDraft)
      return new Promise<Response>((resolve) => {
        resolveDraft = resolve;
      });
    if (name === "identity.policy.draft" && options.failDraft)
      return Response.json({ status: "failed", errorCode: "unavailable" });
    let data: unknown;
    switch (name) {
      case "identity.policy.history":
        data = {
          version: currentVersion,
          items: [
            {
              id: 10,
              baseVersion: 0,
              reason: "Active restrictions",
              published: true,
              authorId: 99,
              authoredByMe: false,
            },
          ],
        };
        break;
      case "identity.policy.preview":
        data = {
          currentVersion: 10,
          proposedVersion: envelope.input.id,
          baseVersion: 10,
          reason: "Review candidate",
          stale: options.stale ?? false,
          published: options.published ?? false,
          authoredByMe: options.own ?? false,
          proposedRules: [rule],
          additions: [rule],
          removals: [],
        };
        break;
      case "identity.policy.simulate":
        data = {
          currentVersion: 10,
          proposedVersion: envelope.input.id,
          currentAllowed: true,
          currentReason: "allowed",
          proposedAllowed: false,
          proposedReason: "policy_deny",
        };
        break;
      case "identity.policy.check":
        data = { issues: [{ rule: 2, code: "conflict" }] };
        break;
      case "identity.policy.searchContexts":
        data = {
          items: [
            {
              accountId: 31,
              fullName: "Lin Chen",
              username: "lin",
            },
          ],
        };
        break;
      default:
        data = { id: 11 };
    }
    return Response.json({ status: "succeeded", data });
  });
  vi.stubGlobal("fetch", mock);
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const tree = () => (
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <MemoryRouter
          initialEntries={[
            options.id
              ? `/iam-governance?policy=${options.id}`
              : "/iam-governance",
          ]}
        >
          <PolicyPanel
            props={{}}
            readOnly={options.preview ?? false}
            emit={vi.fn()}
          />
        </MemoryRouter>
      </QueryClientProvider>
    </I18nextProvider>
  );
  const view = render(tree());
  return {
    calls,
    client,
    user: userEvent.setup(),
    advanceVersion: async () => {
      currentVersion = 20;
      await client.invalidateQueries({ queryKey: ["iam.policy"] });
    },
    changeContext: () => {
      actor.id = 8;
      view.rerender(tree());
    },
    resolveDraft: () =>
      resolveDraft?.(Response.json({ status: "succeeded", data: { id: 11 } })),
  };
}

it("never reads or writes from preview or an unauthorized context", async () => {
  setup({ preview: true });
  expect(fetch).not.toHaveBeenCalled();
});

it("checks conflicting rules without saving a draft", async () => {
  const { user, calls } = setup();
  await user.click(await screen.findByRole("button", { name: "New draft" }));
  const rules = await screen.findByRole("textbox", {
    name: "Proposed rules",
  });
  await user.clear(rules);
  await user.paste(JSON.stringify([rule, rule]));
  await user.click(screen.getByRole("button", { name: "Check rules" }));
  expect(await screen.findByText(/Rule 2.*contradicts/)).toBeVisible();
  expect(
    calls.find((call) => call.name === "identity.policy.check")?.input,
  ).toEqual({ rules: [rule, rule] });
  expect(calls.some((call) => call.name === "identity.policy.draft")).toBe(
    false,
  );
});

it("searches for an active account and department and simulates that context", async () => {
  const { user, calls } = setup({ id: 11 });
  await user.type(
    await screen.findByRole("combobox", {
      name: "Simulation account and department",
    }),
    "Lin",
  );
  const candidate = await screen.findByRole("option", {
    name: /Lin Chen.*lin/,
  });
  await user.click(candidate);
  await user.click(
    screen.getByRole("button", { name: "Simulate authorization" }),
  );
  await screen.findByText(/policy_deny/);
  expect(
    calls.find((call) => call.name === "identity.policy.searchContexts")?.input,
  ).toEqual({ keyword: "Lin" });
  expect(
    calls.find((call) => call.name === "identity.policy.simulate")?.input,
  ).toEqual({
    id: 11,
    accountId: 31,
    action: rule.action,
    resource: rule.resource,
  });
});

it("keeps the original base version when history refreshes during editing", async () => {
  const { user, calls, advanceVersion } = setup();
  await user.click(await screen.findByRole("button", { name: "New draft" }));
  await user.type(
    await screen.findByRole("textbox", { name: "Change reason" }),
    "Restrict records during review",
  );
  await advanceVersion();
  await user.click(screen.getByRole("button", { name: "Save draft" }));
  await screen.findByText("Version 11");
  expect(
    calls.find((call) => call.name === "identity.policy.draft")?.input
      .baseVersion,
  ).toBe(10);
});
it("does not query when the user lacks management access", () => {
  actor.permissions = [];
  setup();
  expect(fetch).not.toHaveBeenCalled();
});
it("starts from current rules, validates JSON and saves an immutable draft through Gateway", async () => {
  const { user, calls } = setup();
  await user.click(await screen.findByRole("button", { name: "New draft" }));
  const rules = await screen.findByRole("textbox", { name: "Proposed rules" });
  await waitFor(() =>
    expect(rules).toHaveValue(JSON.stringify([rule], null, 2)),
  );
  await user.type(
    screen.getByRole("textbox", { name: "Change reason" }),
    "Restrict records during review",
  );
  await user.clear(rules);
  await user.type(rules, "invalid");
  await user.click(screen.getByRole("button", { name: "Save draft" }));
  expect(
    await screen.findByText(
      "Enter a valid array of up to 100 structured authorization rules.",
    ),
  ).toBeVisible();
  expect(
    calls.filter((call) => call.name === "identity.policy.draft"),
  ).toHaveLength(0);
  await user.clear(rules);
  await user.paste("[]");
  await user.click(screen.getByRole("button", { name: "Save draft" }));
  await screen.findByText("Version 11");
  expect(
    calls.find((call) => call.name === "identity.policy.draft"),
  ).toMatchObject({
    input: {
      baseVersion: 10,
      reason: "Restrict records during review",
      rules: [],
    },
    key: expect.any(String),
  });
});
it.each([{ own: true }, { stale: true }])(
  "blocks publishing own or stale drafts: %o",
  async (flags) => {
    const { calls } = setup({ id: 11, ...flags });
    expect(
      await screen.findByRole("button", { name: "Approve and publish" }),
    ).toBeDisabled();
    expect(calls.some((call) => call.name === "identity.policy.publish")).toBe(
      false,
    );
  },
);
it("requires explicit confirmation to publish and simulates the current trusted context", async () => {
  const { user, calls } = setup({ id: 11 });
  await user.click(
    await screen.findByRole("button", { name: "Simulate authorization" }),
  );
  await screen.findByText(/policy_deny/);
  expect(
    calls.find((call) => call.name === "identity.policy.simulate")?.input,
  ).toMatchObject({
    id: 11,
    accountId: 7,
    action: rule.action,
    resource: rule.resource,
  });
  await user.click(screen.getByRole("button", { name: "Approve and publish" }));
  expect(calls.some((call) => call.name === "identity.policy.publish")).toBe(
    false,
  );
  await user.click(screen.getByRole("button", { name: "Confirm" }));
  expect(await screen.findByText("Policy published.")).toBeVisible();
  expect(
    calls.find((call) => call.name === "identity.policy.publish")?.input,
  ).toEqual({ id: 11 });
});
it("rolls back through a new draft and never publishes it automatically", async () => {
  const { user, calls } = setup({ id: 9, published: true });
  await user.click(
    await screen.findByRole("button", { name: "Draft a rollback" }),
  );
  await user.type(
    await screen.findByRole("textbox", { name: "Change reason" }),
    "Restore reviewed restrictions",
  );
  await user.click(screen.getByRole("button", { name: "Save draft" }));
  await screen.findByText("Active restrictions");
  expect(
    calls.find((call) => call.name === "identity.policy.rollback")?.input,
  ).toEqual({
    id: 9,
    baseVersion: 10,
    reason: "Restore reviewed restrictions",
  });
  expect(calls.some((call) => call.name === "identity.policy.publish")).toBe(
    false,
  );
});
it("retains the same idempotency key for a failed save with unchanged input", async () => {
  const { user, calls } = setup({ failDraft: true });
  await user.click(await screen.findByRole("button", { name: "New draft" }));
  await user.type(
    await screen.findByRole("textbox", { name: "Change reason" }),
    "Restrict records during review",
  );
  await user.click(screen.getByRole("button", { name: "Save draft" }));
  await screen.findByRole("alert");
  await user.click(screen.getByRole("button", { name: "Save draft" }));
  await waitFor(() =>
    expect(
      calls.filter((call) => call.name === "identity.policy.draft"),
    ).toHaveLength(2),
  );
  const writes = calls.filter((call) => call.name === "identity.policy.draft");
  expect(writes[0].key).toBe(writes[1].key);
});
it("discards a save callback after switching contexts", async () => {
  const { user, calls, changeContext, resolveDraft } = setup({
    delayedDraft: true,
  });
  await user.click(await screen.findByRole("button", { name: "New draft" }));
  await user.type(
    await screen.findByRole("textbox", { name: "Change reason" }),
    "Restrict records during review",
  );
  await user.click(screen.getByRole("button", { name: "Save draft" }));
  await waitFor(() =>
    expect(calls.some((call) => call.name === "identity.policy.draft")).toBe(
      true,
    ),
  );
  changeContext();
  resolveDraft();
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
  expect(screen.queryByText("Version 11")).not.toBeInTheDocument();
});
