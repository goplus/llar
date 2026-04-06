---
name: publish-to-skills-registry
description: Publish a new skill or a new skill version into `github.com/MeteorsLiu/skills-registry`. Use when importing a skill from another repository, adding a new `{owner}/{repo}/{version}` entry, updating `INDEX.md`, or adding a repo-level `COMPARATOR.md` for version comparison guidance.
---

# Publish To Skills Registry

Publish verified skill contents into `github.com/MeteorsLiu/skills-registry` using the registry's current hosted layout and search conventions.

Keep registry edits exact and minimal. Do not invent namespaces, versions, summaries, comparator rules, or provenance metadata.

## Workflow

1. Verify the source and target namespace:
- Confirm the source skill location and the files that actually exist.
- Confirm the target namespace in `{owner}/{repo}` form.
- Decide whether this is the first published version for that repo or an additional version.
- If the source or namespace cannot be verified, stop and ask instead of guessing.

2. Choose the version directory:
- If the user supplies an exact version, use it.
- If the repo already has published versions, preserve that repo's existing verified versioning scheme.
- For a new repo with no user-specified version, default the first published version to `v0.1.0`.
- For a new repo that follows the default workflow, use a single leading `v` plus a SemVer 2.0.0 version, and follow the precedence rules defined at `https://semver.org/`.
- Treat `{owner}/{repo}/{version}` as an exact published artifact, not as a range or selector.
- Published version directories are immutable. Do not rewrite or "fix" an already published `{owner}/{repo}/{version}/`.
- If any file inside an already published version directory would need to change, publish a new version instead.
- For repos that follow SemVer, treat any skill-artifact change as a new patch release by default unless the user or repo-specific rule says otherwise:
  - `1.0.0 -> 1.0.1`
  - `v0.1.0 -> v0.1.1`
- This rule applies even when the change is only metadata or packaging, such as frontmatter normalization, `agents/openai.yaml` correction, or another fix inside the version directory.
- Do not invent an ad hoc versioning scheme. If you cannot verify a non-semver scheme, use the default `v0.1.0` starting point or ask the user.

3. Prepare repo-level files:
- Check whether `{owner}/{repo}/COMPARATOR.md` already exists.
- Every published repo must have `{owner}/{repo}/COMPARATOR.md`.
- Before creating or editing `COMPARATOR.md`, investigate how that repo's published versions are actually ordered.
- Base that investigation on verified evidence such as existing version directory names, upstream release tags, embedded version strings, release notes, or other repo-specific signals.
- If `{owner}/{repo}/COMPARATOR.md` is missing, create it before updating `INDEX.md` or proposing the publish PR.
- For a new repo that follows the default workflow, write `COMPARATOR.md` using the SemVer 2.0.0 precedence rules from `https://semver.org/`, applied after stripping one leading `v` from the directory name.
- Keep `COMPARATOR.md` limited to pairwise ordering for existing version directories.
- Do not put selection policy, "latest" policy, or installation workflow into `COMPARATOR.md`.
- If the ordering rule cannot be verified cleanly, do not guess. Ask the user for the intended rule or an authoritative source before creating or changing `COMPARATOR.md`.
- If a new semver comparator is needed, start from [`references/comparator-template.md`](./references/comparator-template.md) and keep it aligned with `https://semver.org/`.
- If the repo does not use semver, replace the template's comparison rule only after the repo's actual version-ordering rule has been investigated and verified.

4. Publish the version directory:
- Create `{owner}/{repo}/{version}/`.
- Copy the real skill contents into that directory, typically `SKILL.md` and any present `agents/`, `scripts/`, `references/`, or `assets/`.
- Preserve upstream contents unless an intentional registry-specific adjustment is required and verified.
- If the imported `SKILL.md` is missing YAML frontmatter, or its frontmatter is missing `name` or `description`, normalize it before publishing:
  - add minimal YAML frontmatter with only `name` and `description`
  - derive `name` from verified skill identity such as the upstream slug, folder name, or existing valid skill name
  - derive `description` from verified skill body and metadata, and make it describe both what the skill does and when it should be used
  - keep the skill body otherwise unchanged unless another verified registry-specific fix is required
