# Changelog

## [Unreleased]

### Added

- M4-3: Linux amd64/arm64 release archives with SHA-256 checksums using GoReleaser v2, plus a versioned Helm chart attached to GitHub Releases.
- M4-3: A tag-triggered release workflow builds and publishes multi-platform images to `ghcr.io/<repository-owner>/<repository-name>`. It validates Go tests, GoReleaser configuration, Compose, and Helm before publication. Pull requests affecting release assets and manual workflow runs validate only.
- M4-4: A minimal Helm chart with a Service, retained SQLite PVC (or existing claim), existing Secret references, optional configuration ConfigMap, startup/readiness/liveness probes, and restricted pod/container security contexts.

### Changed

- Docker builds use Go 1.26.3 and BuildKit target-platform arguments, rather than forcing amd64. The runtime uses UID/GID 10001 and includes only the binary and runtime packages, not the repository's configuration or local data.
- Compose now uses a read-only root filesystem, dropped capabilities, no-new-privileges, a bounded temporary filesystem, rotated container logs, and a persistent named volume. It binds to loopback by default; set `FAKEMBY_BIND_ADDRESS` explicitly for LAN/reverse-proxy access.
- Compose requires `FAKEMBY_ADMIN_API_KEY` and `FAKEMBY_PLAYBACK_SIGN_KEY` from the shell or a secret manager, instead of shipping blank/placeholder credentials. Removed unused scraper environment settings from the deployment example.
- Container file logging is redirected to `/dev/null`; the application logger still writes to stderr. Database and image-cache writes stay under `/app/data`.
- Probes issue GET requests to the existing `/emby/System/Info/Public` route. They check HTTP responsiveness, not database readiness; no new health endpoint is claimed by these deployment changes.

### Deployment and upgrade notes

- Back up an existing SQLite deployment before changing storage. Stop the old instance cleanly, then migrate its `./data/db` contents into the new named volume and set ownership to `10001:10001`. Do not copy a live SQLite database without its consistent WAL state. Alternatively, retain the old bind mount at `/app/data` with matching ownership. No migration is automatic, and starting with an empty named volume creates a new database. Old cache contents are optional; file logs are no longer bind-mounted.
- Export strong, distinct, persistent values for both required Compose variables before `docker compose up --build -d`. Environment values remain visible to Docker administrators. Do not commit a secret-bearing `.env` or share rendered Compose configuration containing credentials. This application does **not** implement `secret_file` or `*_FILE` configuration; merely mounting a secret file would not load it.
- A configuration file is optional. Uncomment the read-only Compose mount or supply `config.existingConfigMap` in Helm if needed. Only supported `FAKEMBY_*` environment keys are used, and deployment environment values override the mounted file. The image never includes the repository's local `config.yaml`.
- Before installing Helm, create a Secret in the release namespace named by `secrets.existingSecret` (default `fakemby-secrets`) with nonempty keys `admin-api-key` and `playback-sign-key`. Override the key names in values if necessary. Values/history never need to contain the secret values. Restart the Deployment after rotating secrets or changing the optional subPath-mounted ConfigMap.
- The chart fixes replicas to one and uses `Recreate`; upgrades may briefly interrupt service. Do not scale the Deployment or share the SQLite volume with another release. A storage driver supporting `fsGroup` must make the claim writable by UID/GID 10001; provision ownership separately if the driver cannot. The chart-created PVC is retained on uninstall; use `persistence.existingClaim` to adopt retained data when reinstalling.
- Source chart `appVersion` records the historical baseline, not an assertion that an image exists for that tag. Override `image.tag` with a published version when installing from source. Released chart packages receive the actual release version automatically. Forks must override `image.repository` to their own registry path.

### Release conventions

- Release tags use `vMAJOR.MINOR.PATCH` or `vMAJOR.MINOR.PATCH-PRERELEASE`, without leading zeroes in numeric identifiers or build metadata. Examples: `v0.10.0`, `v0.10.0-rc.1`. These conventions are compatible with both SemVer and container tags.
- Before tagging, move relevant entries out of Unreleased into a dated version section. Keep application release versions separate from `server.version`, which advertises Emby protocol compatibility.
- A tag push publishes GitHub Release archives/checksums/chart and amd64/arm64 images. Container version tags omit the leading `v`; only stable releases update `latest`. Prerelease tags create GitHub prereleases and do not update `latest`.
- The workflow needs GitHub Actions permissions to write repository releases and GHCR packages. It derives registry ownership from the repository rather than hardcoding publication to an upstream account.
- Local checks can use `goreleaser check`, `helm lint --strict deploy/helm/fakemby`, `helm template fakemby deploy/helm/fakemby`, and `docker compose config --quiet` with non-secret validation values. GoReleaser writes to `dist/release`, not the existing `dist` runtime directory; the workflow packages charts separately under `dist/charts`.

## [0.9.0-pre]

- Historical pre-release baseline tag referenced by the roadmap. This entry does not claim that release archives or container images were published for that tag.
