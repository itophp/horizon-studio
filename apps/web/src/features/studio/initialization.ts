import { queryOptions } from "@tanstack/react-query";
import { studio } from "./api";

export function initializationOptions(
  scope: readonly (string | number | undefined)[],
  project?: number,
) {
  return queryOptions({
    queryKey: ["studio", ...scope, "environment", project],
    queryFn: ({ signal }) => studio.environment(project!, signal),
    enabled: project !== undefined,
    retry: false,
    staleTime: 15_000,
  });
}
