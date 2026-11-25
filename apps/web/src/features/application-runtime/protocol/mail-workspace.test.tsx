import { readFileSync } from "node:fs";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import {
  $getRoot,
  $getSelection,
  $isRangeSelection,
  getNearestEditorFromDOMNode,
} from "lexical";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nextProvider } from "react-i18next";
import { MemoryRouter, Route, Routes } from "react-router";
import type { ReactElement } from "react";
import { beforeEach, expect, it, onTestFinished, vi } from "vitest";
import { i18n } from "@/i18n";
import { ApiError } from "@/api/client/request";
import type { RuntimeApplicationV2 } from "@/api/generated/runtime";
import { DynamicApplicationRoute } from "../route";
import { EmailHtml } from "./email-html";
import { ProtocolPage } from "./protocol-page";

const api = vi.hoisted(() => ({
  permissions: ["mail:read", "mail:send", "mail:configure"],
  read: vi.fn(),
  execute: vi.fn(),
  upload: vi.fn(async () => ({
    status: 200,
    data: { id: 30, name: "evidence.txt" },
  })),
}));
vi.mock("@/features/identity/auth", () => ({
  useSession: () => ({
    tenantId: "tenant-a",
    account: { id: 7 },
    permissions: api.permissions,
  }),
}));
vi.mock("@/lib/capabilities", async (original) => ({
  ...(await original<typeof import("@/lib/capabilities")>()),
  runtimeApplication: async () => mailDefinition,
  runtimeRead: api.read,
  runtimeExecute: api.execute,
  filesUpload: api.upload,
}));

const mailDefinition = (
  JSON.parse(
    readFileSync("../../docs/generated/runtime/applications.json", "utf8"),
  ) as RuntimeApplicationV2[]
).find((app) => app.id === "mail");
if (!mailDefinition?.protocol)
  throw new Error("Missing generated mail protocol");
const application = mailDefinition.protocol;
const mailbox = {
  id: 8,
  name: "Work",
  emailAddress: "ada@example.test",
  defaultSend: true,
  status: "active",
  revision: 1,
  channels: [
    {
      direction: "send",
      protocol: "smtp",
      host: "smtp.old.test",
      port: 465,
      tlsMode: "tls",
      username: "ada@example.test",
      verificationStatus: "ok",
      credential: { version: 1, fingerprint: "1234567890abcdef" },
    },
    {
      direction: "receive",
      protocol: "pop3",
      host: "pop.old.test",
      port: 995,
      tlsMode: "tls",
      username: "ada@example.test",
      verificationStatus: "ok",
      credential: { version: 1, fingerprint: "1234567890abcdef" },
    },
  ],
};
const delivery = {
  id: 12,
  mailboxId: 8,
  messageId: 20,
  status: "failed",
  attempts: 1,
  createdAt: "2026-11-19T04:00:00Z",
  subject: "Retry me",
  from: "ada@example.test",
};
const message = {
  id: 20,
  mailboxId: 8,
  direction: "inbound",
  folder: "inbox",
  subject: "Hello",
  from: "sender@example.test",
  occurredAt: "2026-11-19T04:00:00Z",
  read: false,
};

beforeEach(() => {
  api.permissions = ["mail:read", "mail:send", "mail:configure"];
  api.read.mockReset();
  api.execute.mockReset();
  api.read.mockImplementation(
    async (name: string, input: { page?: number; pageSize?: number }) => {
      if (name === "application.runtime.list")
        return { items: [mailDefinition] };
      if (name === "mail.mailbox.list") return { items: [mailbox] };
      if (name === "mail.delivery.list") return { items: [delivery] };
      if (name === "mail.message.list")
        return {
          items: [message],
          total: 1,
          page: input.page ?? 1,
          pageSize: input.pageSize ?? 50,
        };
      if (name === "mail.message.get")
        return {
          ...message,
          bodyHtml: "<p>Mail content</p>",
          bodyText: "Mail content",
          recipients: [
            { kind: "to", address: "ada@example.test", displayName: "Ada" },
          ],
          attachments: [],
        };
      throw new Error("Unexpected provider");
    },
  );
  api.execute.mockImplementation(async (name: string) => ({
    status: "succeeded",
    data: name === "mail.mailbox.onboard" ? { id: 9, status: "active" } : {},
  }));
});

