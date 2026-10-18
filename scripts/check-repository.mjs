import { execFileSync } from 'node:child_process';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

// Only return file/rule names: never print matched credentials or source lines.
export function auditFile(filename, contents) {
  const findings = [];
  const name = filename.replaceAll('\\', '/');
  const base = path.posix.basename(name);
  const example = base.endsWith('.env.example') || base === '.env.example';
  if ((/(^|\/)\.(?:env|idea|vscode)(?:[./]|$)/i.test(name) || /\.env(?:\.|$)/i.test(base)) && !example) findings.push('private configuration or IDE state');
  if (/^(?:data|logs|bin|tmp)\//.test(name) || /(^|\/)(?:node_modules|dist|test-results|playwright-report)\//.test(name) || /\.(?:sqlite3?(?:-wal|-shm)?|db|exe|tsbuildinfo|wav|flac|mp3)$/i.test(base)) findings.push('runtime data or build output');
  const patterns = [
    ['API token', /\bsk-(?:or-v1-)?[A-Za-z0-9_-]{24,}\b/],
    ['Google API key', /\bAIza[0-9A-Za-z_-]{30,}\b/],
    ['GitHub token', /\b(?:gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{30,})\b/],
    ['private key', /-----BEGIN (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----/],
    ['URL credentials', /https?:\/\/[^\s/@:]+:[^\s/@]+@/],
  ];
  for (const [rule, pattern] of patterns) if (pattern.test(contents)) findings.push(rule);
  if (example) {
    for (const line of contents.split(/\r?\n/)) {
      const match = line.match(/^\s*[A-Z0-9_]*(?:API_KEY|TOKEN|SECRET|PASSWORD)\s*=\s*(.*?)\s*$/);
      if (!match) continue;
      const value = match[1].replace(/^['"]|['"]$/g, '');
      if (value && !/^\$\{[A-Z0-9_]+\}$/.test(value) && !/^(?:填写|your[-_ ]|<|replace[-_ ]|example)/i.test(value)) findings.push('non-placeholder credential in example');
    }
  }
  return [...new Set(findings)];
}

export function checkRepository(cwd = process.cwd()) {
  const git = (...args) => execFileSync('git', args, { cwd, encoding: 'utf8', maxBuffer: 16 * 1024 * 1024 });
  const files = git('ls-files', '-z').split('\0').filter(Boolean);
  const failures = [];
  // Inspect the index so editing a staged secret in the working tree cannot hide it.
  for (const file of files) {
    const contents = git('show', `:${file}`);
    for (const rule of auditFile(file, contents)) failures.push(`${file}: ${rule}`);
  }
  return { files: files.length, failures };
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  try {
    const result = checkRepository();
    if (result.failures.length) {
      console.error(result.failures.join('\n'));
      process.exitCode = 1;
    } else console.log(`Repository guard passed (${result.files} indexed files).`);
  } catch {
    console.error('Unable to inspect the Git index. Run from the repository root.');
    process.exitCode = 1;
  }
}
