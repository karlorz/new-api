# Responses WebSocket dual-release design

## Summary

Add Codex-compatible Responses WebSocket support to two reproducible New API release lines:

- Current line based on upstream `v1.0.0-rc.23`, released by the fork as `v1.0.0-rc.23-0`.
- Legacy line based on upstream `v0.13.2`, released by the fork as `v0.13.2-0`.

The implementation ports the behavior of QuantumNous/new-api pull request #5062 and the safe backend hardening from Yorick-Ryu/new-api. It preserves full token accounting for `function_call_output`, adds release-grade connection limits and billing behavior, and publishes multi-architecture images to both Docker Hub and GitHub Container Registry.

## Goals

1. Support the Codex Responses WebSocket transport at `GET /v1/responses`.
2. Preserve the existing HTTP/SSE Responses endpoint at `POST /v1/responses`.
3. Support multiple sequential `response.create` calls over one authenticated WebSocket.
4. Integrate WebSocket calls with New API channel selection, rate limiting, concurrency, billing, channel shutdown, and usage logging.
5. Backport the same externally observable contract to `v0.13.2` without forcing current-line internal architecture onto the legacy codebase.
6. Preserve upstream tags and identify fork releases with a numeric suffix.
7. Publish immutable, multi-architecture fork images to `karlorz/new-api` and `ghcr.io/karlorz/new-api`.

## Non-goals

- Do not merge Yorick-Ryu's complete branch history.
- Do not import unrelated Yorick-Ryu UI, deployment, pricing, workflow, or branding changes.
- Do not move, recreate, or overwrite upstream tags.
- Do not publish fork images under `calciumion/new-api`.
- Do not make the legacy branch internally identical to the current branch when its architecture differs.
- Do not add a continuously rebased rolling-upstream release line.

## Source lineage

The implementation has two upstream sources:

- QuantumNous/new-api PR #5062 at `95e73032a327c4ef36ee07f3db6ff6243d1bbd00` supplies the canonical Responses WebSocket relay behavior.
- Yorick-Ryu/new-api supplies the reviewed resource, billing, and shutdown hardening, principally from commits `5551cab71`, `b3a197e98`, `eb76195bf`, `d6942d680`, and `954cb82cb`.

The legacy and current ports must retain source attribution in commit messages. Yorick-Ryu commit `2f1ca8a01` is intentionally excluded because it narrows tool-output pre-consumption. The fork must continue accounting for large `function_call_output` content.

## Branch and tag model

### Current release line

```text
upstream tag:  v1.0.0-rc.23
fork branch:   release/responses-websocket-v1
fork tag:      v1.0.0-rc.23-0
```

The current integration begins from the existing fork `main`, which already contains upstream `v1.0.0-rc.23` and the fork's existing local commits. The upstream tag must continue pointing at the original upstream commit.

### Legacy release line

```text
upstream tag:  v0.13.2
fork branch:   release/responses-websocket-v0.13
fork tag:      v0.13.2-0
```

The legacy integration begins from the exact upstream `v0.13.2` tag. Only explicitly selected fork compatibility or deployment files may be added. Current-line application code must not be merged wholesale into this branch.

### Fork tag rule

An upstream release tag is preserved exactly. A fork release based on it appends a numeric prerelease/build iteration suffix:

```text
upstream v1.0.0-rc.23 -> fork v1.0.0-rc.23-0
upstream v1.0.0-rc.23 -> next fork revision v1.0.0-rc.23-1
upstream v0.13.2      -> fork v0.13.2-0
```

When the upstream base changes, the fork counter resets to zero. The release workflow accepts only fork-suffixed tags and refuses unsuffixed upstream tags.

## HTTP and WebSocket routing

The relay router exposes both transports:

```text
POST /v1/responses -> existing HTTP/SSE Responses relay
GET  /v1/responses -> Codex Responses WebSocket controller
```

The GET route uses the release line's existing token authentication and model request-rate middleware. Where the current line provides group-concurrency middleware compatible with a long-lived connection and per-call accounting, it is also applied. The legacy port implements an equivalent bounded policy using the closest existing middleware seam rather than copying incompatible current code.

## Session lifecycle

