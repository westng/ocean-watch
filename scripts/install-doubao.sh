#!/usr/bin/env bash
set -euo pipefail

# Ocean Watch Doubao Work Skills 安装脚本
# 自动配置 Skills 的 MCP 依赖路径

DOUBAO_USER_SKILLS="$HOME/Library/Application Support/DoubaoWork/Default/.doubaowork/agent_mode/workspace/.user_skills"

# 检测 Ocean Watch 安装位置
detect_ocean_watch() {
  local repo_root
  repo_root="$(cd "$(dirname "$0")/.." && pwd)"

  local binary="$repo_root/.codex-plugin/bin/ocean-watch_darwin_arm64"

  if [[ ! -f "$binary" ]]; then
    echo "错误：未找到 Ocean Watch 二进制文件: $binary"
    echo "请先运行 'make build' 构建 Ocean Watch"
    exit 1
  fi

  echo "$repo_root"
}

# 更新 Skill 的 agents/openai.yaml
update_skill_config() {
  local skill_name="$1"
  local ocean_watch_root="$2"
  local skill_dir="$DOUBAO_USER_SKILLS/$skill_name"
  local config_file="$skill_dir/agents/openai.yaml"

  if [[ ! -f "$config_file" ]]; then
    echo "跳过 $skill_name: 配置文件不存在"
    return
  fi

  local binary="$ocean_watch_root/.codex-plugin/bin/ocean-watch_darwin_arm64"

  echo "配置 $skill_name..."

  # 备份原文件
  cp "$config_file" "$config_file.bak"

  # 使用 awk 在 transport: "stdio" 后添加 command 和 args
  awk -v bin="$binary" -v root="$ocean_watch_root" '
    /transport: "stdio"/ {
      print $0
      print "      command: \"" bin "\""
      print "      args: [\"mcp\", \"proxy\", \"--stdio\", \"--plugin-root\", \"" root "\"]"
      next
    }
    # 跳过已存在的 command 和 args 行
    /^      command:/ || /^      args:/ { next }
    { print }
  ' "$config_file.bak" > "$config_file"

  echo "✓ 已更新 $skill_name"
}

# 主流程
main() {
  echo "开始配置 Ocean Watch Doubao Work Skills..."
  echo ""

  # 检测 Ocean Watch 安装位置
  OCEAN_WATCH_ROOT=$(detect_ocean_watch)
  echo "检测到 Ocean Watch 安装位置: $OCEAN_WATCH_ROOT"
  echo ""

  # 检查豆包工作用户 Skills 目录是否存在
  if [[ ! -d "$DOUBAO_USER_SKILLS" ]]; then
    echo "错误：豆包工作用户 Skills 目录不存在"
    echo "路径: $DOUBAO_USER_SKILLS"
    exit 1
  fi

  # 更新两个 Skills
  update_skill_config "qc-plan-monitor" "$OCEAN_WATCH_ROOT"
  update_skill_config "ads-plan-monitor" "$OCEAN_WATCH_ROOT"

  echo ""
  echo "✓ 配置完成！"
  echo ""
  echo "下一步："
  echo "1. 完全退出豆包工作 (Command+Q)"
  echo "2. 重新打开豆包工作"
  echo "3. 在对话中测试 Skills (例如：@巨量千川计划助手 帮我列出本地有哪些千川模板)"
}

main