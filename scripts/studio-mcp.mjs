import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { parseEnv } from "node:util";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";
import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import {
  CallToolRequestSchema,
  ListToolsRequestSchema,
} from "@modelcontextprotocol/sdk/types.js";

const localTool = {
  name: "get_session_context",
  description:
    "Read the Studio task, project development brief, and optional linked requirement for a DSH session ID.",
  inputSchema: {
    type: "object",
    properties: { sessionId: { type: "string", minLength: 1, maxLength: 256 } },
    required: ["sessionId"],
    additionalProperties: false,
  },
  annotations: { readOnlyHint: true, destructiveHint: false },
};

const readAttachmentTool = {
  name: "read_attachment",
  description:
    "Read one attachment of the Studio requirement linked to the current work as base64. Use the fileId from get_session_context attachments. Requires files:read. Large files may exceed the remote MCP result limit and return attachment_too_large.",
  inputSchema: {
    type: "object",
    properties: { fileId: { type: "integer", minimum: 1 } },
    required: ["fileId"],
    additionalProperties: false,
  },
  annotations: { readOnlyHint: true, destructiveHint: false },
};

async function readOptionalFile(path) {
  try {
    return await readFile(new URL(path, import.meta.url), "utf8");
  } catch (error) {
    if (error.code !== "ENOENT") throw error;
    return "";
  }
}

export async function backendURL() {
  let origin = process.env.HORIZONSTUDIO_API;
  if (!origin) {
    const metadata = await readOptionalFile(
      "../.local/horizon-cli/backend.json",
    );
    if (metadata) {
      const { pid, origin: currentOrigin } = JSON.parse(metadata);
      if (Number.isSafeInteger(pid) && pid > 1) {
        try {
          process.kill(pid, 0);
          origin = currentOrigin;
        } catch (error) {
          if (error.code !== "ESRCH") throw error;
        }
      }
    }
  }
  if (!origin) {
    const env = parseEnv(await readOptionalFile("../.env"));
    origin = `http://${process.env.API_ADDR || env.API_ADDR || "127.0.0.1:8080"}`;
  }
  const url = new URL(origin);
  if (
    url.protocol !== "http:" ||
    !["127.0.0.1", "[::1]"].includes(url.hostname) ||
    url.username ||
    url.password ||
    url.pathname !== "/" ||
    url.search ||
    url.hash
  ) {
    throw new Error("invalid_backend_origin");
  }
  return new URL("/mcp", url);
}

function toolError(text) {
  return { isError: true, content: [{ type: "text", text }] };
}

async function callRemote(name, args, signal) {
  const token = process.env.HORIZON_STUDIO_MCP_TOKEN;
  if (!token) throw new Error("studio_mcp_token_missing");
  const client = new Client({
    name: "horizon-studio-adapter",
    version: "1.0.0",
  });
  try {
    const transport = new StreamableHTTPClientTransport(await backendURL(), {
      requestInit: { headers: { Authorization: `Bearer ${token}` } },
      fetch: (input, init) => fetch(input, { ...init, redirect: "error" }),
    });
    const options = { timeout: 8000, signal };
    await client.connect(transport, options);
    return await client.callTool({ name, arguments: args }, undefined, options);
  } finally {
    await client.close();
  }
}

async function readSessionContext(sessionId, signal) {
  try {
    const result = await callRemote(
      "studio_session_context",
      { sessionId },
      signal,
    );
    const envelope = result.structuredContent;
    if (result.isError || envelope?.status !== "succeeded") {
      const code = envelope?.errorCode ?? result.content?.[0]?.text;
      return toolError(
        ["not_found", "forbidden", "invalid"].includes(code)
          ? code
          : "studio_context_unavailable",
      );
    }
    return {
      structuredContent: envelope.data,
      content: [{ type: "text", text: JSON.stringify(envelope.data) }],
    };
  } catch {
    return toolError("studio_context_unavailable");
  }
}

async function readAttachment(fileId, signal) {
  try {
    const result = await callRemote(
      "file_content_read",
      { id: fileId },
      signal,
    );
    const envelope = result.structuredContent;
    if (result.isError || envelope?.status !== "succeeded") {
      const code = envelope?.errorCode ?? result.content?.[0]?.text;
      if (code === "result_too_large") return toolError("attachment_too_large");
      return toolError(
        ["not_found", "forbidden", "invalid"].includes(code)
          ? code
          : "attachment_unavailable",
      );
    }
    return {
      structuredContent: envelope.data,
      content: [{ type: "text", text: JSON.stringify(envelope.data) }],
    };
  } catch {
    return toolError("attachment_unavailable");
  }
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const server = new Server(
    { name: "horizon-studio", version: "1.0.0" },
    { capabilities: { tools: {} } },
  );
  server.setRequestHandler(ListToolsRequestSchema, () => ({
    tools: [localTool, readAttachmentTool],
  }));
  server.setRequestHandler(CallToolRequestSchema, ({ params }, { signal }) => {
    const args = params.arguments ?? {};
    if (params.name === localTool.name) {
      const { sessionId } = args;
      if (
        Object.keys(args).length !== 1 ||
        typeof sessionId !== "string" ||
        !sessionId ||
        sessionId.trim() !== sessionId ||
        sessionId.includes("\0") ||
        [...sessionId].length > 256
      ) {
        return toolError("invalid");
      }
      return readSessionContext(sessionId, signal);
    }
    if (params.name === readAttachmentTool.name) {
      if (
        Object.keys(args).length !== 1 ||
        typeof args.fileId !== "number" ||
        !Number.isSafeInteger(args.fileId) ||
        args.fileId < 1
      ) {
        return toolError("invalid");
      }
      return readAttachment(args.fileId, signal);
    }
    return toolError("unknown_tool");
  });
  await server.connect(new StdioServerTransport());
}
