import { cp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { existsSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { pathToFileURL } from "node:url";
import { parseEnv } from "node:util";
import { root, dshVersion } from "./checkout-dsh.mjs";

const local = join(root, ".local");
const runtime = join(local, "runtime");
const envPath = join(root, ".env");
const localEnv = existsSync(envPath)
  ? parseEnv(await readFile(envPath, "utf8"))
  : {};
const webPort = process.env.WEB_PORT ?? localEnv.WEB_PORT ?? "5173";
if (!/^\d+$/.test(webPort) || Number(webPort) < 1 || Number(webPort) > 65535) {
  throw new Error("WEB_PORT must be a valid TCP port");
}
const studioOrigin = `http://127.0.0.1:${Number(webPort)}`;
const require = createRequire(join(root, "package.json"));
const { build } = require("esbuild");

// Install the pinned published DSH build first (ships prebuilt lib/bin.js):
// npm prunes foreign packages, so the Studio plugins are staged afterwards.
const dshTarget = join(runtime, "node_modules", "@deepseek-ai", "dsh");
await mkdir(dirname(dshTarget), { recursive: true });
if (!existsSync(join(dshTarget, "lib", "bin.js"))) {
  await rm(dshTarget, { recursive: true, force: true });
  execFileSync(
    "npm",
    [
      "install",
      "--prefix",
      runtime,
      "--no-save",
      "--no-audit",
      "--no-fund",
      `@deepseek-ai/dsh@${dshVersion}`,
    ],
    { stdio: "inherit" },
  );
}

// Stage the Studio plugins into the DSH web profile: copy host sources, bundle
// browser entries with esbuild and wrap them for the DSH module loader, then
// install the web patch layer. They are staged in BOTH node_modules roots —
// the runtime root for package resolution and DSH_HOME for the profile that
// the patch layer imports from.
for (const name of ["studio-host", "studio-brand", "studio-agent-bridge"]) {
  const source = join(root, "plugins", name);
  const target = join(runtime, "node_modules", "@horizonstudio", name);
  await rm(target, { recursive: true, force: true });
  await cp(source, target, { recursive: true });
  if (name !== "studio-host") {
    // Compile the Horizon design system (Tailwind v4) before bundling: the
    // generated utilities CSS is imported by the entry and inlined into the
    // client bundle wrapper.
    const result = await build({
      entryPoints: [join(source, "index.jsx")],
      bundle: true,
      format: "cjs",
      platform: "browser",
      define: {
        "process.env.HORIZONSTUDIO_WEB_ORIGIN": JSON.stringify(studioOrigin),
      },
      // Vendored Horizon components rely on the automatic JSX runtime and do
      // not import React themselves; the classic createElement transform would
      // crash them with "React is not defined".
      jsx: "automatic",
      external: ["react", "react/jsx-runtime", "react-dom", "react-dom/client"],
      write: false,
      outdir: "lib",
    });
    const js = result.outputFiles.find((file) =>
      file.path.endsWith(".js"),
    ).text;
    const css = result.outputFiles.find((file) =>
      file.path.endsWith(".css"),
    )?.text;
    await mkdir(join(target, "lib"), { recursive: true });
    await writeFile(
      join(target, "lib", "client.js"),
      `
      window.__ModuleLoader__.load({ id: "@horizonstudio/${name}", factory: (require) => {
        var module = { exports: {} };
        ${css ? `const style = document.createElement('style'); style.textContent = ${JSON.stringify(css)}; document.head.append(style);` : ""}
        ${js}
        return module.exports;
      } });`,
    );
  }
  const homeTarget = join(
    runtime,
    "home",
    "node_modules",
    "@horizonstudio",
    name,
  );
  await rm(homeTarget, { recursive: true, force: true });
  await mkdir(dirname(homeTarget), { recursive: true });
  await cp(target, homeTarget, { recursive: true });
}
// Keep the pinned native DSH tools and plan mode in sync with the Standard preset.
const runtimeRequire = createRequire(join(dshTarget, "package.json"));
const include = runtimeRequire.resolve("@deepseek-ai/cordis-plugin-include");
const { entryListSchema } = await import(pathToFileURL(include).href);
const yaml = createRequire(include)("js-yaml");
const webApp = dirname(
  runtimeRequire.resolve("@deepseek-ai/dsh-web-app/package.json"),
);
const standard = yaml.load(
  await readFile(join(webApp, "presets", "standard.patch.yml"), "utf8"),
  { schema: entryListSchema },
);
const plugins = standard
  .flatMap((row) => row.insert ?? [])
  .find((row) => row.config?.id === "standard")?.config.plugins;
const persona = plugins?.find((row) => row.name === "@deepseek-ai/dsh-persona");
if (!persona) throw new Error("固定版本 DSH 缺少 Standard 预设的 persona");
const prompt = await readFile(
  join(root, "patches", "horizon-development.md"),
  "utf8",
);
if (/\{\{[^}]+\}\}/.test(prompt))
  throw new Error("horizon-development.md 不得使用 persona 未注册的模板变量");
persona.config.prefix += "\n\n" + prompt.trim();
// Ship the preset's own skills beside the packaged dsh skills, so the Horizon
// preset keeps working in target projects that do not carry .agents/skills.
const filesystem = plugins?.find(
  (row) => row.name === "@deepseek-ai/dsh-skill-filesystem",
);
if (!filesystem)
  throw new Error("固定版本 DSH 缺少 Standard 预设的 skill-filesystem");
filesystem.config = {
  ...filesystem.config,
  customSkillDirs: [
    ...(filesystem.config?.customSkillDirs ?? []),
    join(root, "patches", "skills"),
  ],
};
const preset = yaml.dump(
  [
    {
      insert: [
        {
          id: "preset-horizon-development",
          name: "@deepseek-ai/dsh-agent-preset",
          config: {
            id: "horizon-development",
            name: "Horizon 应用开发",
            description:
              "把 Horizon 的能力模型、开发模式与 horizon-cli 带进项目：先发现契约，再沿 Capability Gateway 实现与验证。",
            order: 10,
            plugins,
          },
        },
      ],
    },
  ],
  { schema: entryListSchema, noRefs: true, lineWidth: -1 },
);
const webPatch = await readFile(join(root, "patches", "web.yml"), "utf8");
await writeFile(join(runtime, "horizon-development.patch.yml"), preset);
await writeFile(
  join(runtime, "studio.patch.yml"),
  webPatch.trimEnd() + "\n\n" + preset,
);
console.log("HorizonStudio web profile prepared at", runtime);
