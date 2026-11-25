import { afterEach, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryRouter, RouterProvider } from "react-router";
import { I18nextProvider } from "react-i18next";
import { i18n, changeLanguage } from "@/i18n";
import { ThemeProvider } from "@/app/providers/theme";
import type { SessionProfile } from "@/api/generated/models";
import { AuthBoundary, RequireSession } from "./layout";
import { FederationCallbackPage } from "./federation-callback";

const profile: SessionProfile = {
  tenantId: "tenant-a",
  csrfToken: "new-csrf",

  permissions: [],
  account: {
    id: 1,
    username: "alice",
    status: "active",
    isAdmin: false,
  },
};
afterEach(() => vi.unstubAllGlobals());

async function setup(
  signedIn: boolean,
  result: object,
  status = 200,
  protocol: "oidc" | "saml" = "oidc",
) {
  await changeLanguage("en");
  const calls: { url: string; init?: RequestInit }[] = [];
  let authenticated = signedIn;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      calls.push({ url, init });
      if (url === "/api/auth/me")
        return Response.json(
          authenticated ? profile : { code: "unauthenticated" },
          { status: authenticated ? 200 : 401 },
        );
      if (
        url ===
        (protocol === "saml"
          ? "/api/auth/federation/saml/complete"
          : "/api/auth/federation/complete")
      ) {
        if ("profile" in result && status === 200) authenticated = true;
        return Response.json(result, { status });
      }
      if (url.includes("/identity.externalIdentity.link/"))
        return Response.json({
          status: "succeeded",
          data: { linked: true, reviewRequired: false },
        });
      return Response.json([]);
    }),
  );
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const router = createMemoryRouter(
    [
      {
        element: <AuthBoundary />,
        children: [
          {
            path: `/login/${protocol}`,
            element: <FederationCallbackPage protocol={protocol} />,
          },
          {
            element: <RequireSession />,
            children: [{ path: "/", element: <h1>Verified workspace</h1> }],
          },
        ],
      },
    ],
    {
      initialEntries: [
        protocol === "saml"
          ? "/login/saml"
          : `/login/oidc?code=one-time-code&state=${"a".repeat(64)}`,
      ],
    },
  );
  render(
    <I18nextProvider i18n={i18n}>
      <ThemeProvider>
        <QueryClientProvider client={client}>
          <RouterProvider router={router} />
        </QueryClientProvider>
      </ThemeProvider>
    </I18nextProvider>,
  );
  await screen.findByRole("button", { name: "Complete sign-in" });
  await waitFor(() => expect(router.state.location.search).toBe(""));
  return { client, router, calls, user: userEvent.setup() };
}

it("removes callback secrets from the URL and requires explicit completion with the local factor", async () => {
  const { calls, client, user } = await setup(false, { profile });
  expect(calls.filter(({ url }) => url.endsWith("/complete"))).toHaveLength(0);
  await user.type(
    screen.getByRole("textbox", {
      name: "Authenticator code or recovery code",
    }),
    "123456",
  );
  await user.click(screen.getByRole("button", { name: "Complete sign-in" }));
  expect(
    await screen.findByRole("heading", { name: "Verified workspace" }),
  ).toBeVisible();
  const completions = calls.filter(({ url }) => url.endsWith("/complete"));
  expect(completions).toHaveLength(1);
  expect(JSON.parse(String(completions[0].init?.body))).toEqual({
    code: "one-time-code",
    state: "a".repeat(64),
    mfaCode: "123456",
  });
  client.clear();
});