1. Authenticate the WebSocket upgrade without logging credentials.
2. Accept Codex CLI connections that omit `Origin`.
3. Validate any supplied browser `Origin` against trusted same-origin or configured allowlist policy.
4. Upgrade with compression disabled.
5. Reserve the per-user WebSocket session slot.
6. Read the first `response.create` frame within configured size and idle bounds.
7. Select the effective model and New API channel using the request body.
8. Dial the supported native OpenAI/Codex upstream Responses WebSocket.
9. Relay typed Responses events bidirectionally.
10. Settle each `response.create` independently.
11. Permit another sequential `response.create` on the same client connection.
12. Release all session, channel, concurrency, and billing state on every close path.

The initial scope does not add timwenx's generic HTTP/SSE fallback bridge. That is a separable compatibility feature and is not required to merge PR #5062 plus the selected Yorick-Ryu hardening. Keeping it out reduces billing and retry ambiguity in the first fork release.

## Protocol behavior

The controller must preserve Codex's relevant request headers, including the Responses WebSocket beta header, and accept the authenticated WebSocket GET generated from an HTTPS `/v1` base URL.

Client text frames are parsed as Responses events. The first and subsequent billable call event is `response.create`. The relay forwards upstream Responses event JSON until a terminal condition, including:

- `response.completed`
- `response.failed`
- `response.incomplete`
- protocol error
- upstream close
- client disconnect
- idle timeout
- channel policy shutdown

The controller must not retry a call after billable output has been exposed unless idempotency can be proven. Control errors returned to the client must use Responses-compatible error payloads and must not expose credentials or sensitive upstream URL components.

## Resource and security controls

Both release lines must provide equivalent external safety controls, even if their configuration wiring differs:

- Explicit client and upstream read limits.
- `WEBSOCKET_MAX_MESSAGE_MB`, with a safe documented default and deployment override.
- Explicit write deadlines.
- WebSocket compression disabled so compressed data cannot bypass read limits.
- Idle timeout.
- `RESPONSES_WEBSOCKET_MAX_PER_USER`, defaulting to 8 unless the release line has a stricter existing policy.
- Separate synchronization for connection-target changes and writes, preventing a blocked writer from indefinitely blocking shutdown.
- Channel disable/delete closure through the WebSocket manager.
- Cross-node closure through Redis when Redis is configured.
- Sanitized dial, close, and relay errors.

Origin policy is:

- Missing `Origin`: allowed after normal token authentication, supporting Codex and other non-browser clients.
- Present `Origin`: allowed only when it matches the configured trusted policy.

An unconditional `CheckOrigin: true` is not acceptable for the fork release.

## Billing and quota invariants

Each `response.create` is a separate accounting unit even when the socket is reused.

Settlement rules are:

- No upstream acceptance, output, or usage: refund the pre-consumed quota.
- Successful terminal response: settle actual observed usage.
- Failed or incomplete terminal response after output or usage: settle observed usage.
- Client disconnect after output or usage: settle observed usage.
- Channel retry before billable output: allowed under existing retry policy.
- Channel retry after billable output: prohibited unless proven idempotent.

Large `function_call_output` payloads remain part of token estimation and pre-consumption. The current line must use rc.23's checked quota-conversion and saturation-audit facilities. The legacy line must preserve its native billing model while adding explicit overflow and negative-charge protections where the backport introduces new arithmetic.

## Usage logging and observability

Usage records identify Responses WebSocket calls as streaming WebSocket traffic without changing the existing public log schema unnecessarily. Timing fields should distinguish at least connection/setup time and first upstream event where the existing log model supports them.

The current frontend may receive Yorick-Ryu's small WebSocket indicator changes after adapting them to the current React application. The legacy line does not require a frontend port if its log UI structure would make that change disproportionate; backend usage metadata remains mandatory.

Logs must not include bearer tokens, WebSocket subprotocol credentials, raw credential-bearing URLs, or unsanitized upstream dial errors.

## Container release workflow

GitHub Actions publishes each accepted fork tag to both registries:

```text
Docker Hub: karlorz/new-api
GHCR:       ghcr.io/karlorz/new-api
```

