import { invoke, resetPreferences } from "./helpers";
import type {
  AccountRecord,
  AdminSnapshot,
  LoginResult,
  TemporaryAccessRecord,
  TemporaryAccessWorkspace,
} from "../src/api/generated/models";
import {
  test,
  expect,
  type Page,
  type APIRequestContext,
} from "@playwright/test";

async function signIn(page: Page, heading?: string) {
  await page.goto("/login");
  await page
    .getByRole("textbox", { name: "Account", exact: true })
    .fill("admin");
  await page
    .getByLabel("Password", { exact: true })
    .fill("TestOnly!2026Secure");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  if (heading) {
    await expect(
      page.getByRole("heading", { name: heading, exact: true }),
    ).toBeVisible();
    return;
  }
  // Sign in lands on the default workbench, which depends on the account's
  // permissions, so wait for the console shell instead of a fixed page.
  await expect(
    page.getByRole("navigation", { name: "Main navigation" }),
  ).toBeVisible();
}
const diagnostics = new WeakMap<
  Page,
  { console: string[]; failed: string[] }
>();
test.afterEach(async ({ page }, testInfo) => {
  await testInfo.attach("console-network-diagnostics", {
    body: JSON.stringify(diagnostics.get(page)),
    contentType: "application/json",
  });
});
test.beforeEach(async ({ page, request }) => {
  const response = await request.post("/api/auth/login", {
    headers: { Origin: "http://127.0.0.1:5179" },
    data: { username: "admin", password: "TestOnly!2026Secure" },
  });
  expect(response.ok()).toBeTruthy();
  const session: LoginResult = await response.json();
  const current = session.profile;
  await resetPreferences(request, {
    Origin: "http://127.0.0.1:5179",
    "X-CSRF-Token": current.csrfToken,
  });
  const diagnostic = { console: [] as string[], failed: [] as string[] };
  diagnostics.set(page, diagnostic);
  page.on("console", (message) => {
    if (message.type() === "error")
      diagnostic.console.push(`${message.location().url}: ${message.text()}`);
  });
  page.on("requestfailed", (request) =>
    diagnostic.failed.push(
      `${request.method()} ${request.url()} ${request.failure()?.errorText}`,
    ),
  );
  await page.addInitScript(() => {
    localStorage.setItem("baseline.language", "en");
  });
});
test("session protection, compact login, theme persistence and logout @smoke", async ({
  page,
}, testInfo) => {
  test.setTimeout(180000);
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto("/accounts");
  await expect(page).toHaveURL(/\/login$/);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page.getByRole("alert").first()).toBeVisible();
  await expect(page.getByLabel("Password", { exact: true })).toHaveAttribute(
    "type",
    "password",
  );
  await signIn(page, "Account management");
  await expect(page).toHaveURL(/\/accounts$/);
  await page.getByRole("link", { name: "Statistics", exact: true }).click();
  await expect(page).toHaveURL(/\/analytics$/);
  await expect(
    page.getByRole("heading", { name: "Data statistics", exact: true }),
  ).toBeVisible();
  await expect(page.locator("html")).toHaveClass(/dark/);
  await page.getByRole("button", { name: "Switch appearance" }).click();
  await expect(page.locator("html")).not.toHaveClass(/dark/);
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "Data statistics", exact: true }),
  ).toBeVisible();
  await expect(page.locator("html")).not.toHaveClass(/dark/);
  await page.locator(".sidebar-profile").click();
  await page.getByRole("menuitem", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/login$/);
  await page.goto("/roles");
  await expect(
    page.getByRole("heading", { name: "Welcome back" }),
  ).toBeVisible();
  expect(errors).toEqual([]);
  await testInfo.attach("page-errors", {
    body: JSON.stringify(errors),
    contentType: "application/json",
  });
});
test("organization, department, role and account changes persist through real API", async ({
  page,
}, testInfo) => {
  // This host's installed Chrome has ~3s per operation; the full cross-page CRUD journey is bounded separately.
  test.setTimeout(300000);
  const unique = Date.now().toString(36);
  const enterpriseName = `Acceptance Enterprise ${unique}`;
  const departmentName = `Acceptance Team ${unique}`;
  const roleName = `Acceptance Reader ${unique}`;
  const accountName = `Acceptance User ${unique}`;
  const updatedName = `Acceptance Updated ${unique}`;
  const errors: string[] = [];
  const responses: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("response", (response) => {
    // Writes go through the capability gateway now, so the console traffic is a
    // mix of session/snapshot reads under /api/admin/ and capability invocations.
    const url = response.url();
    if (url.includes("/api/admin/") || url.includes("/api/capabilities/"))
      responses.push(
        `${response.status()} ${response.request().method()} ${url}`,
      );
  });
  try {
    await signIn(page);
    await page
      .getByRole("link", { name: "Organizations", exact: true })
      .click();
    await page
      .getByRole("button", { name: "Create organization", exact: true })
      .click();
    let dialog = page.getByRole("dialog");
    await dialog.getByLabel("Name", { exact: true }).fill(enterpriseName);
    await dialog
      .getByLabel("Code", { exact: true })
      .fill(`acceptance-${unique}-enterprise`);
    await dialog.getByRole("button", { name: "Save changes" }).click();
    await expect(dialog).not.toBeVisible();
    await expect(
      page.getByRole("row", { name: new RegExp(enterpriseName) }),
    ).toBeVisible();

    // Departments now live in the second tab of Organization management.
    await page
      .getByRole("button", { name: "Departments", exact: true })
      .click();
    await expect(
      page.getByRole("heading", {
        name: "Organization management",
        exact: true,
      }),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "Create department", exact: true })
      .click();
    dialog = page.getByRole("dialog");
    await dialog.getByLabel("Name", { exact: true }).fill(departmentName);
    await dialog
      .getByLabel("Code", { exact: true })
      .fill(`acceptance-${unique}-team`);
    await dialog
      .getByLabel("Company", { exact: true })
      .selectOption({ label: "Horizon Technology" });
    await dialog.getByRole("button", { name: "Save changes" }).click();
    await expect(dialog).not.toBeVisible();
    await expect(
      page.getByRole("row", { name: new RegExp(departmentName) }),
    ).toBeVisible();

    // Role maintenance now lives in the third tab of Permission management.
    await page.getByRole("link", { name: "Permissions", exact: true }).click();
    await page.getByRole("button", { name: "Roles", exact: true }).click();
    await page
      .getByRole("button", { name: "Create role", exact: true })
      .click();
    dialog = page.getByRole("dialog");
    await dialog.getByLabel("Name", { exact: true }).fill(roleName);
    await dialog
      .getByLabel("Code", { exact: true })
      .fill(`acceptance-${unique}-reader`);
    await dialog
      .getByRole("checkbox", { name: "Accounts View", exact: true })
      .check();
    await dialog
      .getByRole("combobox", { name: "Data scope", exact: true })
      .selectOption("DEPARTMENT");
    await dialog.getByRole("button", { name: "Save changes" }).click();
    await expect(dialog).not.toBeVisible();
    await expect(
      page.getByRole("heading", { name: roleName, exact: true }),
    ).toBeVisible();

    await page.getByRole("link", { name: /^Accounts/ }).click();
    await page
      .getByRole("button", { name: "Create account", exact: true })
      .click();
    dialog = page.getByRole("dialog");
    await dialog.getByLabel("Full name", { exact: true }).fill(accountName);
    await dialog
      .getByLabel("Account", { exact: true })
      .fill(`acceptance.${unique}`);
    await dialog
      .getByLabel("Company", { exact: true })
      .selectOption({ label: "Horizon Technology" });
    await dialog
      .getByLabel("Department", { exact: true })
      .selectOption({ label: departmentName });
    await dialog
      .getByLabel("Initial password", { exact: true })
      .fill("Acceptance!2026Secure");
    await dialog
      .getByRole("button", { name: "Departments & permissions", exact: true })
      .click();
    await dialog.getByRole("checkbox", { name: new RegExp(roleName) }).check();
    await dialog
      .getByRole("group", { name: "Accounts View", exact: true })
      .getByRole("button", { name: "Allow", exact: true })
      .click();
    await dialog
      .getByRole("combobox", { name: "Data scope", exact: true })
      .selectOption("OWN");
    await dialog.getByRole("button", { name: "Save changes" }).click();
    await expect(dialog).not.toBeVisible();
    await expect(
      page.getByRole("row", { name: new RegExp(accountName) }),
    ).toBeVisible();
    await page.reload();
    const row = page.getByRole("row", { name: new RegExp(accountName) });
    await expect(row).toBeVisible();
    await row.getByRole("button", { name: "Actions" }).click();
    await page.getByRole("menuitem", { name: "Edit", exact: true }).click();
    dialog = page.getByRole("dialog");
    await dialog.getByLabel("Full name", { exact: true }).fill(updatedName);
    await dialog
      .getByRole("button", { name: "Departments & permissions", exact: true })
      .click();
    await expect(
      dialog.getByRole("combobox", { name: "Data scope", exact: true }),
    ).toHaveValue("OWN");
    await expect(
      dialog
        .getByRole("group", { name: "Accounts View", exact: true })
        .getByRole("button", { name: "Allow", exact: true }),
    ).toHaveAttribute("aria-pressed", "true");
    await dialog.getByRole("button", { name: "Save changes" }).click();
    await expect(dialog).not.toBeVisible();
    const updated = page.getByRole("row", { name: new RegExp(updatedName) });
    await expect(updated).toBeVisible();
    await updated.getByRole("button", { name: "Actions" }).click();
    await page.getByRole("menuitem", { name: "Delete", exact: true }).click();
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "Delete", exact: true })
      .click();
    await expect(updated).not.toBeVisible();
    expect(
      responses.some(
        (line) =>
          line.startsWith("200 POST") &&
          line.includes("identity.account.create"),
      ),
    ).toBe(true);
    expect(
      responses.some(
        (line) =>
          line.startsWith("200 POST") &&
          line.includes("identity.account.update"),
      ),
    ).toBe(true);
    expect(
      responses.some(
        (line) =>
          line.startsWith("200 POST") &&
          line.includes("identity.account.delete"),
      ),
    ).toBe(true);
    expect(errors).toEqual([]);
  } finally {
    await testInfo.attach("management-diagnostics", {
      body: JSON.stringify({ errors, responses }, null, 2),
      contentType: "application/json",
    });
  }
});

