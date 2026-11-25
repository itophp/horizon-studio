import { readFileSync } from "node:fs";
import { beforeEach, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nextProvider } from "react-i18next";
import { MemoryRouter } from "react-router";
import { i18n, changeLanguage } from "@/i18n";
import { DynamicApplicationRoute } from "../route";
import type { ApplicationV3 } from "./types";

const api = vi.hoisted(() => ({
  application: vi.fn(),
  read: vi.fn(),
  execute: vi.fn(),
}));

vi.mock("@/lib/capabilities", async (original) => ({
  ...(await original<typeof import("@/lib/capabilities")>()),
  runtimeApplication: api.application,
  runtimeRead: api.read,
  runtimeExecute: api.execute,
}));

vi.mock("@/features/identity/auth", () => ({
  useSession: () => ({
    tenantId: "tenant",
    account: { id: 1 },

    permissions: [
      "accounts:read",
      "accounts:create",
      "accounts:update",
      "accounts:delete",
      "organizations:read",
      "organizations:update",
      "departments:read",
      "roles:read",
      "roles:create",
      "access:manage",
    ],
  }),
}));

vi.mock("@/features/governance/runtime", () => ({
  useConfiguration: () => ({
    value: (_key: string, fallback: unknown) => fallback,
  }),
}));

type DeploymentApplication = {
  id: string;
  schemaVersion: string;
  metadataVersion: string;
  protocol?: ApplicationV3;
};

const applications = JSON.parse(
  readFileSync("../../docs/generated/runtime/applications.json", "utf8"),
) as DeploymentApplication[];

const organization = {
  id: 8,
  name: "Acme",
  code: "ACME",
  kind: "enterprise",
  parentId: 0,
  status: "active",
  createdAt: "2026-01-01T00:00:00Z",
};
const company = {
  ...organization,
  id: 9,
  name: "Acme Labs",
  kind: "company",
  parentId: 8,
};
const department = {
  id: 3,
  name: "Platform",
  code: "PLAT",
  organizationId: 9,
  parentId: 0,
  manager: "Alice",
  status: "active",
  createdAt: "2026-01-01T00:00:00Z",
};
const role = {
  id: 4,
  name: "Administrator",
  code: "ADMIN",
  description: "Full access",
  status: "active",
  memberCount: 2,
  grants: [],
  createdAt: "2026-01-01T00:00:00Z",
};
const adminOrganization = {
  id: organization.id,
  name: organization.name,
  code: organization.code,
  kind: organization.kind,
  status: organization.status,
};
const adminCompany = {
  ...adminOrganization,
  id: company.id,
  name: company.name,
  kind: company.kind,
};
const adminDepartment = {
  id: department.id,
  name: department.name,
  code: department.code,
  organizationId: department.organizationId,
  parentId: department.parentId,
  manager: department.manager,
  status: department.status,
  description: "",
};
const departmentOrganization = {
  id: organization.id,
  name: organization.name,
  kind: organization.kind,
};
const departmentCompany = {
  ...departmentOrganization,
  id: company.id,
  name: company.name,
  kind: company.kind,
};

const accountDepartment = {
  id: adminDepartment.id,
  name: adminDepartment.name,
  code: adminDepartment.code,
  organizationId: adminDepartment.organizationId,
  parentId: adminDepartment.parentId,
  manager: adminDepartment.manager,
  status: adminDepartment.status,
};

const adminRole = {
  id: role.id,
  name: role.name,
  code: role.code,
  status: role.status,
  grants: role.grants,
};

const account = {
  id: 1,
  username: "alice",
  fullName: "Alice Chen",
  email: "alice@example.test",
  status: "active",
  createdAt: "2026-01-01T00:00:00Z",
  organizationId: 9,
  memberships: [
    {
      organizationId: 9,
      departmentId: 3,
      departmentName: "Platform",
      roleIds: [4],
      overrides: [],
    },
  ],
  isAdmin: false,
};

function mount(path: string) {
  return render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider
        client={
          new QueryClient({ defaultOptions: { queries: { retry: false } } })
        }
      >
        <MemoryRouter initialEntries={[path]}>
          <DynamicApplicationRoute />
        </MemoryRouter>
      </QueryClientProvider>
    </I18nextProvider>,
  );
}

