# COMPARATOR.md Template

Use this template for repos that follow the default semantic-version workflow.
Under that workflow, the first version is `v0.1.0` when the user does not specify a version.

This template follows the SemVer 2.0.0 precedence rules from `https://semver.org/`.

If the target repo already uses a different verified versioning scheme, do not use this template blindly.
Investigate the repo's real ordering rule first, and ask the user if it is still unclear.

```md
---
name: "<owner>/<repo> comparator"
description: "Use when the model needs to compare version directories inside <owner>/<repo>. This document teaches only pairwise ordering for two concrete versions."
---

# <owner>/<repo> Comparator

## Purpose

Define how to compare two published versions under `<owner>/<repo>/`.

This document only defines ordering.
It does not define anything beyond pairwise comparison.

## Comparison Rule

Each version directory under `<owner>/<repo>/` is an exact published snapshot.

To compare two version directories:

1. Remove exactly one leading `v` from each version directory name.
2. Parse the remaining strings as valid SemVer 2.0.0 versions.
3. Compare `major`, `minor`, and `patch` numerically.
4. A version without a pre-release field has higher precedence than one with the same `major.minor.patch` and a pre-release field.
5. If both versions have pre-release fields, compare dot-separated pre-release identifiers from left to right:
   - compare numeric identifiers numerically
   - compare non-numeric identifiers lexically in ASCII sort order
   - treat numeric identifiers as lower precedence than non-numeric identifiers
   - if all compared identifiers are equal so far, the version with fewer pre-release identifiers has lower precedence
6. Ignore build metadata for precedence.
7. If the two versions still have equal precedence, treat them as semantically equal for ordering under this comparator.

## Model Guidance

- Compare only concrete published versions that already exist in the registry.
- Do not infer selection policy, installation workflow, or search behavior here.
- If a version directory name does not match the expected `v`-prefixed SemVer form, stop using this template and investigate the repo's actual versioning rule.
- Treat version directories as exact artifacts already published in the registry.

## Rationale

`<owner>/<repo>` follows `v`-prefixed SemVer 2.0.0 by default, so ordering comes from SemVer 2.0.0 precedence after removing the leading `v`.
```
