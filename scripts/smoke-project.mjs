import { spawn } from "node:child_process";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { checkout } from "./checkout-dsh.mjs";

// Boots the web profile on a scratch home and checks that the Studio panel
// boots and its RPC endpoint answers. Does not create or start any project.
const studio = fileURLToPath(new URL("../", import.meta.url));
checkout();
const home = await mkdtemp(join(tmpdir(), "horizonstudio-smoke-"));
const port = 3195;
const log = [];
const child = spawn(
  process.execPath,
  [
    join(
      studio,
      ".local",
      "runtime",
      "node_modules",
      "@deepseek-ai",
      "dsh",
      "lib",
      "bin.js",
    ),
    "--profile",
    "web",
    "--patch",
    join(studio, ".local", "runtime", "studio.patch.yml"),
    "--host",
    "127.0.0.1",
    "--port",
    String(port),
    "--no-open",
  ],
  {
    stdio: ["ignore", "pipe", "pipe"],
    env: {
      ...process.env,
      DSH_HOME: join(home, "dsh"),
      HORIZONSTUDIO_HOME: join(home, "studio"),
      HORIZONSTUDIO_CLI: join(studio, "vendor", "horizon-cli", "horizon-cli"),
    },
  },
);
child.stdout.on("data", (row) => log.push(row));
child.stderr.on("data", (row) => log.push(row));

try {
  let up = false;
  for (let attempt = 0; attempt < 60 && !up; attempt++) {
    await new Promise((resolve) => setTimeout(resolve, 1000));
    up = await fetch(`http://127.0.0.1:${port}/`)
      .then((response) => response.ok)
      .catch(() => false);
  }
  if (!up) throw new Error(`web 未就绪：\n${log.join("")}`);
  const rejected = await fetch(
    `http://127.0.0.1:${port}/horizon-studio/projects`,
    { method: "POST" },
  );
  console.log("web ready, RPC guard status", rejected.status);
  if (rejected.status >= 500) throw new Error("RPC 端点异常");
  console.log("smoke passed");
} finally {
  child.kill("SIGTERM");
  await rm(home, { recursive: true, force: true }).catch(() => {});
}
