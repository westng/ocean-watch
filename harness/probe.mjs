import assert from 'node:assert/strict';
import { mkdir, readFile, realpath, writeFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { tmpdir } from 'node:os';
import { dirname, isAbsolute, join, relative, resolve, sep } from 'node:path';
import { pathToFileURL } from 'node:url';

const [hostArgument, stateArgument] = process.argv.slice(2);
assert.ok(process.versions.electron, 'Run npm run test:desktop with the installed desktop application');
assert.ok(hostArgument && stateArgument, 'Desktop probe requires its bundled runtime and disposable profile');
const hostRoot = await realpath(resolve(hostArgument));
const state = await realpath(resolve(stateArgument));
const profileDir = await realpath(join(state, 'dsh/profiles/desktop'));
const temporaryRoots = [await realpath(tmpdir())];
if (process.env.RUNNER_TEMP) temporaryRoots.push(await realpath(process.env.RUNNER_TEMP));
assert.ok(temporaryRoots.some((root) => {
  const path = relative(root, state);
  return path !== '..' && !path.startsWith(`..${sep}`) && !isAbsolute(path);
}), 'Use a disposable profile under the system or CI temporary directory');
// The launcher owns this single fixture and its cleanup; never fall back to user state.
for (const [key, directory] of Object.entries({
  DSH_HOME: 'dsh', DSH_AGENTS_HOME: 'agents', CODEX_HOME: 'codex', OCEAN_WATCH_HOME: 'ocean',
})) assert.equal(process.env[key], join(state, directory), `${key} must use the disposable fixture`);

assert.equal(JSON.parse(await readFile(join(hostRoot, 'package.json'), 'utf8')).name,
  '@deepseek-ai/dsh-desktop-runtime', 'Use the desktop app bundled runtime');
const hostRequire = createRequire(join(hostRoot, 'package.json'));
const load = (specifier) => import(pathToFileURL(hostRequire.resolve(specifier)).href);
let ctx;
try {
  const { loadProfileDirectory, createRuntimeResolution, PluginPackages, evaluatePluginCompatibility, getDshRuntimeVersion } = await load('@deepseek-ai/dsh-app-boot');
  const installAnchor = join(hostRoot, 'node_modules', '@deepseek-ai', 'dsh', 'package.json');
  const profile = loadProfileDirectory(
    'ocean-watch-probe', profileDir, installAnchor,
  );
  assert.deepEqual(profile.skippedBundles ?? [], [], 'Harness must accept every selected bundle');
  const layer = profile.layers.find((item) => item.packageName === 'ocean-watch');
  assert.ok(layer, 'Harness did not select the installed Ocean Watch bundle');
  assert.equal(layer.patches[0].insert[0].name, 'ocean-watch');
  const pluginRoot = await realpath(layer.packageDir);

  const { Context } = await load('@deepseek-ai/cordis');
  ctx = new Context();
  const manifest = JSON.parse(await readFile(join(pluginRoot, 'package.json'), 'utf8'));
  assert.equal(evaluatePluginCompatibility(manifest), undefined, 'Desktop must accept the plugin without a version exemption');
  const runtimeVersion = getDshRuntimeVersion();
  await ctx.plugin(PluginPackages, {
    resolution: await createRuntimeResolution({ installAnchor, profile, home: process.env.DSH_HOME }),
  });
  const { default: SystemPrompt } = await load('@deepseek-ai/dsh-system-prompt');
  const { default: ToolRuntime } = await load('@deepseek-ai/dsh-tools');
  const { default: SkillRegistry } = await load('@deepseek-ai/dsh-skill');
  const SkillFilesystem = await load('@deepseek-ai/dsh-skill-filesystem');
  const profileRequire = createRequire(join(profileDir, 'package.json'));
  // Resolve the public package export, exactly as a bundle's named row does.
  const OceanWatch = await import(pathToFileURL(profileRequire.resolve(layer.patches[0].insert[0].name)).href);

  const baselineRoot = join(state, 'baseline-skills');
  await mkdir(join(baselineRoot, 'host-fixture'), { recursive: true });
  await writeFile(join(baselineRoot, 'host-fixture', 'SKILL.md'),
    '---\nname: host-fixture\ndescription: Existing host skill.\n---\nKeep this skill.\n');
  await ctx.plugin(SystemPrompt);
  await ctx.plugin(ToolRuntime);
  await ctx.plugin(SkillRegistry);
  await ctx.plugin(SkillFilesystem, {
    includeDefaultRoots: false, customSkillDirs: [baselineRoot], watch: false,
  });
  const ocean = await ctx.plugin(OceanWatch);

  const catalog = await ctx.skills.list({ cwd: state });
  const names = catalog.filter((item) => item.provider === 'ocean-watch').map((item) => item.name).sort();
  assert.deepEqual(names, ['ads-plan-monitor', 'qc-plan-monitor']);
  assert.ok(catalog.some((item) => item.name === 'host-fixture'), 'Existing host skills must remain available');
  for (const name of names) {
    const skill = await ctx.skills.get(name, { cwd: state });
    assert.equal(await realpath(dirname(skill.path)), join(pluginRoot, 'skills', name));
    assert.match(skill.content, /On DeepSeek Harness/);
  }

  const tools = ctx.tools.schemas().filter((item) => item.name.startsWith('mcp__ocean-watch__'));
  assert.equal(tools.length, 18, 'Harness must register the entire MCP tool catalog');
  for (const channel of ['marketing', 'qianchuan', 'star_map', 'shared', 'all']) {
    const result = await ctx.tools.execute({
      signal: AbortSignal.timeout(30_000), callId: `ocean-watch-harness-probe-${channel}`,
      name: 'mcp__ocean-watch__get_capabilities', arguments: { channel },
    });
    const detail = result.isError ? result.content
      .filter((item) => item.type === 'text').map((item) => item.text.slice(0, 800)).join('\n') : '';
    assert.equal(result.isError, false, `The keyless ${channel} capability query must succeed: ${detail}`);
    assert.ok(result.content.some((item) => item.type === 'text' && item.text.length > 0));
  }

  await ocean.dispose();
  assert.ok(ctx.tools.schemas().every((item) => !item.name.startsWith('mcp__ocean-watch__')));
  const remaining = await ctx.skills.list({ cwd: state });
  assert.deepEqual(remaining.map((item) => item.name), ['host-fixture']);
  // Reusing the same host catches leaked MCP server names, tools and skill providers.
  const reloaded = await ctx.plugin(OceanWatch);
  assert.deepEqual(ctx.tools.schemas().filter((item) => item.name.startsWith('mcp__ocean-watch__'))
    .map((item) => item.name).sort(), tools.map((item) => item.name).sort());
  const reloadedSkills = await ctx.skills.list({ cwd: state });
  assert.deepEqual(reloadedSkills.filter((item) => item.provider === 'ocean-watch')
    .map((item) => item.name).sort(), names);
  const reloadedResult = await ctx.tools.execute({
    signal: AbortSignal.timeout(30_000), callId: 'ocean-watch-reload-probe',
    name: 'mcp__ocean-watch__get_capabilities', arguments: { channel: 'shared' },
  });
  assert.equal(reloadedResult.isError, false, 'The remounted plugin must remain callable');
  await reloaded.dispose();
  assert.ok(ctx.tools.schemas().every((item) => !item.name.startsWith('mcp__ocean-watch__')));
  assert.deepEqual((await ctx.skills.list({ cwd: state })).map((item) => item.name), ['host-fixture']);
  console.log(JSON.stringify({ runtimeVersion, ok: true, tools: tools.length, skills: names,
    capabilityQuery: true, unload: true, reload: true }));
} finally {
  if (ctx) await ctx.fiber.dispose();
}
