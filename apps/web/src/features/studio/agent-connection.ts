import { useQuery } from "@tanstack/react-query";
import { request } from "@/api/client/request";

export const PROTOCOL = "horizonstudio/agent";
export const agentOrigin = "http://127.0.0.1:3095";

// Runtime facts the DSH bridge reports for every Session it knows about.
export type AgentSessionStatus = {
  sessionId: string;
  running: boolean;
  completed: boolean;
  error: boolean;
};

export const getAgentConnection = (signal?: AbortSignal) =>
  request<{ data: { url: string } }>("/__studio/agent", { signal });

export function useAgentConnection(sessionScope: string, enabled: boolean) {
  return useQuery({
    queryKey: ["studio-agent", sessionScope],
    queryFn: ({ signal }) => getAgentConnection(signal),
    retry: false,
    enabled,
  });
}
