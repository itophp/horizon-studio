// studio.* capability wrappers. Tasks are Horizon 事项 (items): the stage they
// carry is the task status shown in the rail, so no client-side task store.
import { capabilityInvoke } from "@/api/generated/baseline";
import { ApiError } from "@/api/client/request";
import { z } from "zod";

export type Project = {
  id: number;
  root: string;
  name?: string;
  developmentBrief?: string;
  createdAt?: string;
  requirementsPending?: number;
  requirementsTotal?: number;
  aiAnalysis?: string;
  aiAnalyzedAt?: string;
};

export type ProjectInput = {
  path: string;
  name?: string;
  developmentBrief?: string;
  repository?: string;
};
export type InitializationCheck = {
  name: string;
  status: string;
  required: boolean;
};
export type Initialization = { result: InitializationCheck[]; ready: boolean };

const manifests = z.object({
  items: z.array(
    z.object({
      id: z.string(),
      version: z.string(),
      apiVersion: z.string(),
      mode: z.string(),
      extensions: z.array(z.string()).nullish(),
      dependencies: z
        .array(z.object({ id: z.string(), version: z.string() }))
        .optional(),
    }),
  ),
});

export type ProjectService = {
  service: string;
  status: string;
  pid?: number;
  log?: string;
  startedAt?: string;
  reason?: string;
  origin?: string;
};
export type Requirement = {
  id: number;
  parentId?: number;
  title: string;
  description?: string;
  source?: string;
  archived?: boolean;
};
export type RequirementAttachment = {
  fileId: number;
  name: string;
  mime: string;
  size: number;
  createdAt?: string;
};
export type Stage = "planned" | "developing" | "verifying" | "accepted";
export type Item = {
  id: number;
  kind: string;
  title: string;
  stage: Stage;
  reference?: string;
  checks?: string;
  requirementId?: number;
  subjectItemId?: number;
  catalogTarget?: string;
  dshSessionId?: string | null;
  pinned?: boolean;
  archived?: boolean;
};
export type Run = {
  id: number;
  mode: string;
  capability: string;
  status: string;
  invocationId: string;
};
export type Summary = {
  requirements: Requirement[];
  items: Item[];
  runs: Run[];
};

export type CatalogKind = "capability" | "workflow" | "plugin";
const catalogEntrySchema = z.object({
  id: z.string(),
  version: z.string(),
  title: z.string().optional(),
  description: z.string().optional(),
  definition: z.record(z.string(), z.unknown()).optional(),
});
export type CatalogEntry = z.infer<typeof catalogEntrySchema>;

const catalogWireKind = (kind: CatalogKind) =>
  kind === "plugin" ? "registered-plugin" : kind;

const catalogListSchema = z.object({ items: z.array(catalogEntrySchema) });
const catalogDetailsSchema = z.object({ entry: catalogEntrySchema });

async function call<T>(
  capability: string,
  input: Record<string, unknown>,
  signal?: AbortSignal,
): Promise<T> {
  const response = await capabilityInvoke(
    capability,
    {
      version: "1.0.0",
      input,
      idempotencyKey: crypto.randomUUID(),
    },
    { signal },
  );
  if (response.status !== 200)
    throw new ApiError("unavailable", response.status);
  const result = response.data;
  if (result.status !== "succeeded")
    throw new ApiError(
      result.status === "awaiting_approval"
        ? "approval_required"
        : (result.errorCode ?? "internal"),
      400,
    );
  return result.data as T;
}

