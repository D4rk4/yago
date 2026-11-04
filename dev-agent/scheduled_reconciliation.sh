#!/usr/bin/env bash
set -euo pipefail
umask 077

PATH=/home/htaf/.nvm/versions/node/v24.22.1/bin:/usr/local/bin:/usr/bin:/bin
repo_root=/home/htaf/hven/yago
state_root=/home/htaf/.local/state/yago-dev-agent
worktree_root="$state_root/worktrees"
log_file="$state_root/agent.log"
processed_file="$state_root/last-processed-key"
schema_file="$repo_root/dev-agent/reconciliation_result.schema.json"
live_base_url=https://yago.zvorygin.ca

mkdir -p "$worktree_root"
chmod 700 "$state_root" "$worktree_root"
exec >>"$log_file" 2>&1
exec 9>"$state_root/lock"
flock -n 9 || exit 0

cd "$repo_root"
git fetch --quiet origin dev main

dev_sha=$(git rev-parse refs/remotes/origin/dev)
main_sha=$(git rev-parse refs/remotes/origin/main)
dev_pr_state=$(gh pr list \
	--repo D4rk4/yago \
	--head D4rk4:dev \
	--base main \
	--state open \
	--json number,headRefOid,mergeStateStatus,statusCheckRollup \
	--jq 'sort_by(.number)')
integration_pr_state=$(gh pr list \
	--repo D4rk4/yago \
	--head D4rk4:codex/dev-integration \
	--base main \
	--state open \
	--json number,headRefOid,mergeStateStatus,statusCheckRollup \
	--jq 'sort_by(.number)')
pr_state=$(printf '%s\n%s\n' "$dev_pr_state" "$integration_pr_state")
run_key=$(printf '%s\n%s\n%s\n' "$dev_sha" "$main_sha" "$pr_state" | sha256sum | cut -d ' ' -f 1)

if git merge-base --is-ancestor "$dev_sha" "$main_sha"; then
	printf '%s\n' "$run_key" >"$processed_file"
	exit 0
fi

if [ -f "$processed_file" ] && [ "$(<"$processed_file")" = "$run_key" ]; then
	exit 0
fi

worktree_path="$worktree_root/$dev_sha"
if [ ! -e "$worktree_path/.git" ]; then
	git worktree add --detach "$worktree_path" "$dev_sha"
fi

prompt_file="$state_root/prompt-$dev_sha.md"
result_file="$state_root/result-$dev_sha.json"
cat >"$prompt_file" <<EOF
Read AGENTS.md and DEV-AGENTS.md before acting. The current development head is $dev_sha and the current main head is $main_sha. Follow the scheduled push-request agent workflow in DEV-AGENTS.md. Run make verify and the read-only Playwright live smoke scenarios against $live_base_url in Chromium and Firefox. When UI code changed, also run the corresponding local browser validation. Repair test discrepancies on dev, write or update the tests, and rerun validation before pushing a repair. For integration, update the stable codex/dev-integration branch from origin/main, merge origin/dev with a merge commit, restore main's AGENTS.md, remove DEV-AGENTS.md and dev-agent/, push the branch, and create or update its pull request to main. Merge only when all validation and GitHub checks are green and GitHub allows the merge. Do not bypass protections or deploy/restart production. Do not copy live response or corpus data into source, tests, logs, commits, or PR text. Return JSON with status processed or blocked, dev_sha exactly $dev_sha, and a concise summary.
EOF

codex exec \
	-C "$worktree_path" \
	--sandbox workspace-write \
	--ask-for-approval never \
	--output-schema "$schema_file" \
	--output-last-message "$result_file" \
	- <"$prompt_file"

grep -Fq "$dev_sha" "$result_file"
grep -Eq '"status"[[:space:]]*:[[:space:]]*"(processed|blocked)"' "$result_file"
printf '%s\n' "$run_key" >"$processed_file"