beforeEach(async () => {
  vi.clearAllMocks();
  await changeLanguage("en", false);
  api.execute.mockResolvedValue({ status: "succeeded" });
  api.read.mockImplementation(
    async (name: string, input: Record<string, unknown>) => {
      if (name === "application.runtime.list")
        return { items: applications.filter((item) => item.protocol) };
      if (name === "identity.accounts.admin.read")
        return {
          items: [account],
          organizations: [
            adminOrganization,
            adminCompany,
            { ...adminCompany, id: 10, name: "Other company" },
          ],
          departments: [accountDepartment],
          roles: [adminRole],
          catalog: [],
          total: 1,
        };
      if (name === "identity.organizations.read")
        return {
          items: [{ ...company, parentId: 0, description: "" }],
          total: 1,
        };
      if (name === "identity.departments.read")
        return {
          items: [adminDepartment],
          organizations: [departmentOrganization, departmentCompany],
          total: 1,
        };
      if (name === "identity.roles.read")
        return {
          items: [
            {
              id: role.id,
              name: role.name,
              code: role.code,
              description: role.description,
              status: role.status,
              memberCount: role.memberCount,
              grants: role.grants,
            },
          ],
          catalog: [
            {
              module: "identity",
              function: "accounts",
              action: "read",
              code: "accounts:read",
              scopeConfigurable: false,
            },
          ],
          total: 1,
        };
      if (name === "application.runtime.data.read") {
        const providerId = String(input.providerId);
        if (providerId === "protocol.organization.options")
          return { items: [organization, company], total: 2 };
        if (providerId === "protocol.department.options")
          return {
            items: [department],
            organizations: [departmentOrganization, departmentCompany],
            total: 1,
          };
        if (providerId === "protocol.role.options")
          return {
            items: [
              {
                id: role.id,
                name: role.name,
                code: role.code,
                description: role.description,
                status: role.status,
                memberCount: role.memberCount,
                grants: role.grants,
              },
            ],
            catalog: [],
            total: 1,
          };
      }
      return { items: [] };
    },
  );
});

it("renders the account console from the deployed V3 definition", async () => {
  const fixture = applications.find((item) => item.id === "accounts");
  api.application.mockResolvedValue(fixture);
  mount("/accounts");
  expect(await screen.findByText("Alice Chen")).toBeInTheDocument();
  expect(
    screen.getByRole("button", { name: /Create account/i }),
  ).toBeInTheDocument();
  expect(
    screen.queryByText("Platform · Administrator"),
  ).not.toBeInTheDocument();
});

it("creates an account without assigning a department or role", async () => {
  const user = userEvent.setup();
  const fixture = applications.find((item) => item.id === "accounts");
  api.application.mockResolvedValue(fixture);
  mount("/accounts");
  await user.click(
    await screen.findByRole("button", { name: /Create account/i }),
  );
  expect(await screen.findByRole("dialog")).toBeInTheDocument();
  expect(screen.getByLabelText("Full name")).toBeInTheDocument();
  expect(screen.queryByLabelText("Company")).not.toBeInTheDocument();
  expect(screen.queryByLabelText("Department")).not.toBeInTheDocument();
  await user.type(screen.getByLabelText("Account"), "bob");
  await user.type(screen.getByLabelText("Full name"), "Bob Chen");
  await user.type(
    screen.getByLabelText("Initial password"),
    "safe-password-123",
  );
  await user.click(screen.getByRole("button", { name: "Save changes" }));
  await waitFor(() => expect(api.execute).toHaveBeenCalled());
  expect(api.execute).toHaveBeenCalledWith(
    "identity.account.create",
    "1.0.0",
    {
      username: "bob",
      fullName: "Bob Chen",
      email: "",
      initialStatus: "active",
      password: "safe-password-123",
    },
    expect.any(String),
    expect.any(String),
    expect.anything(),
  );
});

it("shows a failed schema form submission in an error dialog", async () => {
  const user = userEvent.setup();
  api.execute.mockResolvedValue({ status: "failed", errorCode: "conflict" });
  const fixture = applications.find((item) => item.id === "accounts");
  api.application.mockResolvedValue(fixture);
  mount("/accounts");
  await user.click(
    await screen.findByRole("button", { name: /Create account/i }),
  );
  await user.type(await screen.findByLabelText("Full name"), "Bob Chen");
  await user.type(await screen.findByLabelText("Account"), "bob");
  await user.type(
    await screen.findByLabelText("Initial password"),
    "safe-password-123",
  );
  await user.click(screen.getByRole("button", { name: "Save changes" }));
  const errorDialog = await screen.findByRole("dialog", {
    name: "Action failed",
  });
  expect(errorDialog).toHaveTextContent(
    "The data changed or conflicts with an existing record. Refresh and try again.",
  );
});

