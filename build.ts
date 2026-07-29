import { spawnSync } from "node:child_process";
import { copyFileSync, existsSync, readFileSync } from "node:fs";

function findMSBuild(): string {
  const which = spawnSync("where", ["msbuild"], { stdio: "pipe" });
  if (which.status === 0) return "msbuild";

  const vs =
    "C:\\Program Files (x86)\\Microsoft Visual Studio\\Installer\\vswhere.exe";
  if (existsSync(vs)) {
    const r = spawnSync(vs, [
      "-products",
      "*",
      "-find",
      "MSBuild\\**\\Bin\\MSBuild.exe",
      "-latest",
    ], { encoding: "utf-8", stdio: "pipe" });
    if (r.status === 0) {
      const lines = r.stdout.trim().split("\n").filter(Boolean);
      if (lines.length > 0) return lines[0].trim();
    }
  }
  throw new Error("msbuild not found");
}

function build(arch: string) {
  const msbuild = findMSBuild();

  console.log("msbuild path:", msbuild);

  const r = spawnSync(msbuild, [
    "foobar-plugin/foo_navifsp.vcxproj",
    "/nologo",
    "/v:q",
    "/maxcpucount",
    "/p:RestorePackagesConfig=true",
    "/p:Configuration=Release",
    `/p:Platform=${arch}`,
    "/p:UseOfAtl=Static",
    "/t:restore,build",
  ], { stdio: "inherit" });
  if (r.status !== 0) {
    throw new Error(`Build failed for ${arch}`);
  }
}

function buildServer() {
  const pkg = JSON.parse(readFileSync("package.json", "utf-8"));
  const version = pkg.version;

  const commit = spawnSync("git", ["rev-parse", "--short", "HEAD"], {
    encoding: "utf-8",
    stdio: "pipe",
  });

  const commitHash = (commit.status === 0) ? commit.stdout.trim() : "none";

  console.log(`Building server version ${version}, commit ${commitHash}`);

  const r = spawnSync("go", [
    "build",
    "-ldflags",
    `-X main.version=${version} -X main.commit=${commitHash}`,
    "-o",
    "../navifsp.exe",
    ".",
  ], {
    cwd: "server",
    stdio: "inherit",
  });

  if (r.status !== 0) {
    throw new Error("Server build failed");
  }
}

function serverRid(arch: string): string {
  switch (arch) {
    case "Win32":
      return "win-x86";
    case "x64":
      return "win-x64";
    case "ARM64EC":
      return "win-arm64";
    default:
      throw new Error(`Unknown arch: ${arch}`);
  }
}

function install(arch: string) {
  const rid = serverRid(arch);
  const platformDir = arch == "Win32" ? "Win32" : arch;
  console.log(`Installing ${arch} build to foobar2000\\components\\...`);

  const files: string[] = [
    `foobar-plugin/${platformDir}/Release/foo_navifsp.dll`,
  ];

  const components = "foobar2000/components";

  for (const src of files) {
    const filename = src.split("/").pop();
    const dest = `${components}/${filename}`;
    console.log(`Copying ${src} to ${dest}`);
    copyFileSync(src, dest);
  }
}

function main() {
  const target = process.argv[2];
  const platformList: string[] = [
    "Win32",
    "x64",
    "ARM64EC",
  ];

  if (target === "plugin") {
    const arch = process.argv[3];
    if (arch) {
      if (!platformList.includes(arch)) {
        throw new Error(`Unknown platform "${arch}". Use: Win32, x64, ARM64EC`);
      }
      console.log("Building plugin for:", arch);
      build(arch);
      if (arch === "x64") install(arch);
    } else {
      console.log("Building plugin for all platforms...");
      for (const a of platformList) {
        build(a);
      }
      install("x64");
    }
  } else if (target === "server") {
    buildServer();
  } else {
    console.error("Usage: node build.ts <plugin [arch] | server>");
    process.exit(1);
  }
}

main();
