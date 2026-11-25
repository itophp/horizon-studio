import { afterEach, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nextProvider } from "react-i18next";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { i18n, changeLanguage } from "@/i18n";
import { SecurityMFA } from "./security-mfa";
import { SecurityPanel } from "./security-panel";

afterEach(() => vi.unstubAllGlobals());

it("keeps enrollment and recovery secrets in memory and requires an activation code", async () => {
  await changeLanguage("en");
  let enabled = false;
  const inputs: { name: string; input: Record<string, unknown> }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const name = decodeURIComponent(url.split("/")[3]);
      const payload = JSON.parse(String(init?.body));
      inputs.push({ name, input: payload.input });
      if (name === "identity.mfa.status")
        return Response.json({
          status: "succeeded",
          data: {
            enabled,
            pending: false,
            recoveryRemaining: enabled ? 10 : 0,
          },
        });
      if (name === "identity.mfa.enroll")
        return Response.json({
          status: "succeeded",
          data: {
            secret: "PRIVATESETUPKEY",
            uri: "otpauth://totp/fixture",
            expiresAt: "2026-11-20T00:00:00Z",
          },
        });
      if (name === "identity.mfa.activate") {
        enabled = true;
        return Response.json({
          status: "succeeded",
          data: { recoveryCodes: ["single-use-recovery"] },
        });
      }
      throw new Error("unexpected capability");
    }),
  );
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const view = render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <SecurityMFA queryKey={["security-test"]} />
      </QueryClientProvider>
    </I18nextProvider>,
  );
  const user = userEvent.setup();
  await user.click(
    await screen.findByRole("button", { name: "Set up authenticator" }),
  );
  await user.type(
    screen.getByLabelText("Password", { exact: true }),
    "TestOnly!2026Secure",
  );
  await user.click(screen.getByRole("button", { name: "Confirm" }));
  expect(await screen.findByText("PRIVATESETUPKEY")).toBeVisible();
  expect(
    inputs.find(({ name }) => name === "identity.mfa.enroll")?.input,
  ).toEqual({ password: "TestOnly!2026Secure" });
  expect(
    JSON.stringify(
      client
        .getQueryCache()
        .getAll()
        .map((query) => query.state.data),
    ),
  ).not.toContain("PRIVATESETUPKEY");
  await user.click(
    screen.getByRole("button", { name: "Activate authenticator" }),
  );
  expect(
    inputs.filter(({ name }) => name === "identity.mfa.activate"),
  ).toHaveLength(0);
  await user.type(
    screen.getByRole("textbox", { name: "Six-digit authenticator code" }),
    "123456",
  );
  await user.click(
    screen.getByRole("button", { name: "Activate authenticator" }),
  );
  expect(await screen.findByText("single-use-recovery")).toBeVisible();
  expect(screen.queryByText("PRIVATESETUPKEY")).not.toBeInTheDocument();
  expect(
    JSON.stringify(
      client
        .getQueryCache()
        .getAll()
        .map((query) => query.state.data),
    ),
  ).not.toContain("single-use-recovery");
  await user.click(
    screen.getByRole("button", { name: "I have saved the codes" }),
  );
  expect(screen.queryByText("single-use-recovery")).not.toBeInTheDocument();
  view.unmount();
  client.clear();
});

it("does not access personal security data or run actions in Runtime preview", async () => {
  await changeLanguage("en");
  const mock = vi.fn();
  vi.stubGlobal("fetch", mock);
  render(
    <I18nextProvider i18n={i18n}>
      <SecurityPanel props={{}} readOnly emit={mock} />
    </I18nextProvider>,
  );
  expect(mock).not.toHaveBeenCalled();
  expect(screen.queryByRole("button")).not.toBeInTheDocument();
});