The workflow builds native or emulated `linux/amd64` and `linux/arm64` images, then creates multi-architecture manifests. The exact Git tag is written into `VERSION` and embedded in the frontend and Go binary.

### Immutable tags

For `v1.0.0-rc.23-0`:

```text
karlorz/new-api:v1.0.0-rc.23-0
ghcr.io/karlorz/new-api:v1.0.0-rc.23-0
```

For `v0.13.2-0`:

```text
karlorz/new-api:v0.13.2-0
ghcr.io/karlorz/new-api:v0.13.2-0
```

### Floating aliases

Only the current v1 release line updates:

```text
karlorz/new-api:latest
ghcr.io/karlorz/new-api:latest
```

The legacy `v0.13.2-0` release never updates `latest`. It may publish a track alias such as `v0.13` only if the workflow can derive it deterministically and test it without confusing it with an upstream tag.

### Workflow authentication

- GHCR uses the repository `GITHUB_TOKEN` with `packages: write`.
- Docker Hub uses `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN` repository secrets.
- The workflow fails before building if a required secret or tag rule is invalid.
- Registry image targets are fork-owned. No workflow publishes to `calciumion/new-api`.
- Images use OCI source, revision, and version labels.
- Published manifests should be signed when the repository's existing signing setup and secrets permit deterministic signing in both registries.

## Validation strategy

### Shared behavioral tests

- Bearer-token and WebSocket-subprotocol authentication.
- Missing-Origin CLI connection accepted.
- Untrusted browser Origin rejected.
- First `response.create` channel selection.
- Complete Responses event relay.
- Sequential response calls on one connection.
- Native upstream failure and incomplete response handling.
- Client disconnect before and after observable output.
- Partial-output billing and refund behavior.
- Large `function_call_output` token estimation.
- Per-user session cap and slot release.
- Read limit, write deadline, and idle timeout.
- Channel disable/delete local closure.
- Redis cross-node close behavior where a deterministic fixture exists.
- Credential-safe errors and logs.

### Current-line validation

- Focused package tests for controller, relay, middleware, service, and `pkg/wsmanager`.
- Race tests for the WebSocket relay and manager.
- Root Go build and relevant test suite.
- Independent `relaykit` build if any affected dependency or API crosses into that module.
- Frontend typecheck/build if usage-log UI changes are included.
- GitHub Actions syntax and tag-policy tests.

### Legacy-line validation

- Equivalent focused relay, billing, authentication, and resource-limit tests adapted to the v0.13.2 architecture.
- Root Go build and relevant tests available in that release.
- Container build for both architectures or a local single-platform validation plus Actions matrix validation.

### Release gates

Before creating either fork tag:

1. The associated upstream tag resolves to its original upstream commit.
2. The release branch has no unrelated imported Yorick-Ryu changes.
3. Required tests pass.
4. The embedded version equals the intended fork tag.
5. The workflow accepts the fork tag and rejects the unsuffixed upstream tag.
6. Docker Hub and GHCR targets contain only the fork namespace.

## Delivery sequence

1. Implement and validate the current `v1.0.0-rc.23` line first.
2. Create annotated tag `v1.0.0-rc.23-0` only after validation.
3. Build the release workflow and verify tag/registry behavior without publishing from unapproved tags.
4. Create the legacy branch from exact upstream `v0.13.2`.
5. Backport the proven current-line behavior to the legacy architecture.
6. Validate and create annotated tag `v0.13.2-0`.
7. Push branches and tags only when explicitly requested or when the user confirms remote publication.

## Acceptance criteria

- Codex no longer falls back because of `404 GET /v1/responses` on either release line.
- Multiple sequential Codex Responses calls work over one socket.
- Partial-output failure and disconnect cannot produce free usage.
- Large tool outputs remain included in accounting.
- Connections are bounded by message size, idle time, write deadlines, and per-user session count.
- Browser Origins are validated while no-Origin authenticated CLI clients remain supported.
- Upstream tags remain unchanged.
- Fork tags are `v1.0.0-rc.23-0` and `v0.13.2-0`.
- Images are available from both `karlorz/new-api` and `ghcr.io/karlorz/new-api` after the release workflow is intentionally run.
- Only the v1 release line updates the `latest` image alias.

