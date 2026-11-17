import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nextProvider } from "react-i18next";
import { i18n } from "@/i18n";
import { expect, it, vi } from "vitest";
import { filesContent, filesUpload } from "@/lib/capabilities";
import { studio } from "./api";
import { RequirementAttachments } from "./requirement-attachments";

vi.mock("@/features/administration/auth", () => ({
  useSession: () => ({
    tenantId: "tenant",
    account: { id: 1 },
    activeDepartmentId: undefined,
    permissions: ["studio:read", "studio:write", "files:create", "files:read"],
  }),
}));
vi.mock("@/lib/capabilities", () => ({
  filesUpload: vi.fn(async () => ({
    status: 200,
    data: {
      id: 5,
      name: "note.txt",
      mime: "text/plain",
      size: 5,
      purpose: "attachment",
      link: "",
    },
  })),
  filesContent: vi.fn(async () => ({
    status: 200,
    data: { name: "spec.pdf", mime: "application/pdf", content: btoa("hello") },
  })),
}));
vi.mock("@/features/governance/shared", () => ({
  downloadContent: vi.fn(),
}));
vi.mock("./api", async (load) => ({
  ...(await load<typeof import("./api")>()),
  studio: {
    requirementAttachments: vi.fn(async () => [
      {
        fileId: 3,
        name: "spec.pdf",
        mime: "application/pdf",
        size: 2048,
      },
    ]),
    addRequirementAttachment: vi.fn(async () => ({ fileId: 5 })),
    removeRequirementAttachment: vi.fn(async () => ({ fileId: 3 })),
  },
}));

it("lists, downloads, unbinds and uploads requirement attachments", async () => {
  const user = userEvent.setup();
  const query = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const { container } = render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={query}>
        <RequirementAttachments
          project={7}
          requirement={{ id: 12, title: "Requirement" }}
          canWrite
        />
      </QueryClientProvider>
    </I18nextProvider>,
  );
  expect(await screen.findByText("spec.pdf")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Download attachment" }));
  await waitFor(() => expect(filesContent).toHaveBeenCalledWith(3));
  await user.click(screen.getByRole("button", { name: "Remove attachment" }));
  await waitFor(() =>
    expect(studio.removeRequirementAttachment).toHaveBeenCalledWith(7, 12, 3),
  );
  const fileInput = container.querySelector(
    'input[type="file"]',
  ) as HTMLInputElement;
  fireEvent.change(fileInput, {
    target: {
      files: [new File(["hello"], "note.txt", { type: "text/plain" })],
    },
  });
  await waitFor(() =>
    expect(filesUpload).toHaveBeenCalledWith(
      expect.objectContaining({ name: "note.txt", purpose: "attachment" }),
    ),
  );
  await waitFor(() =>
    expect(studio.addRequirementAttachment).toHaveBeenCalledWith(7, 12, 5),
  );
});
