import { queryOptions } from "@tanstack/react-query";
import { studio } from "./api";

export type StudioScope = readonly (string | number | undefined)[];
export const studioProjectsOptions = (scope: StudioScope) =>
  queryOptions({
    queryKey: ["studio", ...scope, "projects"],
    queryFn: ({ signal }) => studio.projects(signal),
  });
export const studioSummaryOptions = (scope: StudioScope, project?: number) =>
  queryOptions({
    queryKey: ["studio", ...scope, "summary", project],
    queryFn: ({ signal }) => studio.summary(project!, signal),
    enabled: project !== undefined,
  });
