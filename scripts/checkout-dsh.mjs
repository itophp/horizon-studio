import { execFileSync } from "node:child_process";
import { existsSync } from "node:fs";
import { fileURLToPath } from "node:url";

export const root = fileURLToPath(new URL("../", import.meta.url));
export const dsh = fileURLToPath(new URL("../dsh/", import.meta.url));
export const tag = "dsh-v0.2.2-rc.3";
export const revision = "38a6a5c965aa99ab525161efb7cc5cf1fe62fced";
export const dshVersion = "0.2.2-rc.3";

export function checkout() {
  if (!existsSync(dsh)) {
    execFileSync(
      "git",
      [
        "clone",
        "--depth",
        "1",
        "--branch",
        tag,
        "https://github.com/deepseek-ai/deepseek-harness",
        dsh,
      ],
      { cwd: root, stdio: "inherit" },
    );
  }
  let current;
  try {
    current = execFileSync(
      "git",
      ["-C", dsh, "rev-parse", "--verify", "HEAD"],
      { encoding: "utf8" },
    ).trim();
  } catch {
    throw new Error(
      `DSH 检出尚未完成：${dsh}。需要完整的 ${tag} 提交 ${revision}。`,
    );
  }
  if (current !== revision)
    throw new Error(`DSH 版本不符：${current}，需要 ${revision}`);
  try {
    execFileSync("git", ["-C", dsh, "diff", "--quiet", "HEAD", "--"]);
  } catch {
    throw new Error(
      "DSH 跟踪文件有本地改动，请先将改动移到独立的 Studio 构建覆盖层",
    );
  }
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1])
  checkout();