it("requires the current account password before sending a verified external proof to Gateway", async () => {
  const { calls, client, user } = await setup(true, {
    linkProof: "b".repeat(64),
  });
  await user.click(screen.getByRole("button", { name: "Complete sign-in" }));
  await screen.findByRole("button", { name: "Verify password and link" });
  expect(
    calls.filter(({ url }) => url.includes("/identity.externalIdentity.link/")),
  ).toHaveLength(0);
  await user.type(
    screen.getByLabelText("Password", { exact: true }),
    "TestOnly!2026Secure",
  );
  await user.click(
    screen.getByRole("button", { name: "Verify password and link" }),
  );
  expect(await screen.findByRole("status")).toHaveTextContent(
    "The external identity is now linked",
  );
  const invocation = calls.find(({ url }) =>
    url.includes("/identity.externalIdentity.link/"),
  );
  expect(JSON.parse(String(invocation?.init?.body))).toMatchObject({
    input: { proof: "b".repeat(64), password: "TestOnly!2026Secure" },
    idempotencyKey: expect.any(String),
  });
  expect(new Headers(invocation?.init?.headers).get("X-CSRF-Token")).toBe(
    "new-csrf",
  );
  client.clear();
});

it("does not replay a failed one-time completion", async () => {
  const { calls, client, user } = await setup(
    false,
    { code: "unauthenticated" },
    401,
  );
  await user.click(screen.getByRole("button", { name: "Complete sign-in" }));
  expect(
    await screen.findByText(/This sign-in attempt has ended/),
  ).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "Complete sign-in" }),
  ).not.toBeInTheDocument();
  expect(calls.filter(({ url }) => url.endsWith("/complete"))).toHaveLength(1);
  client.clear();
});

it("completes SAML using its HttpOnly cookie and recovery factor without URL credentials", async () => {
  const { calls, client, user } = await setup(false, { profile }, 200, "saml");
  expect(calls.filter(({ url }) => url.endsWith("/complete"))).toHaveLength(0);
  await user.type(
    screen.getByRole("textbox", {
      name: "Authenticator code or recovery code",
    }),
    "c".repeat(32),
  );
  await user.click(screen.getByRole("button", { name: "Complete sign-in" }));
  expect(
    await screen.findByRole("heading", { name: "Verified workspace" }),
  ).toBeVisible();
  const completion = calls.filter(({ url }) => url.endsWith("/complete"));
  expect(completion).toHaveLength(1);
  expect(completion[0].url).toBe("/api/auth/federation/saml/complete");
  expect(JSON.parse(String(completion[0].init?.body))).toEqual({
    recoveryCode: "c".repeat(32),
  });
  expect(
    JSON.stringify(
      client
        .getQueryCache()
        .getAll()
        .map((query) => query.state.data),
    ),
  ).not.toContain("c".repeat(32));
  client.clear();
});

it("requires local ownership before linking a SAML identity", async () => {
  const { calls, client, user } = await setup(
    true,
    { linkProof: "d".repeat(64) },
    200,
    "saml",
  );
  await user.click(screen.getByRole("button", { name: "Complete sign-in" }));
  await screen.findByRole("button", { name: "Verify password and link" });
  expect(
    calls.some(({ url }) => url.includes("/identity.externalIdentity.link/")),
  ).toBe(false);
  await user.type(
    screen.getByLabelText("Password", { exact: true }),
    "TestOnly!2026Secure",
  );
  await user.click(
    screen.getByRole("button", { name: "Verify password and link" }),
  );
  expect(await screen.findByRole("status")).toHaveTextContent(
    "The external identity is now linked",
  );
  const invocation = calls.find(({ url }) =>
    url.includes("/identity.externalIdentity.link/"),
  );
  expect(JSON.parse(String(invocation?.init?.body))).toMatchObject({
    input: { proof: "d".repeat(64), password: "TestOnly!2026Secure" },
  });
  expect(
    JSON.stringify(
      client
        .getQueryCache()
        .getAll()
        .map((query) => query.state.data),
    ),
  ).not.toContain("d".repeat(64));
  client.clear();
});

it("ends a rejected SAML completion without replay", async () => {
  const { calls, client, user } = await setup(
    false,
    { code: "unauthenticated" },
    401,
    "saml",
  );
  await user.click(screen.getByRole("button", { name: "Complete sign-in" }));
  expect(
    await screen.findByText(/This sign-in attempt has ended/),
  ).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "Complete sign-in" }),
  ).not.toBeInTheDocument();
  expect(calls.filter(({ url }) => url.endsWith("/complete"))).toHaveLength(1);
  client.clear();
});
