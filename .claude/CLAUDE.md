# Dagda

Local Go CLI. Source and tests live in `engine/`. See `AGENTS.md`.

- Check with `make test`, `make check` and `make build`.
- Tests use small synthetic fixtures in temp directories, never real agent logs.
- Work out expected token and cost totals by hand, not from the code under test.
- Bucket dates in the local timezone.
- Unknown models stay unpriced. List-rate value is not a bill.
- Never commit secrets, personal logs, machine identifiers or session URLs.
- No commits, pushes, releases or publishing without asking.