function view(node: ReactElement, path = "/mail") {
  return render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider
        client={
          new QueryClient({ defaultOptions: { queries: { retry: false } } })
        }
      >
        <MemoryRouter initialEntries={[path]}>{node}</MemoryRouter>
      </QueryClientProvider>
    </I18nextProvider>,
  );
}
function open(path: string, readOnly = false) {
  return view(
    <Routes>
      {application.pages.map((page) => (
        <Route
          key={page.id}
          path={page.route}
          element={
            <ProtocolPage
              application={application}
              page={page}
              readOnly={readOnly}
            />
          }
        />
      ))}
    </Routes>,
    path,
  );
}
async function choose(
  user: ReturnType<typeof userEvent.setup>,
  action: string,
) {
  await user.click(await screen.findByRole("button", { name: "Actions" }));
  await user.click(await screen.findByRole("menuitem", { name: action }));
}
function invoked(name: string, payload: Record<string, unknown>) {
  expect(api.execute).toHaveBeenCalledWith(
    name,
    "1.0.0",
    payload,
    expect.any(String),
    expect.any(String),
    undefined,
  );
}

it("shows message headers and opens the detail directly from the subject", async () => {
  const user = userEvent.setup();
  api.read.mockResolvedValueOnce({
    items: [
      message,
      { ...message, id: 21, subject: "Earlier message", read: true },
    ],
    total: 2,
    page: 1,
    pageSize: 50,
  });
  open("/mail");
  expect(await screen.findByRole("heading", { name: "Inbox" })).toBeVisible();
  const inbox = within(screen.getByRole("region", { name: "Inbox" }));
  const openMessage = inbox.getByRole("button", { name: message.subject });
  expect(within(openMessage).getByText(message.from)).toBeVisible();
  expect(within(openMessage).getByText("Unread")).toBeVisible();
  const readMessage = inbox.getByRole("button", { name: "Earlier message" });
  expect(within(readMessage).getByText("Read")).toBeVisible();
  expect(openMessage).toHaveAttribute("data-unread", "true");
  expect(readMessage).not.toHaveAttribute("data-unread");
  expect(screen.getByRole("link", { name: "Mailboxes" })).toHaveAttribute(
    "href",
    "/mail/mailboxes",
  );
  await user.click(openMessage);
  expect(
    await screen.findByRole("heading", { name: message.subject }),
  ).toBeVisible();
  expect(screen.getByRole("link", { name: "Back to inbox" })).toHaveAttribute(
    "href",
    "/mail",
  );
  expect(screen.queryByRole("link", { name: "Compose" })).toBeNull();
  expect(screen.getByText(message.from)).toBeVisible();
  expect(screen.getByText("Ada <ada@example.test>")).toBeVisible();
  expect(screen.getByText("Date")).toBeVisible();
  expect(await screen.findByTitle("Message body")).toHaveAttribute(
    "srcdoc",
    expect.stringContaining("Mail content"),
  );
  expect(api.read).toHaveBeenCalledWith(
    "mail.message.get",
    { id: 20 },
    expect.anything(),
    "1.0.0",
  );
  await waitFor(() =>
    invoked("mail.message.flag.update", { id: 20, read: true }),
  );
});

it("provides a detail entry even when the message has no subject or sender", async () => {
  const user = userEvent.setup();
  api.read.mockResolvedValueOnce({
    items: [{ ...message, subject: "", from: "" }],
    total: 1,
    page: 1,
    pageSize: 50,
  });
  open("/mail");
  expect(await screen.findByText("Unknown sender")).toBeVisible();
  await user.click(await screen.findByRole("button", { name: "(no subject)" }));
  expect(await screen.findByTitle("Message body")).toBeVisible();
});

it("opens a message URL through the production Runtime router", async () => {
  view(
    <Routes>
      <Route path="/mail/*" element={<DynamicApplicationRoute />} />
    </Routes>,
    "/mail/20",
  );
  expect(
    await screen.findByRole("heading", { name: message.subject }),
  ).toBeVisible();
  expect(screen.getByTitle("Message body")).toBeVisible();
});

