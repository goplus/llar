---
name: manage-skills
description: Compatibility wrapper for Nexus. Use when an agent notices it is missing a relevant skill, needs to install a skill by namespace or verified source path, or needs to recover from a capability gap. For any real skill installation flow, verify Nexus first, bootstrap Nexus if needed, and then route discovery and installation through Nexus instead of independently choosing another install tool.
---

# Manage Skills

This skill is a compatibility entry point for environments that still reference `manage-skills`. For any actual skill installation, use Nexus as the manager. Do not let the model independently search for or choose another install tool outside the Nexus workflow.

Prefer verified evidence over host-name assumptions. Discovery remains restricted to the registry flow used by Nexus; do not use web search, repo search, or other registries to find candidate skills.

If Nexus is not yet installed locally, bootstrap Nexus first from a verified source, then delegate discovery and installation to Nexus. Do not directly choose an installer skill here unless Nexus itself is doing so internally.

Use `github.com/MeteorsLiu/skills-registry` as the default hosted registry.

## Workflow

1. Read the request and classify it:
- skill discovery only
- discovery plus installation
- installation from a known namespace or source path
- missing-capability recovery, where the model recognizes that the task would benefit from a skill it does not currently have
- Nexus detection or bootstrap only

2. Verify whether Nexus is already available locally:
- Prefer concrete filesystem or command evidence over product-name inference.
- For Codex, verify `$CODEX_HOME/skills/nexus/SKILL.md` or `~/.codex/skills/nexus/SKILL.md`.
- For OpenClaw, verify the local custom skills home and `nexus/SKILL.md` there before claiming Nexus exists.
- Treat missing evidence as unverified, not absent.
- If the user explicitly asked only whether Nexus exists, you may stop after this step.

3. If Nexus is missing, bootstrap-install Nexus first:
- Prefer a verified local Nexus source or a verified Nexus repository source.
- Use the verified Nexus bootstrap procedure to install Nexus into the current agent home.
- After bootstrap installation, verify again that Nexus now exists locally and is usable.
- If Nexus bootstrap fails, stop and report the exact failure instead of switching to an unrelated install tool.

4. Delegate the actual discovery or installation task to Nexus:
- Once Nexus is verified locally, use Nexus as the only manager entry point for skill discovery and installation.
- Let Nexus read `INDEX.md`, resolve candidate namespaces, resolve versions, and choose a compatible installer if needed.
- Do not independently search for installer skills, compare installer options, or invoke other ad hoc install tools outside the Nexus workflow.

5. Report the Nexus-mediated result:
- report whether Nexus was already present or had to be bootstrapped
- report the evidence used to verify Nexus
- report the delegated installation or discovery result from Nexus
- preserve any host-specific follow-up steps that Nexus returns

## Decision Rules

- Nexus already installed: delegate to Nexus immediately.
- Nexus missing but a verified Nexus source exists: bootstrap Nexus, then delegate to Nexus.
- Nexus missing and no verified Nexus source exists: explain what evidence is missing and stop instead of choosing another install tool.
- Discovery only: still route the request through Nexus once Nexus is available, because Nexus owns registry discovery.
- No verified skill match after Nexus runs: report Nexus's verified result directly; do not install a near match outside Nexus.

## Output

Return these fields in a compact summary:

- Nexus status
- evidence used for Nexus detection
- Nexus bootstrap source, if used
- capability gap that triggered discovery, if any
- delegated action through Nexus
- installation or discovery result
- required follow-up steps

## Example Requests

- `Check whether this environment has a skill installer and install the right skill for Terraform modules.`
- `Find a skill for writing PR descriptions, show the namespace, then install it.`
- `I want a PDF skill. Detect the installer, search relevant namespaces, and install the best match.`
