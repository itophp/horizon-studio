import { afterEach, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nextProvider } from "react-i18next";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { changeLanguage, i18n } from "@/i18n";
import { PlatformPrincipalDetail } from "./principal-detail";

afterEach(() => vi.unstubAllGlobals());

it("requires the reason before reading a principal and binds the security proof to the edited profile", async () => {
  await changeLanguage("en");
  const id = "fedcba98-7654-4321-9876-0123456789ab";
  const calls: { action: string; input: Record<string, unknown> }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const action = decodeURIComponent(url.split("/")[3]);
      const input = JSON.parse(String(init?.body)).input;
      calls.push({ action, input });
      if (action === "identity.principal.detail")
        return Response.json({
          status: "succeeded",
          data: {
            id,
            displayName: "Verified subject",
            kind: "human",
            status: "active",
            source: "platform",
            createdAt: "2026-11-21T00:00:00Z",
            riskLevel: "none",
            securityNotes: "",
            roles: [],
            auditEvents: [],
            logins: [],
            memberships: [],
            bindings: [],
            sessions: [],
          },
        });
      if (action === "identity.principal.reauthenticate")
        return Response.json({
          status: "succeeded",
          data: { proof: "p".repeat(64) },
        });
      if (action === "identity.principal.setSecurity")
        return Response.json({ status: "succeeded", data: { id } });
      throw new Error("Unexpected action " + action);
    }),
  );
  const client = new QueryClient();
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <PlatformPrincipalDetail
          tenant="tenant-a"
          id={id}
          queryKey={["platform"]}
          onBack={vi.fn()}
        />
      </QueryClientProvider>
    </I18nextProvider>,
  );
  expect(calls).toEqual([]);
  const user = userEvent.setup();
  await user.type(screen.getByLabelText("Change reason"), "Verify identity");
  await user.click(
    screen.getByRole("button", { name: "View principal details" }),
  );
  await screen.findByText("Verified subject");
  expect(calls[0].input).toEqual({
    targetTenantId: "tenant-a",
    principalId: id,
    reason: "Verify identity",
  });
  await user.click(
    screen.getByRole("button", { name: "Edit security profile" }),
  );
  await user.selectOptions(screen.getByLabelText("Risk level"), "high");
  await user.type(
    screen.getByLabelText("Security notes"),
    "Follow-up required",
  );
  await user.type(
    screen.getByLabelText("Change reason"),
    "Mark elevated access risk",
  );
  await user.type(
    screen.getByLabelText("Password", { exact: true }),
    "test-password",
  );
  await user.click(screen.getByRole("button", { name: "Confirm" }));
  await screen.findByRole("button", { name: "Edit security profile" });
  const fields = {
    targetTenantId: "tenant-a",
    target: id,
    riskLevel: "high",
    securityNotes: "Follow-up required",
    reason: "Mark elevated access risk",
  };
  expect(
    calls.find((call) => call.action === "identity.principal.reauthenticate")
      ?.input,
  ).toEqual({ ...fields, operation: "setSecurity", password: "test-password" });
  expect(
    calls.find((call) => call.action === "identity.principal.setSecurity")
      ?.input,
  ).toEqual({ ...fields, proof: "p".repeat(64) });
});