it("recommends recent mail and opens the original message", async () => {
  const user = userEvent.setup();
  api.execute.mockResolvedValueOnce({
    status: "succeeded",
    data: {
      items: [
        {
          id: 20,
          subject: "Review requested",
          from: "sender@example.test",
          reason: "Approval is due tomorrow.",
        },
      ],
    },
  });
  open("/mail");

  await user.click(
    await screen.findByRole("button", { name: "Find emails worth reading" }),
  );
  invoked("mail.inbox.recommend", { locale: "en" });
  expect(await screen.findByText("Approval is due tomorrow.")).toBeVisible();
  await user.click(screen.getByRole("link", { name: "Review requested" }));
  await waitFor(() =>
    expect(api.read).toHaveBeenCalledWith(
      "mail.message.get",
      { id: 20 },
      expect.anything(),
      "1.0.0",
    ),
  );
});

it("analyzes the open message and renders its summary and deadlines", async () => {
  const user = userEvent.setup();
  api.execute.mockImplementation(async (name: string) => ({
    status: "succeeded",
    data:
      name === "mail.message.analyze"
        ? {
            summary: "The approval is requested.",
            keyPoints: [],
            actionItems: [{ task: "Approve the draft", deadline: "Friday" }],
            deadlines: ["Friday"],
            truncated: false,
          }
        : {},
  }));
  open("/mail/20");

  await user.click(
    await screen.findByRole("button", { name: "Analyze email" }),
  );
  invoked("mail.message.analyze", { id: 20, locale: "en" });
  expect(await screen.findByText("The approval is requested.")).toBeVisible();
  expect(screen.getByText("Approve the draft")).toBeVisible();
  expect(screen.getByText("Due Friday")).toBeVisible();
});

it("loads and opens the connection dialog from a mailbox Provider with no records", async () => {
  const user = userEvent.setup();
  api.read.mockResolvedValueOnce({ items: [] });
  open("/mail/mailboxes");
  await user.click(
    await screen.findByRole("button", { name: "Connect mailbox" }),
  );
  const dialog = within(
    await screen.findByRole("dialog", { name: "Connect mailbox" }),
  );
  await user.type(dialog.getByLabelText("Display name"), "Personal");
  await user.type(dialog.getByLabelText("Email address"), "ada@example.test");
  await user.type(
    dialog.getByLabelText("Password or app password"),
    "app-password",
  );
  expect(dialog.getByLabelText("SMTP host")).toHaveValue("smtp.example.test");
  expect(dialog.getByLabelText("POP3S host")).toHaveValue("pop.example.test");
  expect(dialog.getByLabelText("SMTP port")).toHaveValue(587);
  expect(dialog.getByLabelText("POP3 port")).toHaveValue(995);
  expect(dialog.getByLabelText("POP3 encryption")).toHaveAttribute("readonly");
  await user.click(dialog.getByRole("button", { name: "Connect mailbox" }));
  await waitFor(() =>
    invoked("mail.mailbox.onboard", {
      name: "Personal",
      emailAddress: "ada@example.test",
      password: "app-password",
      smtpHost: "smtp.example.test",
      smtpPort: 587,
      smtpTls: "starttls",
      smtpUsername: "ada@example.test",
      receiveProtocol: "pop3",
      receiveTls: "tls",
      receiveHost: "pop.example.test",
      receivePort: 995,
      receiveUsername: "ada@example.test",
    }),
  );
  expect(await screen.findByRole("status")).toHaveTextContent(
    "Mailbox changes saved",
  );
});

