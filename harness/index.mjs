import * as mcpClient from '@deepseek-ai/dsh-mcp-client';
import * as skillFilesystem from '@deepseek-ai/dsh-skill-filesystem';
import { pluginConfig } from './config.mjs';

export const name = 'ocean-watch';
export const inject = ['tools', 'skills'];

export async function apply(ctx) {
  const config = pluginConfig();
  // Child providers own their connections and watchers through Cordis effects.
  // Disposing this parent releases both; no separate shutdown or profile mutation.
  await Promise.all([
    ctx.plugin(mcpClient, config.mcp),
    ctx.plugin(skillFilesystem, config.skills),
  ]);
}
