import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdir, mkdtemp, readFile, realpath, rename, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

// Use the installed desktop app directly; never download a separate Harness.
const app = resolve(process.argv[2] ?? '/Applications/DeepSeek Harness.app');
assert.equal(process.platform, 'darwin', 'This launcher currently verifies the macOS desktop app only');
const executable = join(app, 'Contents/MacOS/DeepSeek Harness');
const runtime = join(app, 'Contents/Resources/app.asar/dsh');
const root = fileURLToPath(new URL('../', import.meta.url));
const state = await realpath(await mkdtemp(join(tmpdir(), 'Ocean Watch Desktop ')));
const profile = join(state, 'dsh/profiles/desktop');
const env = Object.fromEntries(Object.entries(process.env).filter(([key]) =>
  !/KEY|PASSWORD|SECRET|TOKEN/i.test(key) && !key.startsWith('DSH_')));
for (const [key, directory] of Object.entries({
  DSH_HOME: 'dsh', DSH_AGENTS_HOME: 'agents', CODEX_HOME: 'codex', OCEAN_WATCH_HOME: 'ocean',
})) env[key] = join(state, directory);

function run(command, args, extra = {}) {
  const result = spawnSync(command, args, {
    cwd: root, env, encoding: 'utf8', timeout: 120_000, maxBuffer: 8 * 1024 * 1024, ...extra,
  });
  if (result.error || result.status !== 0) {
    throw new Error(`${command} failed (${result.status}): ${result.error?.message ?? ''}\n${result.stderr ?? ''}\n${result.stdout ?? ''}`);
  }
  return result.stdout;
}

try {
  const [packed] = JSON.parse(run('npm', ['pack', '--json', '--ignore-scripts', '--pack-destination', state]));
  const modules = join(profile, 'node_modules');
  await mkdir(modules, { recursive: true });
  run('tar', ['-xzf', join(state, packed.filename), '-C', modules]);
  await rename(join(modules, 'package'), join(modules, 'ocean-watch'));
  await writeFile(join(profile, 'package.json'), JSON.stringify({
    private: true,
    dependencies: { 'ocean-watch': `file:${join(state, packed.filename)}` },
    dsh: { profile: { bundles: ['@deepseek-ai/dsh-base', '@deepseek-ai/dsh-web-app', 'ocean-watch'] } },
  }));
  assert.equal(JSON.parse(await readFile(join(modules, 'ocean-watch/package.json'), 'utf8')).name, 'ocean-watch');
  const output = run(executable, [join(root, 'harness/probe.mjs'), runtime, state], {
    env: { ...env, ELECTRON_RUN_AS_NODE: '1' },
  });
  process.stdout.write(output);
} finally {
  await rm(state, { recursive: true, force: true });
}