it("validates ports and submits custom settings when connecting a mailbox", async () => {
  const user = userEvent.setup();
  api.read.mockResolvedValueOnce({ items: [] });
  open("/mail/mailboxes");
  await user.click(
    await screen.findByRole("button", { name: "Connect mailbox" }),
  );
  const dialog = within(
    await screen.findByRole("dialog", { name: "Connect mailbox" }),
  );
  const email = dialog.getByLabelText("Email address");
  const smtpPort = dialog.getByLabelText("SMTP port");
  const popPort = dialog.getByLabelText("POP3 port");
  const popUsername = dialog.getByLabelText("POP3 username");
  const encryption = dialog.getByLabelText("SMTP encryption");
  const connect = dialog.getByRole("button", { name: "Connect mailbox" });

  await user.type(dialog.getByLabelText("Display name"), "Personal");
  await user.type(email, "ada@example.test");
  await user.type(
    dialog.getByLabelText("Password or app password"),
    "app-password",
  );
  await user.clear(popUsername);
  await user.type(popUsername, "incoming-user");
  await user.clear(email);
  await user.type(email, "mail@example.test");
  expect(popUsername).toHaveValue("incoming-user");
  expect(dialog.getByLabelText("SMTP username")).toHaveValue(
    "mail@example.test",
  );

  await user.selectOptions(encryption, "tls");
  expect(smtpPort).toHaveValue(465);
  await user.clear(smtpPort);
  await user.type(smtpPort, "2465");
  await user.clear(popPort);
  await user.type(popPort, "65536");
  await user.click(connect);
  expect(await dialog.findByRole("alert")).toHaveTextContent(
    "Enter a whole-number port from 1 to 65535.",
  );
  expect(api.execute).not.toHaveBeenCalled();

  await user.clear(popPort);
  await user.type(popPort, "1995");
  await user.click(connect);
  await waitFor(() =>
    invoked(
      "mail.mailbox.onboard",
      expect.objectContaining({
        emailAddress: "mail@example.test",
        smtpTls: "tls",
        smtpPort: 2465,
        smtpUsername: "mail@example.test",
        receiveProtocol: "pop3",
        receivePort: 1995,
        receiveUsername: "incoming-user",
      }),
    ),
  );
});

it("connects an IMAP mailbox and submits the selected encryption mode and port", async () => {
  const user = userEvent.setup();
  api.read.mockResolvedValueOnce({ items: [] });
  open("/mail/mailboxes");
  await user.click(
    await screen.findByRole("button", { name: "Connect mailbox" }),
  );
  const dialog = within(
    await screen.findByRole("dialog", { name: "Connect mailbox" }),
  );
  await user.type(dialog.getByLabelText("Display name"), "Personal");
  await user.type(dialog.getByLabelText("Email address"), "ada@example.test");
  await user.type(
    dialog.getByLabelText("Password or app password"),
    "app-password",
  );
  await user.selectOptions(
    dialog.getByLabelText("Incoming mail protocol"),
    "imap",
  );
  expect(dialog.getByLabelText("IMAP host")).toHaveValue("imap.example.test");
  const port = dialog.getByLabelText("IMAP port");
  expect(port).toHaveValue(993);
  await user.selectOptions(
    dialog.getByLabelText("IMAP encryption"),
    "starttls",
  );
  expect(port).toHaveValue(143);
  await user.clear(port);
  await user.type(port, "1143");
  await user.click(dialog.getByRole("button", { name: "Connect mailbox" }));
  await waitFor(() =>
    invoked(
      "mail.mailbox.onboard",
      expect.objectContaining({
        receiveProtocol: "imap",
        receiveHost: "imap.example.test",
        receivePort: 1143,
        receiveTls: "starttls",
        receiveUsername: "ada@example.test",
      }),
    ),
  );
});

it("loads and edits saved connection options and submits credential changes separately", async () => {
  const user = userEvent.setup();
  open("/mail/mailboxes");
  await choose(user, "Edit");
  const smtp = screen.getByLabelText("SMTP host");
  const smtpPort = screen.getByLabelText("SMTP port");
  const smtpUsername = screen.getByLabelText("SMTP username");
  const smtpEncryption = screen.getByLabelText("SMTP encryption");
  expect(smtpPort).toHaveValue(465);
  expect(smtpUsername).toHaveValue("ada@example.test");
  expect(smtpEncryption).toHaveValue("tls");
  expect(screen.getByLabelText("POP3 port")).toHaveValue(995);
  await user.clear(smtp);
  await user.type(smtp, "smtp.new.test");
  await user.clear(smtpPort);
  await user.type(smtpPort, "2587");
  await user.clear(smtpUsername);
  await user.type(smtpUsername, "outgoing-user");
  await user.selectOptions(smtpEncryption, "starttls");
  await user.click(screen.getByRole("button", { name: "Save mailbox" }));
  await waitFor(() =>
    invoked(
      "mail.mailbox.update",
      expect.objectContaining({
        id: 8,
        revision: 1,
        smtpHost: "smtp.new.test",
        smtpPort: 2587,
        smtpUsername: "outgoing-user",
        smtpTls: "starttls",
        receivePort: 995,
      }),
    ),
  );
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  await choose(user, "New SMTP password");
  await user.type(
    screen.getByLabelText("New SMTP password", { selector: "input" }),
    "new-secret",
  );
  await user.click(screen.getByRole("button", { name: "Save password" }));
  await waitFor(() =>
    invoked("mail.mailbox.credential.replace", {
      mailboxId: 8,
      purpose: "smtp",
      password: "new-secret",
    }),
  );
});

