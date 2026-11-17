import { afterEach, beforeEach, it, expect, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryRouter, RouterProvider } from "react-router";
import { I18nextProvider } from "react-i18next";
import { i18n, changeLanguage } from "@/i18n";
import { ThemeProvider } from "@/app/providers/theme";
import type { SessionProfile } from "@/api/generated/models";
import { AuthBoundary, RequireSession } from "./layout";
import { LoginPage } from "./login";
import {
  isStudioPairingAvailable,
  pairStudioMcpAfterLogin,
} from "@/features/studio/studio-mcp-pairing";

vi.mock("@/features/studio/studio-mcp-pairing", () => ({
  isStudioPairingAvailable: vi.fn(() => true),
  pairStudioMcpAfterLogin: vi.fn(),
}));
beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(isStudioPairingAvailable).mockReturnValue(true);
});
const profile: SessionProfile = {
  tenantId: "t",
  csrfToken: "server-csrf",
  permissions: ["accounts:read"],
  activeDepartmentId: 1,
  account: {
    memberships: [],
    id: 1,
    username: "admin",
    fullName: "Administrator",
    email: "",
    status: "active",
    organizationId: 1,
    departmentId: 1,
    roleIds: [],
    overrides: [],
    isAdmin: true,
    createdAt: "2026-10-28T00:00:00Z",
    lastLogin: "",
  },
};
afterEach(() => vi.unstubAllGlobals());
function mountLogin() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const router = createMemoryRouter(
    [
      {
        element: <AuthBoundary />,
        children: [
          { path: "/login", element: <LoginPage /> },
          {
            element: <RequireSession />,
            children: [{ path: "/", element: <h1>Verified workspace</h1> }],
          },
        ],
      },
    ],
    { initialEntries: ["/login"] },
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
  return client;
}
it("validates login, keeps password hidden and opens the verified session", async () => {
  await changeLanguage("en");
  const mock = vi.fn(
    async (url: string) =>
      new Response(
        JSON.stringify(
          url === "/api/auth/login" ? { profile } : { code: "unauthenticated" },
        ),
        {
          status: url === "/api/auth/login" ? 200 : 401,
          headers: { "Content-Type": "application/json" },
        },
      ),
  );
  vi.stubGlobal("fetch", mock);
  const client = mountLogin();
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "Sign in" }));
  expect(screen.getAllByRole("alert").length).toBeGreaterThan(0);
  expect(screen.getByLabelText("Password", { exact: true })).toHaveAttribute(
    "type",
    "password",
  );
  await user.type(screen.getByRole("textbox", { name: "Account" }), "admin");
  await user.type(
    screen.getByLabelText("Password", { exact: true }),
    "TestOnly!2026Secure",
  );
  await user.click(screen.getByRole("button", { name: "Sign in" }));
  expect(
    await screen.findByRole("heading", { name: "Verified workspace" }),
  ).toBeVisible();
  expect(mock.mock.calls.some(([url]) => url === "/api/auth/login")).toBe(true);
  expect(pairStudioMcpAfterLogin).not.toHaveBeenCalled();
  client.clear();
});

it("uses the submitted password only after successful login when local DSH pairing is available", async () => {
  await changeLanguage("en");
  const permitted = {
    ...profile,
    permissions: [...profile.permissions, "studio:read"],
  };
  let attempts = 0;
  const mock = vi.fn(async (url: string) => {
    const loggedIn = url === "/api/auth/login" && ++attempts > 1;
    return new Response(
      JSON.stringify(
        loggedIn ? { profile: permitted } : { code: "unauthenticated" },
      ),
      {
        status: loggedIn ? 200 : 401,
        headers: { "Content-Type": "application/json" },
      },
    );
  });
  vi.stubGlobal("fetch", mock);
  vi.mocked(pairStudioMcpAfterLogin).mockRejectedValueOnce(
    new Error("DSH not running"),
  );
  const client = mountLogin();
  const user = userEvent.setup();
  expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  await user.type(screen.getByRole("textbox", { name: "Account" }), "admin");
  await user.type(
    screen.getByLabelText("Password", { exact: true }),
    "test-only-password",
  );
  await user.click(screen.getByRole("button", { name: "Sign in" }));
  await screen.findByRole("alert");
  expect(pairStudioMcpAfterLogin).not.toHaveBeenCalled();

  await user.click(screen.getByRole("button", { name: "Sign in" }));
  expect(
    await screen.findByRole("heading", { name: "Verified workspace" }),
  ).toBeVisible();
  await waitFor(() =>
    expect(pairStudioMcpAfterLogin).toHaveBeenCalledExactlyOnceWith(
      permitted,
      "test-only-password",
      expect.any(Function),
    ),
  );
  client.clear();
});

it("persists an explicit appearance choice", async () => {
  const user = userEvent.setup();
  localStorage.setItem("baseline.theme", "dark");
  function Probe() {
    const theme = useThemeForTest();
    return <button onClick={theme.toggle}>{theme.theme}</button>;
  }
  render(
    <ThemeProvider>
      <Probe />
    </ThemeProvider>,
  );
  await user.click(screen.getByRole("button", { name: "dark" }));
  expect(document.documentElement).not.toHaveClass("dark");
  expect(localStorage.getItem("baseline.theme")).toBe("light");
  localStorage.removeItem("baseline.theme");
});
import { useTheme as useThemeForTest } from "@/app/providers/theme";