test("workspace panel owns scrolling and keeps its rounded shell stationary", async ({
  page,
}, testInfo) => {
  // Bound the real login, navigation, scroll and theme journey for this host's slow Chrome protocol.
  test.setTimeout(180000);
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.setViewportSize({ width: 1114, height: 937 });
  await signIn(page);
  await page.getByRole("link", { name: "Permissions", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Permission management", exact: true }),
  ).toBeVisible();
  const panel = page.locator(".console-main");
  const header = page.locator(".console-topbar");
  const content = page.getByRole("main");
  const panelBefore = await panel.boundingBox();
  const headerBefore = await header.boundingBox();
  expect(panelBefore).not.toBeNull();
  expect(headerBefore).not.toBeNull();
  const save = page.getByRole("button", { name: "Save changes", exact: true });
  await save.scrollIntoViewIfNeeded();
  await expect(save).toBeInViewport();
  await expect
    .poll(() => content.evaluate((element) => element.scrollTop))
    .toBeGreaterThan(0);
  expect(await page.evaluate(() => window.scrollY)).toBe(0);
  expect(await panel.boundingBox()).toEqual(panelBefore);
  expect((await header.boundingBox())?.y).toBe(headerBefore?.y);
  await page.screenshot({ path: testInfo.outputPath("workspace-dark.png") });
  await page
    .getByRole("button", { name: "Switch appearance", exact: true })
    .click();
  await expect(page.locator("html")).not.toHaveClass(/dark/);
  expect(await panel.boundingBox()).toEqual(panelBefore);
  expect((await header.boundingBox())?.y).toBe(headerBefore?.y);
  await expect(save).toBeInViewport();
  expect(errors).toEqual([]);
  await page.screenshot({ path: testInfo.outputPath("workspace-light.png") });
});

test("the sidebar opens the knowledge base for an administrator", async ({
  page,
}) => {
  await signIn(page);
  await page
    .getByRole("navigation", { name: "Main navigation" })
    .getByRole("link", { name: "Knowledge base", exact: true })
    .click();
  await expect(page).toHaveURL(/\/knowledge$/);
  await expect(
    page.getByRole("heading", { name: "Knowledge base", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("searchbox", { name: "Search titles and document text" }),
  ).toBeInViewport();
});

test("an account without resource permissions can request temporary access", async ({
  page,
  request,
}, testInfo) => {
  test.setTimeout(180000);
  const { account } = await temporaryApplicantFixture(request);
  await page.goto("/login");
  await page
    .getByRole("textbox", { name: "Account", exact: true })
    .fill(account.username);
  await page
    .getByLabel("Password", { exact: true })
    .fill("TestOnly!2026Secure");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(
    page.getByRole("navigation", { name: "Main navigation" }),
  ).toBeVisible();
  await expect(page.getByRole("link", { name: /^Accounts/ })).not.toBeVisible();
  await expect(
    page.getByRole("link", { name: "Knowledge base", exact: true }),
  ).toHaveCount(0);
  await expect(
    page
      .getByRole("navigation")
      .getByRole("link", { name: "Temporary access", exact: true }),
  ).toHaveCount(0);
  await page
    .getByRole("banner")
    .getByRole("link", { name: "Request temporary access", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Request temporary access", exact: true })
    .click();
  const dialog = page.getByRole("dialog");
  await dialog
    .getByLabel("Reason for request", { exact: true })
    .fill("Investigate a scoped support incident");
  await dialog
    .getByRole("combobox", { name: "Validity period", exact: true })
    .selectOption("120");
  await dialog
    .getByRole("combobox", { name: "Data scope", exact: true })
    .selectOption("ALL");
  await dialog
    .getByRole("checkbox", { name: "Accounts View", exact: true })
    .check();
  await expect(
    dialog.getByRole("checkbox", {
      name: "Temporary access approval",
      exact: true,
    }),
  ).not.toBeVisible();
  await dialog.screenshot({
    path: testInfo.outputPath("temporary-request-form.png"),
  });
  await dialog
    .getByRole("button", { name: "Submit request", exact: true })
    .click();
  await expect(dialog).not.toBeVisible();
  await expect(
    page.getByRole("row", { name: new RegExp(account.fullName) }),
  ).toContainText("Pending");
  const response = await page.request.get("/api/admin/temporary-access");
  expect(response.status()).toBe(200);
  const data = (await response.json()) as TemporaryAccessWorkspace;
  expect(data.canReview).toBe(false);
  expect(data.requests[0].requestedMinutes).toBe(120);
  expect(data.requests[0].requestedGrants).toEqual([
    {
      permission: "accounts:read",
      effect: "allow",
      scope: "ALL",
      departmentIds: [],
    },
  ]);
  await testInfo.attach("temporary-request-id", {
    body: String(data.requests[0].id),
    contentType: "text/plain",
  });
});
test("approver amends temporary authorization and revokes it through the UI", async ({
  page,
  request,
  playwright,
}, testInfo) => {
  test.setTimeout(240000);
  const { account, adminAccount } = await temporaryApplicantFixture(request);
  const applicant = await playwright.request.newContext({
    baseURL: "http://127.0.0.1:5179",
  });
  try {
    const login = await applicant.post("/api/auth/login", {
      headers: { Origin: "http://127.0.0.1:5179" },
      data: { username: account.username, password: "TestOnly!2026Secure" },
    });
    expect(login.status()).toBe(200);
    const loginResult = (await login.json()) as LoginResult;
    const applicantProfile = loginResult.profile;
    const created = await applicant.post("/api/admin/temporary-access", {
      headers: {
        Origin: "http://127.0.0.1:5179",
        "X-CSRF-Token": applicantProfile.csrfToken,
      },
      data: {
        reason: "Need access to investigate an incident",
        minutes: 120,
        grants: [
          {
            permission: "accounts:read",
            effect: "allow",
            scope: "ALL",
          },
        ],
      },
    });
    expect(created.status()).toBe(200);
    const record = (await created.json()) as TemporaryAccessRecord;
    await signIn(page);
    await page.getByRole("link", { name: "Permissions", exact: true }).click();
    await expect(
      page.locator("th").getByText("Administration", { exact: true }),
    ).toHaveAttribute("rowspan", "4");
    await page
      .getByRole("banner")
      .getByRole("link", { name: "Request temporary access", exact: true })
      .click();
    await page
      .getByRole("button", { name: "Approval inbox", exact: true })
      .click();
    const row = page.getByRole("row", {
      name: new RegExp(account.username.replace(".", "\\.")),
    });
    await row
      .getByRole("button", { name: "Review request", exact: true })
      .click();
    let dialog = page.getByRole("dialog");
    await dialog
      .getByRole("combobox", { name: "Validity period", exact: true })
      .selectOption("30");
    await dialog
      .getByRole("combobox", { name: "Data scope", exact: true })
      .selectOption("OWN");
    await dialog
      .getByLabel("Review notes", { exact: true })
      .fill("Own records for 30 minutes only");
    await dialog.screenshot({
      path: testInfo.outputPath("temporary-approval-form.png"),
    });
    await dialog
      .getByRole("button", { name: "Submit decision", exact: true })
      .click();
    await expect(dialog).not.toBeVisible();
    await expect(row).toContainText("Active");
    const workspaceResponse = await applicant.get(
      "/api/admin/temporary-access",
    );
    expect(workspaceResponse.status()).toBe(200);
    const workspace =
      (await workspaceResponse.json()) as TemporaryAccessWorkspace;
    const approved = workspace.requests.find((item) => item.id === record.id);
    expect(approved?.requestedMinutes).toBe(120);
    expect(approved?.requestedGrants[0].scope).toBe("ALL");
    expect(approved?.approvedMinutes).toBe(30);
    expect(approved?.approvedGrants[0].scope).toBe("OWN");
    const visibleResponse = await applicant.get("/api/admin/snapshot");
    expect(visibleResponse.status()).toBe(200);
    const snapshot = (await visibleResponse.json()) as AdminSnapshot;
    expect(snapshot.accounts.some((item) => item.id === account.id)).toBe(true);
    expect(snapshot.accounts.some((item) => item.id === adminAccount.id)).toBe(
      false,
    );
    await row
      .getByRole("button", { name: "Revoke access", exact: true })
      .click();
    dialog = page.getByRole("dialog");
    await dialog
      .getByRole("button", { name: "Revoke access", exact: true })
      .click();
    await expect(dialog).not.toBeVisible();
    await expect(row).toContainText("Revoked");
    const revokedResponse = await applicant.get("/api/admin/snapshot");
    expect(revokedResponse.status()).toBe(200);
    expect(((await revokedResponse.json()) as AdminSnapshot).accounts).toEqual(
      [],
    );
  } finally {
    await applicant.dispose();
  }
});
async function temporaryApplicantFixture(request: APIRequestContext) {
  const login = await request.post("/api/auth/login", {
    headers: { Origin: "http://127.0.0.1:5179" },
    data: { username: "admin", password: "TestOnly!2026Secure" },
  });
  expect(login.ok()).toBeTruthy();
  const { profile } = (await login.json()) as LoginResult;
  const admin = profile;
  const adminAccount = admin.account;
  if (!adminAccount) throw new Error("Expected an administrator session");
  const suffix = Date.now().toString(36);
  const headers = {
    Origin: "http://127.0.0.1:5179",
    "X-CSRF-Token": admin.csrfToken,
  };
  const account = await invoke<AccountRecord>(
    request,
    headers,
    "identity.account.create",
    {
      username: `temporary.${suffix}`,
      fullName: `Temporary applicant ${suffix}`,
      initialStatus: "active",
      password: "TestOnly!2026Secure",
    },
  );
  const role = await invoke<{ id: number }>(
    request,
    headers,
    "identity.role.create",
    {
      name: `Temporary applicant ${suffix}`,
      code: `temporary-${suffix}`,
      description: "",
      status: "active",
      grants: [{ permission: "roles:read", effect: "allow", scope: "ALL" }],
    },
  );
  await invoke(request, headers, "identity.roleGrant.create", {
    accountId: account.id,
    roleId: role.id,
    minutes: 60,
    reason: "Enter the tenant to request temporary access",
  });
  return { account, admin, adminAccount };
}

test("the work session exposes tenant account authority without application profile fields", async ({
  request,
  playwright,
}) => {
  const { account, admin } = await temporaryApplicantFixture(request);
  const applicant = await playwright.request.newContext({
    baseURL: "http://127.0.0.1:5179",
  });
  try {
    const login = await applicant.post("/api/auth/login", {
      headers: { Origin: "http://127.0.0.1:5179" },
      data: { username: account.username, password: "TestOnly!2026Secure" },
    });
    expect(login.ok()).toBeTruthy();
    const { profile } = (await login.json()) as LoginResult;
    expect(profile.tenantId).toBe("");
    expect(profile.account).toBeUndefined();
    const selected = profile;
    expect(selected.tenantId).toBe(admin.tenantId);
    expect(selected.account).toEqual({
      id: account.id,
      username: account.username,
      status: "active",
      isAdmin: false,
    });
    expect(selected.permissions).toEqual(["roles:read"]);
    const current = await applicant.get("/api/auth/me");
    expect(current.ok()).toBeTruthy();
    expect(await current.json()).toMatchObject({
      tenantId: selected.tenantId,
      account: selected.account,
      csrfToken: selected.csrfToken,
    });
  } finally {
    await applicant.dispose();
  }
});

test("the built-in administrator cannot be suspended or revoked", async ({
  request,
}) => {
  const { admin, adminAccount } = await temporaryApplicantFixture(request);
  for (const operation of ["suspend", "revoke"]) {
    const response = await request.post(
      `/api/capabilities/identity.membership.${operation}/invocations`,
      {
        headers: {
          Origin: "http://127.0.0.1:5179",
          "X-CSRF-Token": admin.csrfToken,
        },
        data: {
          idempotencyKey: crypto.randomUUID(),
          input: {
            accountId: adminAccount.id,
            reason: "Try to change the protected tenant administrator",
          },
        },
      },
    );
    expect(response.status()).toBe(403);
  }
  const session = await request.get("/api/auth/me");
  expect(session.ok()).toBeTruthy();
  expect(await session.json()).toMatchObject({
    tenantId: admin.tenantId,
    account: { id: adminAccount.id, status: "active", isAdmin: true },
  });
});