- Do not leave a published `SKILL.md` in an invalid state just because upstream shipped malformed metadata.
- When importing from another repository or snapshot, preserve or record verified provenance only in the format already used by that repo or explicitly requested for this publish flow.
- Do not assume every skill repo uses `source.json` or any single provenance filename.

5. Update `INDEX.md`:
- Add or update one concise entry for `{owner}/{repo}`.
- Follow one uniform entry format for every repo. Keep the field names, field order, and heading shape consistent across the whole file.
- Start from [`references/index-entry-template.md`](./references/index-entry-template.md) when adding a new entry or normalizing an existing one.
- Keep the entry short and searchable: repo heading, name, summary, tags, latest published version, and comparator field.
- The comparator field is required and must point to the real `{owner}/{repo}/COMPARATOR.md`.
- Do not add or update the repo entry until that comparator file exists.
- If this is the first published version for the repo, set `Latest published version` to that version.
- If the repo already has published versions, update the `Latest published version` field only when verified comparison guidance shows the new version is newer.

6. Verify before proposing the change:
- Review the diff and keep only intended registry changes.
- Re-read copied files rather than assuming the copy was correct.
- Validate YAML frontmatter and `agents/openai.yaml` if they were added or changed.
- Confirm the namespace, version directory, and index entry agree with each other.
- Use `skill-creator` for final validation:
  - if `skill-creator` is not available locally, install it through Nexus before continuing
  - if Nexus is not available locally, bootstrap Nexus first from a verified source, then use Nexus to install `skill-creator`
  - run its `scripts/quick_validate.py` against the published `{owner}/{repo}/{version}` directory
  - if validation fails, fix the published skill and rerun validation until it passes
- If normalization changed `SKILL.md`, treat that normalized file as part of the publish diff and verify it explicitly.

7. Commit, push a branch, and open a PR:
- Commit the registry change with a message that identifies the published repo and version.
- Push the change to a branch, not directly to the default branch.
- Open a pull request against the registry repository.
- Use the PR description to summarize the namespace, version, index changes, comparator changes, and provenance handling.
- Report the published namespace, version, branch name, commit hash, and PR URL.

## Decision Rules

- First version for a repo:
  If the user did not specify a version, publish `v0.1.0`, create a semver `COMPARATOR.md`, and then add the `INDEX.md` entry.
- Additional version for an existing repo:
  Preserve the existing repo-level structure, and if `COMPARATOR.md` is missing, create or clarify it before publishing the new version.
- Existing published version needs any skill-file change:
  Do not modify that existing version directory. Publish a new version instead, and for semver repos default to a patch increment.
- Imported external skill:
  Preserve existing provenance conventions for that repo instead of forcing one metadata file shape on every skill.
- Malformed imported `SKILL.md`:
  Normalize it to a valid skill file with `name` and `description` before publishing, then validate it with `skill-creator`. If the malformed file was already published, publish a new version for the corrected artifact instead of rewriting the old version.
- `INDEX.md` entry format conflict:
  Normalize the entry to the shared template instead of inventing a one-off layout for that repo.
- Comparator investigation incomplete:
  Ask the user for the version-ordering rule or a trusted source before creating or updating `COMPARATOR.md`, and do not publish the repo entry without it.
- Registry conflict or ambiguity:
  Stop if the namespace, version, provenance, or comparator rule cannot be verified cleanly.

## Output

Return a compact publish summary with:

- source skill path or repository
- published namespace
- published version
- files added or updated
- whether `COMPARATOR.md` changed
- whether `INDEX.md` changed
- branch name and commit hash
- PR URL

## Example Requests

- `Publish the skill at this GitHub path into github.com/MeteorsLiu/skills-registry as a new repo.`
- `Add a new version of openai/pdf to skills-registry and update the index.`
- `Import this local skill folder into skills-registry under meteors/checks and record the upstream source metadata.`