it("loads an existing IMAP mailbox and targets its receiving credential", async () => {
  const user = userEvent.setup();
  api.read.mockResolvedValue({
    items: [
      {
        ...mailbox,
        channels: [
          mailbox.channels[0],
          {
            ...mailbox.channels[1],
            protocol: "imap",
            port: 1143,
            tlsMode: "starttls",
          },
        ],
      },
    ],
  });
  open("/mail/mailboxes");
  await choose(user, "Edit");
  expect(screen.getByLabelText("Incoming mail protocol")).toHaveValue("imap");
  expect(screen.getByLabelText("IMAP encryption")).toHaveValue("starttls");
  expect(screen.getByLabelText("IMAP port")).toHaveValue(1143);
  await user.click(screen.getByRole("button", { name: "Cancel" }));
  await choose(user, "New incoming mail password");
  await user.type(
    screen.getByLabelText("New incoming mail password", { selector: "input" }),
    "new-secret",
  );
  await user.click(screen.getByRole("button", { name: "Save password" }));
  await waitFor(() =>
    invoked("mail.mailbox.credential.replace", {
      mailboxId: 8,
      purpose: "imap",
      password: "new-secret",
    }),
  );
});

it("uses Runtime actions for retries and confirms delivery cancellation", async () => {
  const user = userEvent.setup();
  api.read.mockResolvedValueOnce({
    items: [{ ...delivery, status: "queued" }],
  });
  open("/mail/deliveries");
  await choose(user, "Cancel delivery");
  expect(api.execute).not.toHaveBeenCalled();
  const dialog = within(
    await screen.findByRole("dialog", { name: "Cancel delivery" }),
  );
  await user.click(dialog.getByRole("button", { name: "Cancel delivery" }));
  await waitFor(() => invoked("mail.delivery.cancel", { id: 12 }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  await choose(user, "Retry");
  await waitFor(() => invoked("mail.delivery.retry", { id: 12 }));
});

it("hides delivery mutations for an account with only read permission", async () => {
  api.permissions = ["mail:read"];
  open("/mail/deliveries");
  expect(await screen.findByText(delivery.subject)).toBeVisible();
  expect(screen.queryByRole("button", { name: "Actions" })).toBeNull();
  expect(screen.queryByRole("link", { name: "Compose" })).toBeNull();
});

it("displays a retryable Runtime Provider error", async () => {
  api.read.mockRejectedValueOnce(new ApiError("unavailable", 503));
  const user = userEvent.setup();
  open("/mail/mailboxes");
  await user.click(
    within(await screen.findByRole("alert")).getByRole("button", {
      name: "Try again",
    }),
  );
  expect(await screen.findByText(mailbox.emailAddress)).toBeVisible();
});

it("uploads a private attachment and submits its ID through the Runtime action", async () => {
  const rangeGeometry = Object.getOwnPropertyDescriptor(
    Range.prototype,
    "getBoundingClientRect",
  );
  Object.defineProperty(Range.prototype, "getBoundingClientRect", {
    configurable: true,
    value: () => new DOMRect(),
  });
  onTestFinished(() => {
    if (rangeGeometry) {
      Object.defineProperty(
        Range.prototype,
        "getBoundingClientRect",
        rangeGeometry,
      );
    } else {
      Reflect.deleteProperty(Range.prototype, "getBoundingClientRect");
    }
  });
  const user = userEvent.setup();
  open("/mail/compose");
  expect(await screen.findByText(mailbox.emailAddress)).toBeVisible();
  await user.type(screen.getByLabelText("To"), "recipient@example.test");
  await user.type(screen.getByLabelText("Subject"), "With attachment");
  const editor = screen.getByRole("textbox", { name: "Message body" });
  await user.click(editor);
  const lexical = getNearestEditorFromDOMNode(editor);
  if (!lexical) throw new Error("Missing mounted HTML editor");
  act(() => lexical.update(() => $getRoot().selectEnd(), { discrete: true }));
  const bold = screen.getByRole("button", { name: "Bold" });
  await user.click(bold);
  expect(bold).toHaveAttribute("aria-pressed", "true");
  // jsdom does not implement the browser editing action behind beforeinput.
  act(() =>
    lexical.update(
      () => {
        const selection = $getSelection();
        if (!$isRangeSelection(selection))
          throw new Error("Missing editor selection");
        selection.insertText("Body");
      },
      { discrete: true },
    ),
  );
  expect(editor).toHaveTextContent("Body");
  await user.upload(
    screen.getByLabelText("Attachments"),
    new File(["evidence"], "evidence.txt", { type: "text/plain" }),
  );
  expect(await screen.findByText("evidence.txt")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Send email" }));
  expect(api.upload).toHaveBeenCalledWith(
    expect.objectContaining({
      name: "evidence.txt",
      purpose: "mail_attachment",
    }),
  );
  await waitFor(() =>
    invoked(
      "mail.message.send",
      expect.objectContaining({
        mailboxId: 8,
        fileIds: [30],
        html: expect.stringMatching(/<strong[^>]*>Body<\/strong>/),
      }),
    ),
  );
  expect(await screen.findByRole("heading", { name: "Sent" })).toBeVisible();
});

it("uses the AI form to fill the subject and rich-text draft for manual sending", async () => {
  api.execute.mockResolvedValueOnce({
    status: "succeeded",
    data: {
      suggestions: [
        { field: "to", value: "recipient@example.test" },
        { field: "cc", value: "reviewer@example.test, lead@example.test" },
        { field: "subject", value: "Schedule a review" },
        { field: "body", value: "Can we review the draft?\nThanks!" },
      ],
    },
  });
  const user = userEvent.setup();
  open("/mail/compose");
  await user.click(
    await screen.findByRole("button", { name: "Draft with AI" }),
  );
  await user.type(
    screen.getByRole("textbox", {
      name: "Describe the email and include any To or CC addresses",
    }),
    "Ask recipient@example.test for a draft review and CC reviewer@example.test, lead@example.test",
  );
  await user.click(screen.getByRole("button", { name: "Generate with AI" }));
  invoked("mail.message.suggest-draft", {
    sourceText:
      "Ask recipient@example.test for a draft review and CC reviewer@example.test, lead@example.test",
    includeSubject: true,
    includeTo: true,
    includeCc: true,
    locale: "en",
  });
  await waitFor(() =>
    expect(screen.getByLabelText("Subject")).toHaveValue("Schedule a review"),
  );
  expect(
    screen.getByRole("textbox", { name: "Message body" }),
  ).toHaveTextContent("Can we review the draft?");
  expect(api.execute).toHaveBeenCalledTimes(1);

  expect(screen.getByLabelText("To")).toHaveValue("recipient@example.test");
  expect(screen.getByLabelText("Cc")).toHaveValue(
    "reviewer@example.test, lead@example.test",
  );
  await user.click(screen.getByRole("button", { name: "Send email" }));
  await waitFor(() =>
    invoked(
      "mail.message.send",
      expect.objectContaining({
        subject: "Schedule a review",
        to: [{ address: "recipient@example.test" }],
        cc: [
          { address: "reviewer@example.test" },
          { address: "lead@example.test" },
        ],
        html: expect.stringMatching(
          /<p[^>]*>.*Can we review the draft\?.*<\/p><p[^>]*>.*Thanks!.*<\/p>/,
        ),
      }),
    ),
  );
});

it("replaces the displayed email with translated HTML while keeping image controls", async () => {
  api.execute.mockImplementation(async (name: string) => ({
    status: "succeeded",
    data:
      name === "mail.message.translate"
        ? {
            subject: "Translated subject",
            body: "Translated body",
            bodyHtml:
              '<table style="color:#333"><tbody><tr><td>Translated body<img src="https://example.test/hero.png"></td></tr></tbody></table>',
            truncated: false,
          }
        : {},
  }));
  const user = userEvent.setup();
  open("/mail/20");
  expect(await screen.findByRole("heading", { name: "Hello" })).toBeVisible();

  await user.click(screen.getByRole("button", { name: "Translate with AI" }));
  invoked("mail.message.translate", { id: 20, locale: "en" });
  expect(
    await screen.findByRole("heading", { name: "Translated subject" }),
  ).toBeVisible();
  const translatedFrame = screen.getByTitle("Message body");
  expect(translatedFrame).toHaveAttribute(
    "srcdoc",
    expect.stringContaining(
      '<td>Translated body<img src="https://example.test/hero.png">',
    ),
  );
  expect(translatedFrame).toHaveAttribute(
    "srcdoc",
    expect.stringContaining("img-src 'none'"),
  );
  await user.click(
    screen.getByRole("button", { name: "Load external images" }),
  );
  expect(translatedFrame).toHaveAttribute(
    "srcdoc",
    expect.stringContaining("img-src https:"),
  );

  await user.click(screen.getByRole("button", { name: "Show original" }));
  expect(screen.getByRole("heading", { name: "Hello" })).toBeVisible();
  expect(screen.getByTitle("Message body")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Show translation" }));
  expect(
    screen.getByRole("heading", { name: "Translated subject" }),
  ).toBeVisible();
  expect(
    api.execute.mock.calls.filter(
      ([name]) => name === "mail.message.translate",
    ),
  ).toHaveLength(1);
});

it("switches the independent reader between HTML and plain text", async () => {
  const user = userEvent.setup();
  open("/mail/20");
  expect(await screen.findByTitle("Message body")).toHaveAttribute(
    "srcdoc",
    expect.stringContaining("default-src 'none'"),
  );
  await user.click(screen.getByRole("button", { name: "Show plain text" }));
  expect(screen.getByText("Mail content")).toBeVisible();
  expect(screen.queryByTitle("Message body")).toBeNull();
  await user.click(
    screen.getByRole("button", { name: "Show formatted message" }),
  );
  expect(screen.getByTitle("Message body")).toBeVisible();
});

it("keeps HTML in a sandboxed frame and falls back to plain text", () => {
  view(<EmailHtml html="<p>Safe</p>" text="Safe" />);
  const frame = screen.getByTitle("Message body");
  expect(frame).toHaveAttribute(
    "sandbox",
    "allow-same-origin allow-popups allow-popups-to-escape-sandbox",
  );
  expect(frame.getAttribute("srcdoc")).toContain("<p>Safe</p>");
  view(<EmailHtml text="Plain fallback" />);
  expect(screen.getByText("Plain fallback")).toBeVisible();
});

it("allows external images in the message frame only after a click", async () => {
  const user = userEvent.setup();
  view(
    <EmailHtml
      html={
        '<table><tr><td style="background-color:#3264ff"><img src="https://example.test/banner.png" alt="Banner"></td></tr></table>'
      }
      text="October"
    />,
  );
  const frame = screen.getByTitle("Message body");
  expect(frame).toHaveAttribute(
    "srcdoc",
    expect.stringContaining("img-src 'none'"),
  );
  expect(frame).toHaveAttribute(
    "srcdoc",
    expect.stringContaining("background-color:#3264ff"),
  );
  await user.click(
    screen.getByRole("button", { name: "Load external images" }),
  );
  expect(frame).toHaveAttribute(
    "srcdoc",
    expect.stringContaining("img-src https:"),
  );
  await user.click(
    screen.getByRole("button", { name: "Hide external images" }),
  );
  expect(frame).toHaveAttribute(
    "srcdoc",
    expect.stringContaining("img-src 'none'"),
  );
});
