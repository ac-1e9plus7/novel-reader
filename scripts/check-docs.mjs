#!/usr/bin/env node
// Check documentation links, index coverage, environment variable references,
// and local files mentioned in inline code. Run from the repository root.
import fs from "node:fs";
import path from "node:path";
import process from "node:process";

const root = process.cwd();
const errors = [];
const skipDirs = new Set([
  ".git",
  ".idea",
  ".vscode",
  "node_modules",
  "data",
  "logs",
  "bin",
  "tmp",
  "dist",
  "test-results",
  "playwright-report",
  "coverage",
]);
const generatedPrefixes = [
  "data/",
  "logs/",
  "bin/",
  "tmp/",
  "frontend/dist",
  "frontend/node_modules",
  "frontend/test-results",
  "frontend/playwright-report",
];
const rootFiles = new Set([
  "README.md",
  "AGENTS.md",
  "CLAUDE.md",
  "CHANGELOG.md",
  "LICENSE",
  "NOTICE",
  ".env.example",
]);

function walk(dir, out = []) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (entry.isDirectory()) {
      if (!skipDirs.has(entry.name)) walk(path.join(dir, entry.name), out);
    } else if (entry.name.endsWith(".md")) {
      out.push(path.relative(root, path.join(dir, entry.name)).split(path.sep).join("/"));
    }
  }
  return out;
}

function read(file) {
  return fs.readFileSync(path.join(root, file), "utf8");
}

function stripFenced(text) {
  return text.replace(/^```[\s\S]*?^```/gm, "").replace(/^~~~[\s\S]*?^~~~/gm, "");
}

function stripInline(text) {
  return text.replace(/`[^`\n]*`/g, "");
}

function slugify(text) {
  return text
    .trim()
    .replace(/\[([^\]]+)\]\([^)]*\)/g, "$1")
    .replace(/<[^>]+>/g, "")
    .replace(/[`*_~]/g, "")
    .toLowerCase()
    .replace(/[^\p{L}\p{N}\s-]/gu, "")
    .replace(/\s+/g, "-")
    .replace(/-+/g, "-");
}

function headings(file) {
  const text = stripFenced(read(file));
  const seen = new Map();
  const result = new Set();
  for (const line of text.split(/\r?\n/)) {
    const match = /^(#{1,6})\s+(.+?)\s*#*\s*$/.exec(line);
    if (!match) continue;
    const base = slugify(match[2]);
    const count = seen.get(base) || 0;
    seen.set(base, count + 1);
    result.add(count === 0 ? base : `${base}-${count}`);
  }
  return result;
}

function checkLinks(file, text) {
  const clean = stripInline(stripFenced(text));
  const link = /!?\[[^\]]*\]\(([^)\s]+)(?:\s+"[^"]*")?\)/g;
  for (const match of clean.matchAll(link)) {
    const raw = match[1].replace(/^<|>$/g, "");
    if (!raw || raw.startsWith("#")) {
      if (raw.startsWith("#") && raw.length > 1 && !headings(file).has(raw.slice(1))) {
        errors.push(`${file}: 找不到锚点 ${raw}`);
      }
      continue;
    }
    if (/^[a-z][a-z0-9+.-]*:/i.test(raw) || raw.startsWith("//")) continue;
    const [targetPath, anchor = ""] = raw.split("#");
    if (!targetPath) continue;
    const target = path
      .relative(root, path.resolve(path.dirname(path.join(root, file)), targetPath))
      .split(path.sep)
      .join("/");
    if (target.startsWith("..")) {
      errors.push(`${file}: 链接指向仓库外 ${raw}`);
      continue;
    }
    if (!fs.existsSync(path.join(root, target))) {
      errors.push(`${file}: 找不到链接目标 ${raw}`);
      continue;
    }
    if (anchor && target.endsWith(".md") && !headings(target).has(anchor)) {
      errors.push(`${file}: 找不到锚点 ${raw}`);
    }
  }
}

function checkIndex(file, docs, label) {
  const text = read(file);
  for (const doc of docs) {
    const name = path.basename(doc);
    if (!text.includes(name)) errors.push(`${file}: 文档索引缺少 ${label}${name}`);
  }
}

function envNames(file) {
  if (!fs.existsSync(path.join(root, file))) return [];
  return read(file)
    .split(/\r?\n/)
    .map((line) => /^([A-Z][A-Z0-9_]*)=/.exec(line)?.[1])
    .filter(Boolean);
}

function checkEnvVars() {
  const config = read("docs/configuration.md");
  const names = new Set([...envNames(".env.example"), ...envNames("config/deepseek-gemini.env.example")]);
  for (const name of names) {
    if (!config.includes("`" + name + "`")) {
      errors.push(`docs/configuration.md: 未记录环境变量 ${name}`);
    }
  }
}

function checkInlinePaths(file, text) {
  for (const match of stripFenced(text).matchAll(/`([^`\n]+)`/g)) {
    const raw = match[1].trim().replace(/[.,;:]+$/, "").replace(/\/+$/, "");
    if (!raw || raw.includes("://") || /[\s*{}<>?|=]/.test(raw) || raw.includes("...")) continue;
    if (raw.startsWith("/") || raw.startsWith("-")) continue;
    const isPath = /^(?:backend|frontend|scripts|config|docs|\.github)\//.test(raw);
    if (!isPath && !rootFiles.has(raw)) continue;
    if (generatedPrefixes.some((prefix) => raw === prefix || raw.startsWith(prefix + "/"))) continue;
    if (!fs.existsSync(path.join(root, raw))) {
      errors.push(`${file}: 找不到示例路径 ${raw}`);
    }
  }
}

const files = walk(root).sort();
const docs = files.filter((file) => file.startsWith("docs/") && file !== "docs/README.md");

for (const file of files) {
  const text = read(file);
  checkLinks(file, text);
  checkInlinePaths(file, text);
}

checkIndex("README.md", docs, "docs/");
checkIndex("docs/README.md", docs, "");
checkEnvVars();

if (errors.length) {
  console.error(`文档检查失败（${errors.length} 项）：`);
  for (const error of errors) console.error(`- ${error}`);
  process.exit(1);
}

const linkCount = files.reduce(
  (sum, file) => sum + [...stripInline(stripFenced(read(file))).matchAll(/!?\[[^\]]*\]\(([^)\s]+)/g)].length,
  0,
);
console.log(`文档检查通过：${files.length} 个 Markdown 文件，${docs.length} 篇索引文档，${linkCount} 个本地/外部链接。`);
