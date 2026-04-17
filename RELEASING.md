# Releasing

This project ships stable releases following strict SemVer for v1:

- No breaking changes to exported Go APIs in v1 minor/patch releases.
- No breaking changes to event type names or event payload semantics in v1 minor/patch releases.
- No breaking changes to published Prometheus metric names/labels in v1 minor/patch releases.

Go support policy for v1:

- support current stable Go + previous stable Go
- keep CI green for both versions before tagging

## Release checklist

Run locally from repo root:

```bash
make test
make test-race
make test-integration
make vet
make lint
make vuln
make benchmark
make build
```

If `make vuln` reports vulnerabilities only in the Go standard library, upgrade to the latest patch release for your supported Go minor and rerun.

Review docs:

- `README.md` quick-start and observability examples are up to date.
- `CHANGELOG.md` contains release notes for the target tag.

## Recommended flow

1. Create release branch or use `main` after freeze.
2. (Optional) cut RC tag and validate:
   - `git tag v1.0.0-rc.1`
   - `git push origin v1.0.0-rc.1`
3. Trigger GitHub **release** workflow:
   - Input `tag`: `v1.0.0`
   - Input `target`: `main`
   - Input `prerelease`: `false`
4. Verify generated GitHub release notes.
5. Verify pkg.go.dev indexing for the release tag.
6. Verify README badges resolve to the tagged commit and default branch.

## Rollback notes

- If release checks fail before tag push: fix forward, re-run workflow.
- If tag is pushed but release content is wrong:
  - Create a corrective patch release (`v1.0.1`) rather than mutating history.
- Avoid deleting public v1 tags once consumers have fetched them.
