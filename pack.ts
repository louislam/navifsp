import { copyFileSync, existsSync, mkdirSync, rmSync } from "node:fs";
import { join, resolve } from "node:path";
import { spawnSync } from "node:child_process";

const ROOT = resolve("./");
const DIST = join(ROOT, "dist");
const STAGE = join(DIST, "foo_navifsp");
const PACKAGE_PATH = join(DIST, "foo_navifsp.fb2k-component");

const dlls: Record<string, string> = {
    x86: join(ROOT, "foobar-plugin", "Release", "foo_navifsp.dll"),
    x64: join(ROOT, "foobar-plugin", "x64", "Release", "foo_navifsp.dll"),
    ARM64EC: join(ROOT, "foobar-plugin", "ARM64EC", "Release", "foo_navifsp.dll"),
};

for (const [arch, dll] of Object.entries(dlls)) {
    if (!existsSync(dll)) {
        console.error(`[ERROR] Missing output (${arch}): ${dll}`);
        process.exit(1);
    }
}

if (existsSync(STAGE)) rmSync(STAGE, { recursive: true });
if (!existsSync(DIST)) mkdirSync(DIST, { recursive: true });
mkdirSync(STAGE);
mkdirSync(join(STAGE, "x64"));
mkdirSync(join(STAGE, "arm64ec"));

copyFileSync(dlls.x86, join(STAGE, "foo_navifsp.dll"));
copyFileSync(dlls.x64, join(STAGE, "x64", "foo_navifsp.dll"));
copyFileSync(dlls.ARM64EC, join(STAGE, "arm64ec", "foo_navifsp.dll"));

if (existsSync(PACKAGE_PATH)) rmSync(PACKAGE_PATH);
const r = spawnSync("7z", ["a", "-tzip", "-mx9", PACKAGE_PATH, "foo_navifsp.dll", "x64\\foo_navifsp.dll", "arm64ec\\foo_navifsp.dll"], { stdio: "inherit", cwd: STAGE });
if (r.status !== 0) {
    console.error("[ERROR] Package failed");
    process.exit(1);
}

console.log(`\nDone: ${PACKAGE_PATH}`);
