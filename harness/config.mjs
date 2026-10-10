import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const bundledRoot = fileURLToPath(new URL('../', import.meta.url));
const binaries = Object.freeze({
  darwin_x64: 'ocean-watch_darwin_amd64',
  darwin_arm64: 'ocean-watch_darwin_arm64',
  linux_x64: 'ocean-watch_linux_amd64',
  linux_arm64: 'ocean-watch_linux_arm64',
  win32_x64: 'ocean-watch_windows_amd64.exe',
});

export function runtimeBinaryName(platform = process.platform, arch = process.arch) {
  const binary = binaries[`${platform}_${arch}`];
  if (!binary) {
    throw new Error(`Ocean Watch does not support this platform: ${platform}/${arch}`);
  }
  return binary;
}

export function pluginConfig({
  pluginRoot = bundledRoot,
  platform = process.platform,
  arch = process.arch,
} = {}) {
  const root = resolve(pluginRoot);
  return {
    mcp: {
      serverName: 'ocean-watch',
      transport: 'stdio',
      command: join(root, '.codex-plugin', 'bin', runtimeBinaryName(platform, arch)),
      args: ['mcp', 'proxy', '--stdio', '--plugin-root', root],
      cwd: root,
      failOnStartupError: true,
    },
    skills: {
      providerName: 'ocean-watch',
      includeDefaultRoots: false,
      customSkillDirs: [join(root, 'skills')],
    },
  };
}
