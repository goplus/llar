# INDEX.md Entry Template

Use this template for every repo entry in `INDEX.md`.
Keep the heading, field names, and field order identical across entries.

```md
### `<owner>/<repo>`

- Name: `<display name>`
- Summary: <one short searchable sentence>
- Tags: `<tag1>`, `<tag2>`, `<tag3>`
- Latest published version: `<version>`
- Comparator: [`<owner>/<repo>/COMPARATOR.md`](./<owner>/<repo>/COMPARATOR.md)
```

Formatting rules:

- Use exactly one `###` heading per repo entry.
- Use the canonical namespace `{owner}/{repo}` in the heading.
- Keep `Summary` to one short sentence.
- Keep `Tags` as a single comma-separated line of backticked tags.
- Keep `Latest published version` as the exact published directory name.
- `Comparator` is mandatory and must link to a real `{owner}/{repo}/COMPARATOR.md`.
- Do not add extra fields for one repo unless the registry format is intentionally changed everywhere.
