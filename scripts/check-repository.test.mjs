import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { auditFile, checkRepository } from './check-repository.mjs';

test('reject private config and runtime assets', () => {
  for (const file of ['.env', '.env.bak', 'config/service.env', '.idea/workspace.xml', 'data/reader.sqlite3', 'audio/sample.wav', 'frontend/dist/index.html', 'bin/server.exe']) assert.ok(auditFile(file, '').length, file);
});
test('allow empty or templated credentials in examples', () => {
  assert.deepEqual(auditFile('.env.example', 'RESPONSES_API_KEY=\nTTS_API_KEY=${OPENROUTER_API_KEY}'), []);
  assert.deepEqual(auditFile('config/deepseek-gemini.env.example', 'TTS_API_KEY=填写密钥'), []);
  assert.deepEqual(auditFile('backend/main.go', 'package main'), []);
  assert.ok(auditFile('config/private.env.example', 'TTS_API_KEY=not-a-placeholder').length);
});
test('credential detection reports only rule names', () => {
  for (const secret of ['sk-' + 'a'.repeat(32), 'sk-or-v1-' + 'b'.repeat(64), 'AIza' + 'c'.repeat(35), 'ghp_' + 'd'.repeat(36), ['-----BEGIN', 'PRIVATE KEY-----'].join(' ')]) {
    const findings = auditFile('accidental.txt', secret);
    assert.ok(findings.length);
    assert.ok(!findings.join(' ').includes(secret));
  }
});
test('inspect staged bytes even after working file is cleaned', () => {
  const root = mkdtempSync(path.join(tmpdir(), 'reader-index-guard-'));
  try {
    execFileSync('git', ['init', '-q'], { cwd: root });
    const file = path.join(root, 'settings.txt');
    writeFileSync(file, 'sk-' + 'x'.repeat(40));
    execFileSync('git', ['add', 'settings.txt'], { cwd: root });
    writeFileSync(file, 'clean working copy');
    assert.equal(checkRepository(root).failures.length, 1);
  } finally {
    // Exact temporary directory created above; never a computed project path.
    rmSync(root, { recursive: true, force: true });
  }
});