export const studio = {
  projects: (signal?: AbortSignal) =>
    call<{ projects: Project[] }>("studio.project.list", {}, signal).then(
      (value) => value.projects,
    ),
  linkProject: ({ path, name, developmentBrief }: ProjectInput) =>
    call<{ id: number }>("studio.project.link", {
      path,
      name,
      developmentBrief,
    }),
  createProject: (input: ProjectInput) =>
    call<{ id: number }>("studio.project.create", input),
  analyzeProject: (project: number, locale: "en" | "zh-CN") =>
    call<{ aiAnalysis: string; aiAnalyzedAt: string }>(
      "studio.project.analyze",
      { project, locale },
    ),
  environment: (project: number, signal?: AbortSignal) =>
    call<Initialization>(
      "studio.project.environment",
      { project, target: "all" },
      signal,
    ),
  projectPlugins: (project: number, signal?: AbortSignal) =>
    call<{ value: unknown }>(
      "studio.catalog.inspect",
      { project, kind: "plugin" },
      signal,
    ).then(({ value }) => manifests.parse(value).items),
  status: (project: number, target: string, signal?: AbortSignal) =>
    call<{ services: ProjectService[] }>(
      "studio.project.status",
      { project, target },
      signal,
    ),
  control: (
    project: number,
    action: string,
    target: string,
    signal?: AbortSignal,
  ) =>
    call<{ services?: ProjectService[] }>(
      "studio.project.control",
      { project, action, target },
      signal,
    ),
  log: (project: number, service: string, signal?: AbortSignal) =>
    call<{ log: string }>(
      "studio.project.log",
      { project, service },
      signal,
    ).then((value) => value.log),
  summary: (project: number, signal?: AbortSignal) =>
    call<Summary>("studio.summary", { project }, signal),
  catalog: (project: number, kind: CatalogKind, signal?: AbortSignal) =>
    call<{ value: unknown }>(
      "studio.catalog.inspect",
      { project, kind: catalogWireKind(kind) },
      signal,
    ).then(({ value }) => catalogListSchema.parse(value).items),
  catalogEntry: (
    project: number,
    kind: CatalogKind,
    reference: string,
    signal?: AbortSignal,
  ) =>
    call<{ value: unknown }>(
      "studio.catalog.inspect",
      { project, kind: catalogWireKind(kind), reference },
      signal,
    ).then(({ value }) => catalogDetailsSchema.parse(value).entry),
  addRequirement: (input: {
    project: number;
    parent?: number;
    title: string;
    description?: string;
    source?: string;
  }) =>
    call<{ id: number; parentId: number; title: string; archived: boolean }>(
      "studio.requirement.create",
      input,
    ),
  editRequirement: (input: {
    project: number;
    id: number;
    parent?: number;
    title: string;
    description?: string;
    source?: string;
  }) =>
    call<{
      id: number;
      parentId: number;
      title: string;
      archived: boolean;
    }>("studio.requirement.update", input),
  searchRequirements: (
    project: number,
    keyword: string,
    exclude?: number,
    signal?: AbortSignal,
  ) =>
    call<{ requirements: { id: number; title: string }[] }>(
      "studio.requirement.search",
      { project, keyword, exclude: exclude ?? 0 },
      signal,
    ).then((value) => value.requirements),
  requirementAttachments: (
    project: number,
    requirement: number,
    signal?: AbortSignal,
  ) =>
    call<{ attachments: RequirementAttachment[] }>(
      "studio.requirement.attachment.list",
      { project, requirement },
      signal,
    ).then((value) => value.attachments),
  addRequirementAttachment: (
    project: number,
    requirement: number,
    file: number,
  ) =>
    call<{ fileId: number }>("studio.requirement.attachment.add", {
      project,
      requirement,
      file,
    }),
  removeRequirementAttachment: (
    project: number,
    requirement: number,
    file: number,
  ) =>
    call<{ fileId: number }>("studio.requirement.attachment.remove", {
      project,
      requirement,
      file,
    }),
  linkSession: (id: number, sessionId: string) =>
    call<{ sessionId: string }>("studio.item.link_session", { id, sessionId }),
  pinSession: (project: number, id: number, pinned: boolean) =>
    call<{ id: number; pinned: boolean }>("studio.item.pin", {
      project,
      id,
      pinned,
    }),
  archiveSession: (project: number, id: number, archived: boolean) =>
    call<{ id: number; archived: boolean; pinned: boolean }>(
      "studio.item.archive",
      { project, id, archived },
    ),
  addItem: (input: {
    project: number;
    kind: string;
    title: string;
    requirementId?: number;
    checks?: string;
    subjectItemId?: number;
    catalogTarget?: string;
  }) => call<{ id: number }>("studio.item.create", input),
};

export const stageMeta: Record<Stage, { label: string; dot: string }> = {
  planned: { label: "stage.planned", dot: "bg-muted-foreground/40" },
  developing: { label: "stage.developing", dot: "bg-primary" },
  verifying: { label: "stage.verifying", dot: "bg-amber-500" },
  accepted: { label: "stage.accepted", dot: "bg-emerald-500" },
};
