import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nextProvider } from "react-i18next";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import type { ComponentRendererProps } from "@/features/application-runtime/protocol/component-registry";
import { i18n, changeLanguage } from "@/i18n";
import { PolicyPanel } from "./policy-panel";

const actor = vi.hoisted(() => ({ id: 7, allowed: true, binding: "" }));
vi.mock("./auth", () => ({
  useSession: () => ({
    tenantId: "tenant-a",
    account: { id: actor.id },

    authorizationVersion: "v1",
    identityBindingId: actor.binding,
  }),
  usePermission: () => actor.allowed,
}));
vi.mock("@/features/governance/runtime", () => ({
  useConfiguration: () => ({
    value: (_key: string, fallback: unknown) => fallback,
  }),
}));

beforeEach(async () => {
  actor.id = 7;
  actor.allowed = true;
  actor.binding = "";
  await changeLanguage("en");
});
afterEach(() => vi.unstubAllGlobals());

function setup(
  options: {
    preview?: boolean;
    delayed?: boolean;
    empty?: boolean;
    failure?: boolean;
  } = {},
) {
  const calls: {
    input: Record<string, unknown>;
    signal?: AbortSignal | null;
  }[] = [];
  let finish: ((response: Response) => void) | undefined;
  const data = {
    asOf: "2026-11-20T04:00:00Z",
    windowHours: 24,
    authenticationFailures: 3,
    authorizationDenials: 9,
    privilegedGrantTotal: 1,
    privilegedGrants: options.empty
      ? []
      : [
          {
            id: "private-grant-id",
            username: "review-member",
            roleName: "Security manager",
            expiresAt: "2026-11-21T00:00:00Z",
          },
        ],
    repeatedDenials: options.empty
      ? []
      : [
          {
            action: "example.record.archive",
            reason: "permission",
            occurrences: 5,
            affectedAccounts: 2,
          },
        ],
  };
  const fetch = vi.fn(async (url: string, init?: RequestInit) => {
    expect(url).toContain("identity.governance.report/invocations");
    calls.push({
      input: JSON.parse(String(init?.body)).input,
      signal: init?.signal,
    });
    if (options.delayed)
      return new Promise<Response>((resolve) => {
        finish = resolve;
      });
    if (options.failure)
      return Response.json({ status: "failed", errorCode: "unavailable" });
    return Response.json({ status: "succeeded", data });
  });
  vi.stubGlobal("fetch", fetch);
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const tree = () => (
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={["/iam-governance?tab=risks"]}>
          <PolicyPanel
            {...({ readOnly: options.preview } as ComponentRendererProps)}
          />
        </MemoryRouter>
      </QueryClientProvider>
    </I18nextProvider>
  );
  const view = render(tree());
  return {
    ...view,
    calls,
    client,
    fetch,
    contextChanged: () => {
      actor.id = 8;
      view.rerender(tree());
    },
    finish: () => finish?.(Response.json({ status: "succeeded", data })),
  };
}

it.each(["preview", "permission", "binding"])(
  "does not read risk signals when %s is restricted",
  (gate) => {
    if (gate === "permission") actor.allowed = false;
    if (gate === "binding") actor.binding = "bound";
    const view = setup({ preview: gate === "preview" });
    expect(view.fetch).not.toHaveBeenCalled();
    view.unmount();
    view.client.clear();
  },
);

it("opens the report through Gateway, shows authorized names and changes the observation window", async () => {
  const view = setup();
  const user = userEvent.setup();
  expect(await screen.findByText("review-member")).toBeVisible();
  expect(screen.getByText("Security manager")).toBeVisible();
  expect(screen.queryByText("private-grant-id")).not.toBeInTheDocument();
  expect(view.calls[0].input).toEqual({ windowHours: 24 });
  await user.click(
    screen.getByRole("combobox", { name: "Observation window" }),
  );
  await user.click(await screen.findByRole("option", { name: "Last 7 days" }));
  await waitFor(() =>
    expect(view.calls.at(-1)?.input).toEqual({ windowHours: 168 }),
  );
  await user.click(screen.getByRole("button", { name: "Refresh" }));
  await waitFor(() => expect(view.calls).toHaveLength(3));
  view.unmount();
  view.client.clear();
});

it("cancels a report for the old identity and does not show its delayed response", async () => {
  const view = setup({ delayed: true });
  await waitFor(() => expect(view.calls).toHaveLength(1));
  const old = view.calls[0];
  view.contextChanged();
  await waitFor(() => expect(view.calls).toHaveLength(2));
  expect(old.signal?.aborted).toBe(true);
  view.unmount();
  await act(async () => view.finish());
  expect(screen.queryByText("review-member")).not.toBeInTheDocument();
  view.client.clear();
});

it.each(["empty", "failure"])(
  "renders the %s state without inventing risk rows",
  async (state) => {
    const view = setup({
      empty: state === "empty",
      failure: state === "failure",
    });
    await waitFor(() => expect(view.calls).toHaveLength(1));
    if (state === "empty") {
      await screen.findByText("Recorded authentication failures");
      expect(screen.queryByText("review-member")).not.toBeInTheDocument();
    } else await screen.findByRole("alert");
    view.unmount();
    view.client.clear();
  },
);
