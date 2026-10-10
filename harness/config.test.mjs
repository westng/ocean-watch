import assert from 'node:assert/strict';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';
import { pluginConfig, runtimeBinaryName } from './config.mjs';

test('selects the shipped runtime for every supported platform', () => {
  for (const [platform, arch, expected] of [
    ['darwin', 'x64', 'ocean-watch_darwin_amd64'],
    ['darwin', 'arm64', 'ocean-watch_darwin_arm64'],
    ['linux', 'x64', 'ocean-watch_linux_amd64'],
    ['linux', 'arm64', 'ocean-watch_linux_arm64'],
    ['win32', 'x64', 'ocean-watch_windows_amd64.exe'],
  ]) {
    assert.equal(runtimeBinaryName(platform, arch), expected);
  }
});

test('unsupported platforms fail before a subprocess starts', () => {
  for (const [platform, arch] of [['win32', 'arm64'], ['freebsd', 'x64']]) {
    assert.throws(() => pluginConfig({ platform, arch }), /does not support this platform/);
  }
});

test('resolves the installed package instead of the user workspace', () => {
  const config = pluginConfig();
  const root = resolve(fileURLToPath(new URL('../', import.meta.url)));
  assert.equal(config.mcp.cwd, root);
  assert.deepEqual(config.skills.customSkillDirs, [join(root, 'skills')]);
  assert.equal(config.skills.includeDefaultRoots, false);
  assert.equal(config.skills.providerName, 'ocean-watch');
});

test('passes a package path containing spaces and shell characters as one argument', () => {
  const root = mkdtempSync(join(tmpdir(), "Ocean Watch $test; '"));
  try {
    const { mcp } = pluginConfig({ pluginRoot: root });
    assert.equal(mcp.command, join(root, '.codex-plugin', 'bin', runtimeBinaryName()));
    assert.deepEqual(mcp.args, ['mcp', 'proxy', '--stdio', '--plugin-root', root]);
    assert.equal(mcp.cwd, root);
    assert.equal(mcp.transport, 'stdio');
    assert.equal(mcp.failOnStartupError, true);
    assert.equal(mcp.serverName, 'ocean-watch');
  } finally {
    rmSync(root, { recursive: true });
  }
});
