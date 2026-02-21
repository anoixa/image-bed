#!/bin/bash
# 分支保护配置脚本
# 使用方法: ./setup-branch-protection.sh <owner/repo>

set -e

REPO="${1:-anoixa/image-bed}"

echo "========================================"
echo "配置分支保护规则: $REPO"
echo "========================================"

# 检查 gh CLI 是否安装
if ! command -v gh &> /dev/null; then
    echo "错误: 需要安装 GitHub CLI (gh)"
    echo "安装: https://cli.github.com/"
    exit 1
fi

# 检查是否已登录
if ! gh auth status &> /dev/null; then
    echo "错误: 请先运行 'gh auth login' 登录"
    exit 1
fi

# 配置 main 分支保护
echo ""
echo "📝 配置 main 分支保护规则..."

gh api repos/${REPO}/branches/main/protection \
  --method PUT \
  --input - << 'EOF'
{
  "required_status_checks": {
    "strict": true,
    "contexts": [
      "Code Linting",
      "Run Tests",
      "Build Verification",
      "Docker Build Verification"
    ]
  },
  "enforce_admins": false,
  "required_pull_request_reviews": {
    "required_approving_review_count": 0,
    "dismiss_stale_reviews": true,
    "require_code_owner_reviews": false
  },
  "restrictions": null,
  "allow_force_pushes": false,
  "allow_deletions": false,
  "required_linear_history": false,
  "required_conversation_resolution": false,
  "required_signatures": false
}
EOF

echo "✅ main 分支保护已配置"

# 配置 dev 分支保护
echo ""
echo "📝 配置 dev 分支保护规则..."

gh api repos/${REPO}/branches/dev/protection \
  --method PUT \
  --input - << 'EOF'
{
  "required_status_checks": {
    "strict": false,
    "contexts": []
  },
  "enforce_admins": false,
  "required_pull_request_reviews": null,
  "restrictions": null,
  "allow_force_pushes": false,
  "allow_deletions": false
}
EOF

echo "✅ dev 分支保护已配置"

# 查看配置结果
echo ""
echo "========================================"
echo "📋 当前分支保护配置:"
echo "========================================"

echo ""
echo "main 分支:"
gh api repos/${REPO}/branches/main/protection --jq '{
  "需要状态检查": .required_status_checks.strict,
  "状态检查列表": .required_status_checks.contexts,
  "禁止强制推送": .allow_force_pushes.enabled == false,
  "禁止删除": .allow_deletions.enabled == false
}'

echo ""
echo "dev 分支:"
gh api repos/${REPO}/branches/dev/protection --jq '{
  "禁止强制推送": .allow_force_pushes.enabled == false,
  "禁止删除": .allow_deletions.enabled == false
}'

echo ""
echo "========================================"
echo "✅ 分支保护配置完成！"
echo "========================================"
echo ""
echo "📝 注意事项:"
echo "   - main 分支必须通过 PR 合并"
echo "   - main 分支要求所有检查通过才能合并"
echo "   - dev 分支禁止删除和强制推送"
echo "   - 自动合并仅适用于来自 dev 分支且作者为 anoixa 的 PR"
