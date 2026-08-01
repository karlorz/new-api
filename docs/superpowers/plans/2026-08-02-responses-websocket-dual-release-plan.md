# Responses WebSocket dual-release implementation plan

## Objective

Deliver Codex-compatible `GET /v1/responses` support on current and legacy New API release lines, harden the relay using the approved Yorick-Ryu subset, preserve upstream tags, and publish fork images to Docker Hub and GHCR.

## Phase 1: Current rc.23 integration

1. Record exact upstream tag objects and verify `v1.0.0-rc.23` resolves to upstream commit `0ab02020603d22e5613bc4cf46bfab06f8567769`.
2. Port the consolidated PR #5062 implementation from Yorick-Ryu commit `5551cab71`, retaining attribution to QuantumNous PR #5062 and resolving it against the current rc.23 architecture.
3. Port dedicated channel-status socket closure from `b3a197e98` where rc.23 does not already cover the path.
4. Port resource, interrupted-billing, and shutdown hardening from `eb76195bf`.
5. Port configurable message-limit alignment from `d6942d680`, adapting it to rc.23 settings without weakening the configured upper bound.
6. Port WebSocket streaming usage-log marking from `954cb82cb`.
7. Adapt the small usage-log WebSocket indicator from `46503ed40` only if it maps cleanly to the current frontend.
8. Preserve full tool-output accounting and do not port `2f1ca8a01`.
9. Replace permissive Origin handling with authenticated no-Origin CLI support plus validation of supplied Origins.
10. Apply group-concurrency middleware or equivalent per-call policy without holding a group slot for an idle reusable socket.

## Phase 2: Current-line validation

1. Format all changed Go files.
2. Run focused tests for relay, middleware, service, controller, DTO, and `pkg/wsmanager`.
3. Run focused race tests for the WebSocket relay and manager.
4. Run root Go build and the broadest practical backend test set.
5. If relaykit is touched, run `cd relaykit && GOWORK=off go build ./...`.
6. If the current frontend is changed, run Bun typecheck and build.
7. Add or adapt deterministic regression tests for authentication, Origin policy, connection reuse, billing after partial output, session caps, and slot release.

## Phase 3: Dual-registry release automation

1. Replace fork-inappropriate `calciumion/new-api` publishing targets with `karlorz/new-api` and `ghcr.io/karlorz/new-api` in the fork release workflow.
2. Trigger release publication only for numeric fork-suffixed tags matching the approved current or legacy form.
3. Reject unsuffixed upstream tags before build or publication.
4. Build `linux/amd64` and `linux/arm64` images and create manifests in both registries.
5. Authenticate Docker Hub with `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN`.
6. Authenticate GHCR with `GITHUB_TOKEN` and `packages: write`.
7. Write the exact fork tag into `VERSION` and OCI version/revision labels.
8. Update `latest` only for the current v1 fork release line.
9. Keep legacy releases immutable and non-latest.
10. Add a deterministic local validation script or workflow test for accepted/rejected tags and alias selection.

## Phase 4: Current release tag

1. Verify the upstream `v1.0.0-rc.23` tag has not moved.
2. Verify the worktree is clean and all release gates pass.
3. Create annotated local tag `v1.0.0-rc.23-0` at the validated current-line release commit.
4. Do not push the branch or tag until remote publication is explicitly authorized.

## Phase 5: Legacy v0.13.2 backport

1. Create `release/responses-websocket-v0.13` from exact upstream tag `v0.13.2` (`bee339d279ccecbf8c8a89e14ddbbd902f78bd5d`).
2. Backport the external WebSocket contract using v0.13.2's router, middleware, relay, billing, and configuration seams.
3. Port the WebSocket manager, local/Redis channel closure, resource limits, write deadlines, idle timeout, and per-user cap in forms compatible with the legacy codebase.
4. Preserve complete tool-output accounting and safe partial-output settlement.
5. Add equivalent deterministic tests using the legacy test infrastructure.
6. Backport the dual-registry release workflow or ensure the shared workflow works from the legacy tag.
7. Validate build, tests, tag policy, and container build inputs.

## Phase 6: Legacy release tag

1. Verify the upstream `v0.13.2` tag has not moved.
2. Verify all legacy release gates pass.
3. Create annotated local tag `v0.13.2-0` at the validated legacy release commit.
4. Do not push the branch or tag until remote publication is explicitly authorized.

## Commit strategy

Keep reviewable commits separated by responsibility:

1. Design and implementation plan.
2. PR #5062 current-line port.
3. Yorick-Ryu current-line hardening.
4. Origin, concurrency, and accounting corrections.
5. Current frontend usage-log indicator, if included.
6. Dual-registry release workflow and tests.
7. Current release validation fixes.
8. Legacy PR #5062 backport.
9. Legacy hardening and tests.
10. Legacy release validation fixes.

## Stop conditions

- Do not weaken rc.23 billing or quota-saturation invariants to make the port compile.
- Do not silently drop a failing test that protects authentication, billing, resource bounds, or channel shutdown.
- Do not import unrelated Yorick-Ryu history.
- Do not move upstream tags.
- Do not publish to either registry or push tags without explicit authorization.

