# Development branch workflow

These instructions apply only on the `dev` branch. Keep this file, the `dev-agent/` scheduler, and branch-specific edits to `AGENTS.md` out of integration changes to `main`.

## Development work

- Commit and push completed changes directly to `dev` without waiting for the scheduled validation agent. Tests still belong in the same change as the behavior they cover.
- Do not push directly to `main`, create release tags, deploy binaries, or restart production services.
- For each user-visible change, run Playwright scenarios by default. Use the local changed build for code behavior and `https://yago.zvorygin.ca` for read-only live smoke checks.
- From `yagonode/ui-tests`, install the locked package with `npm ci` and run `npx playwright test -c live-smoke.config.js`; the config defaults to `https://yago.zvorygin.ca` and covers Chromium and Firefox.
- Production browser scenarios may open the public portal and submit synthetic local-search queries. They must not log in, change settings, start or clear crawls, submit admin forms, or perform any other write. Do not deploy or restart production to make a scenario pass.
- Do not retain production result text, response bodies, screenshots, traces, or credentials in the repository. Visual screenshots for code under change belong to local test output and must not contain live result content.

## Scheduled push-request agent

- The local cron runner checks `origin/dev` and its integration pull request to `upstream/main`. It runs in a separate Git worktree so it cannot overwrite an active development checkout.
- Read this file and `AGENTS.md`, run `make verify`, run the live Playwright smoke scenarios in Chromium and Firefox, and run the matching local browser scenarios when UI code changes.
- If validation fails, identify the failing behavior, make the smallest correction on `dev`, add or repair the corresponding test, and rerun the required checks. Push a repair only after validation passes. If the failure cannot be resolved safely, leave the pull request open and report the exact failing check.
- The scheduled runner starts from a detached worktree. For a repair, create a temporary branch, confirm `origin/dev` still matches the supplied starting SHA, and push a validated repair to `origin/dev`; do not overwrite a newer development push.
- Prepare integration changes on the stable `codex/dev-integration` branch from `upstream/main`, using a Git merge commit from `origin/dev`. Retain the `upstream/main` version of `AGENTS.md` and exclude `DEV-AGENTS.md` and `dev-agent/` from the integration tree.
- Merge the integration pull request only after local validation passes, required GitHub checks pass, the branch is current with `main`, and GitHub permits the merge. Never bypass a required check or branch rule.
- An integration merge does not authorize a production deployment. Live Playwright checks remain read-only.