it("requires a policy-length password", async () => {
  const user = userEvent.setup();
  api.application.mockResolvedValue(
    applications.find((item) => item.id === "accounts"),
  );
  mount("/accounts");
  await user.click(
    await screen.findByRole("button", { name: /Create account/i }),
  );
  await user.type(screen.getByLabelText("Full name"), "Bob Chen");
  await user.type(screen.getByLabelText("Account"), "bob");
  const password = screen.getByLabelText("Initial password");
  await user.type(password, "short");
  const save = screen.getByRole("button", { name: "Save changes" });
  await user.click(save);
  expect(
    await screen.findByText(
      /New passwords must contain at least 12 characters/,
    ),
  ).toBeInTheDocument();
  expect(api.execute).not.toHaveBeenCalled();

  await user.clear(password);
  await user.type(password, "TestOnly!2026Secure");
  await user.click(save);
  await waitFor(() => expect(api.execute).toHaveBeenCalledTimes(1));
});

it("renders organization and department views from one V3 page", async () => {
  const fixture = applications.find((item) => item.id === "organizations");
  api.application.mockResolvedValue(fixture);
  const view = mount("/organizations");
  expect((await screen.findAllByText("Acme Labs")).length).toBeGreaterThan(0);
  const departments = screen.getByRole("tab", { name: /Departments/i });
  await userEvent.click(departments);
  expect(departments).toHaveClass("active");
  expect(departments).toHaveAttribute("aria-selected", "true");
  expect(await screen.findByText("Platform")).toBeInTheDocument();
  expect(screen.queryByText("Companies")).not.toBeInTheDocument();
  view.unmount();
});

it("shows one editable organization without create or delete actions", async () => {
  api.application.mockResolvedValue(
    applications.find((item) => item.id === "organizations"),
  );
  mount("/organizations");
  const user = userEvent.setup();
  const table = within(await screen.findByRole("table"));
  expect(table.getAllByRole("row")).toHaveLength(2);
  expect(table.getByText("Acme Labs")).toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Create organization" }),
  ).toBeNull();
  await user.click(table.getByRole("button", { name: "Actions" }));
  const edit = await screen.findByRole("menuitem", { name: "Edit" });
  expect(screen.queryByRole("menuitem", { name: "Delete" })).toBeNull();
  await user.click(edit);
  const dialog = within(await screen.findByRole("dialog"));
  expect(dialog.queryByLabelText("Parent enterprise")).toBeNull();
  const name = dialog.getByLabelText("Name");
  await user.clear(name);
  await user.type(name, "Acme Workspace");
  await user.click(dialog.getByRole("button", { name: "Save changes" }));
  await waitFor(() =>
    expect(api.execute).toHaveBeenCalledWith(
      "identity.organization.update",
      "1.0.0",
      {
        id: company.id,
        name: "Acme Workspace",
        code: company.code,
        kind: "company",
        parentId: 0,
        status: "active",
        description: "",
      },
      expect.any(String),
      expect.any(String),
      expect.anything(),
    ),
  );
});

it("renders the V3 permissions role gallery and detail tabs", async () => {
  const fixture = applications.find((item) => item.id === "permissions");
  api.application.mockResolvedValue(fixture);
  mount("/permissions?type=roles");
  expect(await screen.findByText("Administrator")).toBeInTheDocument();
  expect(screen.getByRole("tab", { name: /^Roles$/i })).toHaveAttribute(
    "aria-selected",
    "true",
  );
});

it("renders the schema-declared role permission matrix", async () => {
  const fixture = applications.find((item) => item.id === "permissions");
  api.application.mockResolvedValue(fixture);
  mount("/permissions?type=role&id=4");
  expect((await screen.findAllByText("Administrator")).length).toBeGreaterThan(
    0,
  );
  expect(document.querySelector(".permission-matrix")).toBeInTheDocument();
  expect(
    document.querySelectorAll(".permission-hierarchy tbody tr").length,
  ).toBeGreaterThan(0);
});
