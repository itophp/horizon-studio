import { queryOptions } from "@tanstack/react-query";
import { studio, type ProjectService } from "./api";

export const isRunning = (service: ProjectService) =>
  /^(running|started|up)$/i.test(service.status);
export const isStopped = (service: ProjectService) =>
  service.status.toLowerCase() === "stopped";

export function serviceStatusOptions(project?: number, target = "all") {
  return queryOptions({
    queryKey: ["studio-service-status", project, target],
    queryFn: async ({ signal }) => {
      if (project === undefined) return [];
      return (await studio.status(project, target, signal)).services;
    },
    enabled: project !== undefined,
    refetchInterval: 15_000,
    retry: false,
  });
}

export function runningServiceURL(
  services: readonly ProjectService[],
  target: "backend" | "frontend",
  path = "/",
) {
  const origin = services.find(
    (service) => service.service === target && isRunning(service),
  )?.origin;
  if (!origin) return undefined;
  try {
    const url = new URL(origin);
    if (
      (url.protocol !== "http:" && url.protocol !== "https:") ||
      !(
        url.hostname.startsWith("127.") ||
        url.hostname === "localhost" ||
        url.hostname === "[::1]"
      ) ||
      url.username ||
      url.password ||
      url.pathname !== "/" ||
      url.search ||
      url.hash
    )
      return undefined;
    return new URL(path, url).href;
  } catch {
    return undefined;
  }
}
