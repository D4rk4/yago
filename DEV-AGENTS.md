# Development branch workflow

These instructions apply only on the `dev` branch. Keep this file, the `dev-agent/` scheduler, and branch-specific edits to `AGENTS.md` out of integration changes to `main`.

## Development work

- Commit and push completed changes directly to `dev` without waiting for the scheduled validation agent. Tests still belong in the same change as the behavior they cover.
- Do not push directly to `main`, create release tags, or deploy to or restart production services.
- `https://yago.zvorygin.ca` is the development instance on this host. Development work includes building new binaries or images from `dev`, deploying them to that instance, restarting its services, and verifying the changed behavior there. Preserve its existing data and crawler frontier.
- For each user-visible change, run Playwright scenarios by default. Use the changed local build and the development instance for behavior checks.
- From `yagonode/ui-tests`, install the locked package with `npm ci` and run `npx playwright test -c live-smoke.config.js`; the config defaults to `https://yago.zvorygin.ca` and covers Chromium and Firefox.
- Browser scenarios may open the development portal and submit synthetic local-search queries. Crawl or admin writes require an explicit request for that behavior, as with the user's request to update and test the development crawler.
- Do not retain production result text, response bodies, screenshots, traces, or credentials in the repository. Do not copy private development corpus data into tests, logs, commits, or public issue text.

## Scheduled push-request agent

- The local cron runner checks `origin/dev` and its integration pull request to `upstream/main`. It runs in a separate Git worktree so it cannot overwrite an active development checkout.
- Read this file and `AGENTS.md`, run `make verify`, run the live Playwright smoke scenarios in Chromium and Firefox, and run the matching local browser scenarios when UI code changes.
- If validation fails, identify the failing behavior, make the smallest correction on `dev`, add or repair the corresponding test, and rerun the required checks. Push a repair only after validation passes. If the failure cannot be resolved safely, leave the pull request open and report the exact failing check.
- The scheduled runner starts from a detached worktree. For a repair, create a temporary branch, confirm `origin/dev` still matches the supplied starting SHA, and push a validated repair to `origin/dev`; do not overwrite a newer development push.
- Prepare integration changes on the stable `codex/dev-integration` branch from `upstream/main`, using a Git merge commit from `origin/dev`. Retain the `upstream/main` version of `AGENTS.md` and exclude `DEV-AGENTS.md` and `dev-agent/` from the integration tree.
- Merge the integration pull request only after local validation passes, required GitHub checks pass, the branch is current with `main`, and GitHub permits the merge. Never bypass a required check or branch rule.
- An integration merge does not authorize a production deployment. Scheduled live Playwright checks remain read-only.
