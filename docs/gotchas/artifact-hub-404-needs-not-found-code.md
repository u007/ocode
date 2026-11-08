---
type: Gotcha
title: A hub 404 means the artifact is gone only when it carries code not_found
description: Hub unpublish and delete treat a 404 as already gone only when the hub body is JSON with code not_found; a plain 404 means the hub lacks the route, so local state must not change
resource: internal/artifacts/hubclient.go
tags:
  - artifacts
  - hub
  - publish
  - unpublish
  - delete
  - http-404
timestamp: 2026-11-08T12:40:00Z
---
Symptom

Deleting a hub-hosted artifact fails with 502 and a body like `hub delete: artifacts: hub delete artifact: status 404 (hub does not support this operation): <body>`. The reverse also matters: if every hub 404 were treated as success, an unpublish against an older hub would report success while the hub still serves the public link.

Cause

An HTTP 404 from the hub is ambiguous. Either the artifact is missing on the hub, or the hub does not serve the route at all. An older hub without `POST /api/ocode/artifacts/:id/unpublish` or `DELETE /api/ocode/artifacts/:id` answers with a plain 404 from its router. Status alone cannot tell these apart.

Rule

`HubClient.artifactOp` (`internal/artifacts/hubclient.go:231`) wraps `ErrNotFound` only when the 404 body parses as JSON with `code: "not_found"` (`hubNotFoundCode`, `internal/artifacts/hubclient.go:228`). Callers treat `ErrNotFound` as success: `removeArtifactFromHub` (`internal/server/artifact_hub.go:162`) and `undoFreshHubPublish` (`internal/server/artifact_hub.go:53`). Any other 404 is an error, and the local row keeps its status.

How to apply

When you add a hub call that can return 404, route it through `artifactOp` or match the same JSON `code` check. Do not compare on the status code alone.

Pinned by `TestHubClientArtifactOpNotFoundWrapsErrNotFound`, `TestHubClientArtifactOpPlainNotFoundIsRouteMissing`, `TestHandleHubPlain404FromMissingRouteKeepsLocalState` and `TestHandleDeleteHubArtifactHub404CountsAsGone`. Context: `concepts/artifact-publishing.md`, section "Hub-first unpublish and delete".
