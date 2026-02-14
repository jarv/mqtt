import { build } from "esbuild";
import { rmSync, existsSync, mkdirSync, readdirSync } from "fs";
import { resolve } from "path";
import { execSync } from "child_process";

const isProd = process.env.NODE_ENV === "production";
const distPath = "./mqtt/dist";

// Ensure dist directory exists
if (!existsSync(distPath)) {
  mkdirSync(distPath, { recursive: true });
}

// Clean dist directory contents
if (existsSync(distPath)) {
  const files = readdirSync(distPath);
  files.forEach((file) => {
    rmSync(resolve(distPath, file), { recursive: true, force: true });
  });
}

// Build Tailwind CSS
console.log("Building Tailwind CSS...");
execSync(
  `npx @tailwindcss/cli -i ./public/style.css -o ${distPath}/style.css${isProd ? " --minify" : ""}`,
  { stdio: "inherit" },
);

// Build JS with esbuild
console.log("Building JS...");
await build({
  entryPoints: ["./src/main.js"],
  bundle: true,
  minify: isProd,
  sourcemap: !isProd,
  target: ["chrome80", "firefox75", "safari13"],
  outdir: distPath,
  define: {
    "process.env.NODE_ENV": JSON.stringify(isProd ? "production" : "development"),
  },
});

console.log("Build complete.");
