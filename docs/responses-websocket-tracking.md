# Codex Responses WebSocket fork tracking

This page tracks the fork-maintained Codex Responses WebSocket relay and the
conditions required to replace it with official upstream New API support.

> [!IMPORTANT]
> The relay described here is available on the maintained `karlorz/new-api`
> fork release branches. It is not yet part of an official tagged
> `QuantumNous/new-api` release. Do not retire the fork implementation merely
> because an upstream pull request is opened or merged.

## Status at a glance

Last verified: **2026-08-02**

<!-- markdownlint-disable MD013 -->

| Item | Status |
| --- | --- |
| Official upstream `GET /v1/responses` WebSocket relay | Not available in an official tagged release |
| Upstream implementation candidate | [QuantumNous/new-api PR #5062](https://github.com/QuantumNous/new-api/pull/5062), open and conflicting as of the last verification |
| Current fork branch | `release/responses-websocket-v1` |
| Current fork release | [`v1.0.0-rc.23-0`](https://github.com/karlorz/new-api/releases/tag/v1.0.0-rc.23-0) |
| Legacy fork branch | `release/responses-websocket-v0.13` |
| Legacy fork release | [`v0.13.2-0`](https://github.com/karlorz/new-api/releases/tag/v0.13.2-0) |
| Fork state | Maintained while waiting for a verified official upstream tag |

<!-- markdownlint-enable MD013 -->

## Why the fork exists

Codex can attempt to establish a WebSocket connection at:

```text
wss://<gateway>/v1/responses
```

Without an authenticated `GET /v1/responses` route, the request returns 404
and Codex falls back to an HTTPS transport. The fork adds a long-lived
Responses WebSocket relay while preserving the gateway responsibilities that
cannot safely be bypassed: authentication, model and channel selection, rate
limits, quota pre-consumption, observed-usage settlement, streaming logs,
channel lifecycle, and retry behavior.

The implementation lineage includes the protocol work represented by upstream
PR #5062, hardening and accounting work from the Yorick-Ryu fork line, and
additional integration, security, release, and lifecycle corrections made in
this fork. The complete release deltas are available here:

<!-- markdownlint-disable MD013 -->

- [Current release delta](https://github.com/karlorz/new-api/compare/v1.0.0-rc.23...v1.0.0-rc.23-0)
- [Legacy release delta: `v0.13.2...v0.13.2-0`](https://github.com/karlorz/new-api/compare/v0.13.2...v0.13.2-0)

<!-- markdownlint-enable MD013 -->

## Supported release lines

<!-- markdownlint-disable MD013 -->

| Line | Upstream base | Fork tag | Architectures | Docker `latest` |
| --- | --- | --- | --- | --- |
| Current | `v1.0.0-rc.23` | `v1.0.0-rc.23-0` | `linux/amd64`, `linux/arm64` | Yes |
| Legacy | `v0.13.2` | `v0.13.2-0` | `linux/amd64`, `linux/arm64` | Never |

<!-- markdownlint-enable MD013 -->

Published images:

```text
docker.io/karlorz/new-api:v1.0.0-rc.23-0
ghcr.io/karlorz/new-api:v1.0.0-rc.23-0
sha256:d1bfdb1200e50ad17a9a5e59e0c206566f87cf694eb10a5ab3222440f1027076

docker.io/karlorz/new-api:v0.13.2-0
ghcr.io/karlorz/new-api:v0.13.2-0
sha256:07335c8511794e69f82e393043e4304daa625b23ba0aa3e78f3f43bcc6c22284
```

The Docker Hub and GHCR `latest` tags are current-line aliases and must resolve
to the same digest as `v1.0.0-rc.23-0`. A v0.13 release must never update
`latest`.

The upstream tags remain preserved. Fork releases use an additional numeric
suffix and do not move, replace, or overwrite the corresponding upstream tag.

## Public protocol contract

The maintained fork contract is:

- Route: authenticated `GET /v1/responses` with a WebSocket upgrade.
- Supported subprotocols: `responses` and `realtime`.
- The first data event must be `response.create`.
- A connection may carry multiple sequential Responses calls.
- The first logical call performs channel selection and establishes the locked
  model/channel target. Each call independently performs model validation,
  rate-limit evaluation, quota pre-consumption, and settlement against that
  established target.
- Responses WebSocket upstream relay is limited to compatible OpenAI/Codex
  channel paths.
- Terminal response events complete the current call without forcing the
  client socket to close when the protocol permits another call.
- Invalid events, relay failures, channel closure, and idle timeout produce
  deterministic errors or close behavior.

The route and controller entry points are visible in the current release at:

- [`router/relay-router.go`](https://github.com/karlorz/new-api/blob/749eb8c605c01f7c368f3ff8540d89a3424161fc/router/relay-router.go)
- [`controller/relay.go`](https://github.com/karlorz/new-api/blob/749eb8c605c01f7c368f3ff8540d89a3424161fc/controller/relay.go)
- [`relay/responses_websocket.go`](https://github.com/karlorz/new-api/blob/749eb8c605c01f7c368f3ff8540d89a3424161fc/relay/responses_websocket.go)

The corresponding legacy implementation is branch-specific:

- [`router/relay-router.go`](https://github.com/karlorz/new-api/blob/9cde69dfcc3d622f3dea600ad77fb55554de6aef/router/relay-router.go)
- [`controller/relay.go`](https://github.com/karlorz/new-api/blob/9cde69dfcc3d622f3dea600ad77fb55554de6aef/controller/relay.go)
- [`relay/responses_websocket.go`](https://github.com/karlorz/new-api/blob/9cde69dfcc3d622f3dea600ad77fb55554de6aef/relay/responses_websocket.go)

## Safety and accounting contract

The fork implementation includes the following controls:

- WebSocket Origin validation when an Origin header is supplied.
- Per-message read limits.
- WebSocket compression disabled for the Responses relay.
- Idle read deadlines refreshed by data messages.
- Per-user active Responses WebSocket limits.
- Per-call model rate-limit checks and commits rather than a handshake-only
  allowance.
- Model/channel lock behavior that prevents unsafe switching after a session
  has established its supported target.
- Quota pre-consumption before upstream work.
- Observed-usage settlement and refund behavior after terminal events or
  failures.
- Streaming usage logs, including supported tool and image accounting on the
  current RelayKit line.
- Channel affinity and retry behavior.
- Local and Redis-backed connection registration so channel disable/delete can
  close affected sockets across nodes.
- Explicit process-shutdown closure is not currently implemented for hijacked
  Responses WebSockets. Channel lifecycle and idle-policy closes are covered;
  shutdown behavior remains a documented comparison point for future fork and
  upstream releases.

Relevant current-line sources:

- [`middleware/model-rate-limit.go`](https://github.com/karlorz/new-api/blob/749eb8c605c01f7c368f3ff8540d89a3424161fc/middleware/model-rate-limit.go)
- [`relay/common/websocket_idle.go`](https://github.com/karlorz/new-api/blob/749eb8c605c01f7c368f3ff8540d89a3424161fc/relay/common/websocket_idle.go)
- [`pkg/wsmanager/wsmanager.go`](https://github.com/karlorz/new-api/blob/749eb8c605c01f7c368f3ff8540d89a3424161fc/pkg/wsmanager/wsmanager.go)
- [`service/ws_close.go`](https://github.com/karlorz/new-api/blob/749eb8c605c01f7c368f3ff8540d89a3424161fc/service/ws_close.go)

## Configuration

<!-- markdownlint-disable MD013 -->

| Variable | Default | Purpose |
| --- | --- | --- |
| `WEBSOCKET_MAX_MESSAGE_MB` | Falls back to `MAX_REQUEST_BODY_MB` | Maximum accepted client WebSocket data message size |
| `MAX_REQUEST_BODY_MB` | 128 MiB | Standard effective fallback for the WebSocket message limit |
| `WEBSOCKET_IDLE_TIMEOUT_MINUTES` | 10 minutes | Idle read timeout, refreshed by data messages |
| `RESPONSES_WEBSOCKET_MAX_PER_USER` | 8 | Maximum concurrent Responses WebSockets per authenticated user |

<!-- markdownlint-enable MD013 -->

The implementation contains a final defensive 32 MiB fallback only if the
initialized `MAX_REQUEST_BODY_MB` value is also non-positive. Normal startup
initializes `MAX_REQUEST_BODY_MB` to 128 MiB.

Deployments behind Cloudflare or another reverse proxy must allow WebSocket
upgrade forwarding. Compose-based deployments should prefer their private
service network for application, database, and Redis communication; joining a
shared global Docker network can introduce ambiguous service-name resolution.

## Branch implementation distinction

The two release lines expose the same intended external contract, but they are
not byte-identical implementations:

- The v1 line follows the current architecture and RelayKit request, usage,
  performance, and tool/image accounting paths.
- The v0.13 line retains the legacy root DTO and relay architecture required by
  deployments pinned to the v0.13 database/application line.

Fixes must be reviewed and tested on both branches. A current-line RelayKit
change must not be assumed to apply directly to v0.13, and the legacy branch
must not receive a wholesale merge from current `main`.

## Release and registry policy

Fork release tags must:

- preserve the upstream base tag;
- add a numeric fork suffix, such as `-0`, `-1`, or a later integer;
- point to the exact intended release-branch commit;
- publish immutable multi-architecture images to Docker Hub and GHCR;
- include provenance/SBOM attestations and signatures where supported;
- update `latest` only for the current v1 line.

The release policy is implemented in:

- [`.github/workflows/docker-build.yml`](https://github.com/karlorz/new-api/blob/749eb8c605c01f7c368f3ff8540d89a3424161fc/.github/workflows/docker-build.yml)
- [`scripts/validate-fork-release-tag.sh`](https://github.com/karlorz/new-api/blob/749eb8c605c01f7c368f3ff8540d89a3424161fc/scripts/validate-fork-release-tag.sh)

## Deployment verification

Use all of the following checks before calling a deployment WebSocket-ready:

1. `GET /api/status` returns HTTP 200 and the expected fork version.
2. An unauthenticated WebSocket-upgrade probe to `GET /v1/responses` reaches
   New API and returns 401 rather than 404. This proves routing, not a complete
   authenticated WebSocket exchange.
3. An authenticated client receives `101 Switching Protocols` with a supported
   subprotocol.
4. A valid `response.create` exchange reaches terminal response events.
5. Multiple sequential calls on the same connection complete without stale
   events or cross-call leakage.
6. Usage logs, rate-limit state, quota settlement, and refunds match the
   completed or failed call.
7. Channel disable/delete closes an associated connection.
8. Idle timeout, message limit, per-user connection limit, invalid Origin, and
   malformed first-event behavior are exercised.
9. The test passes through the actual reverse proxy and deployment platform,
   not only through a local direct port.
10. The previously published image remains available for rollback.

During the recorded operator verification on 2026-08-02, the fork staging
deployment passed health and proxy routing: `/api/status` returned 200 and an
unauthenticated WebSocket-upgrade request reached the relay authentication
layer with 401 rather than the previous 404. This observation is deployment
evidence rather than a repository test. An authenticated 101 and complete
Responses exchange remain required whenever a new fork or official upstream
image is evaluated.

## Upstream watch list

Monitor:

- [QuantumNous/new-api PR #5062](https://github.com/QuantumNous/new-api/pull/5062),
  or any official successor implementation;
- upstream changes that register `GET /v1/responses`;
- controller, relay, billing, rate-limit, Origin, frame-limit, idle-timeout,
  connection-limit, and channel-lifecycle behavior;
- official New API release notes and tags;
- official tagged multi-architecture container images and digests;
- Codex transport behavior and fallback telemetry when relevant.

The tracker state should move through these stages:

```text
fork-maintained
    ↓
upstream-merged
    ↓
upstream-tagged
    ↓
staging-verified
    ↓
production-approved
    ↓
fork-relay-retired
```

## Official-support acceptance gate

Official upstream support can replace the fork only when a tagged upstream
artifact satisfies every applicable condition:

- Authenticated `GET /v1/responses` is present.
- The handshake returns 101 through the real Cloudflare/deployment path.
- Supported Responses subprotocol negotiation works.
- First-event validation and deterministic error/close behavior work.
- Multiple sequential Responses calls work on one connection.
- Channel/model selection, retries, affinity, and failure behavior are safe.
- Rate limits are evaluated per logical Responses call.
- Quota pre-consumption, observed usage, tool/image usage, settlement, refunds,
  and logs are correct.
- Origin validation, compression policy, message limits, idle timeout, and
  per-user connection limits are present.
- Channel disable/delete closes associated sockets. Process-shutdown behavior
  is explicitly tested and documented rather than assumed from HTTP server
  shutdown behavior.
- Official release notes and source identify the included implementation.
- A tagged, digest-pinnable multi-architecture official image is available.
- Fork-versus-upstream staging comparison and rollback tests pass.

An open PR, merged PR, untagged main-branch commit, unauthenticated 401 probe,
or mutable container tag is not sufficient by itself.

## Retirement and rollback procedure

When the acceptance gate passes:

1. Pin the official upstream image by version and digest in staging.
2. Run the complete deployment-verification checklist.
3. Compare protocol events, usage logs, rate limits, quota settlement, and
   channel lifecycle with the fork release.
4. Record the result and obtain production approval.
5. Deploy the official image while retaining the last verified fork image as
   rollback.
6. Observe the agreed production window.
7. Remove fork-specific relay code only in a separate reviewed change.
8. Archive this tracker only after the fork retirement and rollback decisions
   are recorded.

If verification fails, return to the last verified fork digest and record the
failed upstream version and failure reason. Do not move or overwrite existing
release tags.

## Maintenance record

<!-- markdownlint-disable MD013 -->

| Date | Event |
| --- | --- |
| 2026-08-01 | Published current fork release `v1.0.0-rc.23-0` to GitHub, Docker Hub, and GHCR. |
| 2026-08-01 | Published legacy fork release `v0.13.2-0` to GitHub, Docker Hub, and GHCR without changing `latest`. |
| 2026-08-02 | Verified both release manifests as `linux/amd64` and `linux/arm64`; verified `latest` points to the current-line digest. |
| 2026-08-02 | Operator verification observed staging health and a WebSocket-upgrade probe reaching `/v1/responses` authentication with 401 instead of 404. |
| 2026-08-02 | Confirmed upstream PR #5062 remains open and conflicting; fork remains the maintained implementation. |

<!-- markdownlint-enable MD013 -->

Update this table when an upstream implementation merges, an official tag is
published, staging verification is attempted, or the migration/rollback state
changes.
