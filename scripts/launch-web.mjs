import { spawn } from "node:child_process";
import {
  openSync,
  closeSync,
  writeFileSync,
  readFileSync,
  mkdirSync,
  fstatSync,
} from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { existsSync } from "node:fs";

const studio = fileURLToPath(new URL("../", import.meta.url));
const local = join(studio, ".local");
const runtime = join(local, "runtime");
mkdirSync(join(local, "studio-home", "studio"), { recursive: true });
const bin = join(
  runtime,
  "node_modules",
  "@deepseek-ai",
  "dsh",
  "lib",
  "bin.js",
);
if (!existsSync(bin)) {
  console.error("DSH 运行时缺失，请先执行: pnpm prepare:web");
  process.exit(1);
}
const logPath = join(local, "web.log");
const log = openSync(logPath, "a", 0o600);
const logStart = fstatSync(log).size;
const child = spawn(
  process.execPath,
  [
    bin,
    "--profile",
    "web",
    "--patch",
    join(runtime, "studio.patch.yml"),
    "--host",
    "127.0.0.1",
    "--port",
    process.env.HORIZONSTUDIO_PORT ?? "3095",
    ...(process.env.HORIZONSTUDIO_NO_OPEN ? ["--no-open"] : []),
  ],
  {
    cwd: studio,
    detached: true,
    stdio: ["ignore", log, log],
    env: {
      ...process.env,
      DSH_HOME: join(runtime, "home"),
      HORIZONSTUDIO_ROOT: studio,
      HORIZONSTUDIO_HOME:
        process.env.HORIZONSTUDIO_HOME ?? join(local, "studio-home"),
      HORIZONSTUDIO_CLI: join(
        studio,
        "resources",
        "horizon-cli",
        "horizon-cli",
      ),
    },
  },
);
closeSync(log);
child.on("spawn", () => {
  writeFileSync(join(local, "web.pid"), String(child.pid) + "\n");
  child.unref();
  const port = process.env.HORIZONSTUDIO_PORT ?? "3095";
  console.log("HorizonStudio web PID", child.pid, "→ http://127.0.0.1:" + port);
  // DSH mints a per-boot launch token; persist the authenticated entry URL so
  // users never have to dig through web.log. The first visit stores a durable
  // signed cookie, so later visits (and restarts) need no token at all.
  const urlPath = join(local, "web-url.txt");
  let attempts = 0;
  const timer = setInterval(() => {
    if (++attempts > 60) {
      clearInterval(timer);
      process.exit(0);
    }
    const logText = existsSync(logPath)
      ? readFileSync(logPath).subarray(logStart).toString("utf8")
      : "";
    const match = logText.match(
      /(http:\/\/127\.0\.0\.1:\d+\/\?token=[A-Za-z0-9_-]+)/,
    );
    if (match) {
      writeFileSync(urlPath, match[1] + "\n");
      clearInterval(timer);
      console.log("入口地址已写入", urlPath);
      process.exit(0);
    }
  }, 500);
});
