# Publication checklist — owner action required

The repository is private, with no tag or release, as checked on 2026-10-16.
These are prepared instructions, not completed publication. Keep private-preview
copy until anonymous source, docs and installation have actually passed.

1. Review and merge the launch-readiness PR only when ready to deploy the landing.
   Review the MIT grant and ownership in LICENSE before publishing repository history.
   Keep the existing module path; renaming is optional and breaks install URLs.
2. Before any visibility change, prepare and review a dedicated PR moving **every
   workflow** (including `.github/workflows/release-build.yml`, build/deploy jobs,
   and reusable workflows) to standard GitHub-hosted runners. Keep the current
   `[self-hosted, jarvis]` runners while this repository remains private; this
   checklist does not migrate them now. As one owner-controlled publication
   transition, pause Actions, merge the reviewed migration, remove this repo's
   access to self-hosted runners, and verify no executable workflow can target
   Jarvis before changing visibility. Resume Actions only after that check.
   A public repository must never dispatch work to a self-hosted runner.

   Check the Actions budget before the transition: GitHub Pro includes 3,000
   minutes/month for private repositories; budget macOS at approximately 10×
   baseline Linux cost (current rates: $0.062 vs $0.006/minute). Confirm remaining
   allowance and spending limits; do not enable paid overages. Standard hosted
   runners are free for public repositories; larger runners are not.
   Sources: [Actions billing](https://docs.github.com/en/billing/concepts/product-billing/github-actions)
   and [runner pricing](https://docs.github.com/en/billing/reference/actions-runner-pricing),
   fetched 2026-10-16.
3. Only after step 2, owner reviews history/assets for publication, then changes repository visibility.
   Set description `Local one-shot model-and-effort router for Claude Code, opencode, and Codex`,
   website `https://jev-model-router.antoinelaborderie.com`, topics
   `claude-code,opencode,codex,llm-routing,golang,cli`. Enable Discussions if desired;
   install-help issues already have a sanitized template.
4. In a fresh authorized checkout of the approved main revision, run:

   ```sh
   go test ./...
   go vet ./...
   scripts/package-release.sh dist
   git tag v0.1.0
   git push origin v0.1.0
   gh release create v0.1.0 dist/*.tar.gz dist/SHA256SUMS --title 'mroute v0.1.0' --notes-file distribution/release-notes.md
   ```

   Check first that `v0.1.0` is still unused. Do not overwrite a tag or release.
   The manual `Prepare release archives` workflow alternatively packages the same
   four archives in one job (Jarvis while private; GitHub-hosted after publication) and uploads an Actions artifact; it does not
   publish a release. SHA256SUMS checks integrity, not publisher identity: archives
   are **not signed**. Signing is a separate owner-controlled identity/key decision.
5. From a machine without GitHub repository access, fetch source/README/LICENSE,
   issue template and plugin manifests; verify all return actual content, not 404.
   Install the real published tag:

   ```sh
   go install github.com/alaborderie/f-0923-jev-model-router/cmd/mroute@v0.1.0
   mroute init
   mroute doctor
   mroute route --json -- 'Rename foo to bar'
   mroute explain
   ```

   Use a fresh config and ensure the backend is heuristics for the no-key test.
   Configure supported model IDs in the catalogue. The founder then runs one
   `mroute run -- 'Explain a small local function'` with their authenticated engine;
   that may use their subscription or incur charges and is not an agent smoke test.
6. Only after step 5, apply `git apply distribution/public-access.patch` on a new
   branch and open a reviewed page/docs PR. This prepared patch removes preview
   labels/GOPRIVATE instructions and pins installs to the verified v0.1.0 tag.
   Run `git apply --check` first; if a different tag was selected, update the patch.
   The current labels are deliberately true while this preparation PR is open.
7. Submit only the approved channel drafts. Refresh upstream ecosystem patch
   against `dev` immediately before opening an upstream PR. No marketing issue
   comments are implied by an unlocked tracker. Do not post to the locked Claude issue.

## Measurement without telemetry

Record date, stars, download counts and voluntarily reported distinct installs.
Downloads/stars are proxies, not installs. Ask for synthetic reproducers, never a
raw config or full prompt log. No new analytics service or install telemetry is
required. Day 30: fewer than ten verified external installs with no organic star
growth means stop repeating launch posts and reassess. Day 60 target: 100 stars.
