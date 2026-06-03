# Share the iframe-comms preamble between CCI and Chart Composer

**Status**: accepted (2026-06-03)

## Decision

`internal/chart/assets` will own the shared **iframe-comms primitives** that both Go chart engines inline into their customJS. The primitives live in a new embedded asset `internal/chart/assets/iframecomms.mjs`, exposed as `assets.IframeCommsJS` (sibling to the existing `assets.PhaseMarkersJS`). The `.mjs` is a block-comment-only IIFE that attaches three pure helpers to `window.__chartComms`:

1. `postToParent(msg)` — loops the `wailsParentOrigins` allowlist plus the `'*'` dev fallback,
2. `isFromParent(e)` — the inbound origin guard (`e.source === window.parent` or origin in allowlist),
3. `handlePngRequest(myChart, e, resultType)` — the `getDataURL`→reply-envelope half of the PNG round-trip.

`internal/cci/chart.go` (`addCCICustomJS`) and `internal/chart/composer.go` (`addComposerCustomJS`) now concatenate `assets.IframeCommsJS` into their customJS (alongside the already-shared `assets.PhaseMarkersJS`) and call `window.__chartComms.*`. **Each engine keeps its own `window.addEventListener('message', …)` listener and its own dispatch order** — control flow stays engine→shared. There is no shared dispatcher.

The `.mjs` is **byte-identical** for both engines because `myChart`, the inbound event, and the reply-type are all runtime arguments. The reply-type is passed as a **full literal** (`'cci-png-result'` / `'composer-png-result'`) rather than composed from a prefix, because request/reply suffixes are asymmetric (`-request-png` in vs `-png-result` out).

`CONTEXT.md` does not get a new term. `window.__chartComms` is iframe transport plumbing, not EMG analysis domain language.

## Why

This is the **iframe-comms-preamble sibling of [[ADR-0003]]**, the same way [[ADR-0010]] is its downsampling sibling. ADR-0003 unified the *parent-side* postMessage bridge (`frontend/src/charts/iframeBridge.mjs`) and shared the *iframe-side* phase-marker helpers via `//go:embed` (`assets.PhaseMarkersJS`). But the rest of the iframe-side comms bootstrap — the origin allowlist, `postToParent`, the inbound guard, the PNG reply handler — stayed hand-mirrored in each engine's customJS. This ADR extends the proven embed seam to also cover those comms primitives.

The duplication was real and self-documented. Both `addCCICustomJS` and `addComposerCustomJS` carried near-verbatim copies of `wailsParentOrigins` + `postToParent` (with the `'*'` dev fallback) + the inbound origin guard + the `getDataURL`→reply handler. `composer.go` even carried an explicit comment that it *duplicated the string rather than cross-package importing it* — that comment is now reversed: the cross-package asset is the source of truth.

`PhaseMarkersJS` already proves the mechanism. It is a block-comment-only IIFE attached to `window.__phaseMarkers`, embedded into **both** chart HTMLs, with the surrounding newline-stripped customJS calling into it. The comms preamble is the same shape with the same constraints, so this is a known-good deepening rather than a new pattern.

[[ADR-0003]] §Reversibility anticipated this moment. It flagged that a future third chart adapter might find "every adapter writes ~10 inline lines" annoying and want to abstract again, and warned that any such move must align with the existing bridge + phaseMarkers seam and **must not become a new third layer**. This ADR resolves that moment for the comms preamble: it shares the primitives at the maximum depth that still respects the no-third-layer rule, and explicitly stops short of a shared dispatcher (which *would* be the forbidden third layer — see Considered Options).

The block-comment-only constraint is load-bearing. go-echarts `AddJSFuncStrs` newline-strips the concatenated customJS; a `//` line comment would then eat to the next newline (or to `</script>`) and blank the chart silently. So `iframecomms.mjs` uses only `/* … */` block comments, and a dedicated `TestIframeCommsJS_BlockCommentOnly` guards it (the brace-balance syntax tests count braces lexically and would *not* catch a stray `//`).

## Considered Options

Three independent decision branches; the chosen leaf of each is marked.

**1. Depth of share**

- **Level 1 — primitives only, no shared namespace object**: expose just the functions, leave each engine to wire them. Rejected as strictly weaker than Level 2: it shares the same bytes but gives up the single `window.__chartComms` audit/grep surface for no benefit.
- **Level 2 — pure helpers on `window.__chartComms`, engines keep their own listener + dispatch (chosen)**: this is the **maximum depth that still respects [[ADR-0003]]'s no-third-layer rule**. Control flow stays engine→shared: each engine owns its `addEventListener('message')` and its own dispatch order, and calls into the shared helpers.
- **Level 3 — a shared dispatcher**: move the message listener and the `if (type === …)` routing into the shared asset too. Rejected: it would **invert control to shared→engine**, i.e. exactly the third layer [[ADR-0003]] §Reversibility forbids. The engines' dispatch bodies are genuinely divergent (CCI: restore/legend + phase-markers; Composer: standardize-zoom + phase-markers), so a shared dispatcher would have to re-expose those differences through callbacks anyway, buying nothing while violating the rule.

**2. Mechanism**

- **Pure helpers ⟹ byte-identical `.mjs` (chosen)**: because every engine-specific value (`myChart`, the event, the reply-type) is a runtime argument, the file is literally identical for both engines and embeds via `//go:embed` exactly like `PhaseMarkersJS`.
- **`%PREFIX%` substitution token**: parameterize the reply-type with a `%PREFIX%` placeholder substituted per engine at render time. Rejected: pure helpers take the value as an argument, so there is **no substitution site to template** — a placeholder would re-introduce per-engine string surgery that the runtime-argument design eliminates.
- **Go string-builder**: assemble the comms preamble in Go instead of embedding a `.mjs`. Rejected: it abandons the proven `.mjs`-embed precedent (`PhaseMarkersJS`) and gives up the source-string invariant tests that pin what actually ships in the iframe.

**3. Call convention**

- **Pass the FULL reply-type literal (chosen)**: callers pass `'cci-png-result'` / `'composer-png-result'` whole. The request/reply suffixes are **asymmetric** (`-request-png` inbound vs `-png-result` outbound), so a single prefix cannot derive both, and passing the full literal keeps the existing source-string invariant tests (CCI `chart_origin_test.go`, Composer source assertions) green.
- **Prefix-composition** (e.g. pass `'cci'`, let the helper build `cci-png-result`): rejected. It bakes the `<prefix>-png-result` naming convention into the shared helper, fails the asymmetric-suffix reality, and would force the source-string tests to chase a runtime concatenation instead of a literal.

## Consequences

- **One source of truth for the comms primitives.** `wailsParentOrigins`, `postToParent` (+ `'*'` dev fallback), the inbound origin guard, and the PNG reply handler exist once, in `iframecomms.mjs`, with one test suite (`internal/chart/assets/iframecomms_test.go`).
- **Engine-specific quirks stay home.** CCI keeps its `restore`/`legendselectchanged` handlers and its phase-markers assembly (category-axis `findNearestLabel`); Composer keeps its Bug 2 / Bug 3 / Bug F fixes, its `composer-standardize-zoom` handler (ADR-0013 D4/D5), and its phase-markers assembly (value-axis numeric). Each engine's dispatch order is its own (CCI: PNG → phase-markers; Composer: PNG → standardize-zoom → phase-markers).
- **`keydown`-`R` (reset zoom) and `resize` stay duplicated inline BY DESIGN.** They are not comms primitives — they touch no origin policy and no postMessage — so they are out of scope for this seam and remain a few inline lines in each engine.
- **The origins unit test migrated packages.** The former `internal/cci` JSON-array-validity assertion now lives in `internal/chart/assets` as `TestIframeCommsJS_WailsParentOriginsIsValidJSONArray`, asserting against the embedded string so the guard tracks what actually ships. The CCI-side `chart_origin_test.go` still source-asserts that the rendered HTML contains the `wailsParentOrigins` allowlist (now sourced from `assets.IframeCommsJS`) and that its URLs survive newline-stripping intact.
- **No frontend twin / no sync test.** Unlike `PhaseMarkersJS` (mirrored in `frontend/src` and sync-tested), the parent half of this protocol is a *different* implementation (`iframeBridge.mjs`), so `iframecomms.mjs` deliberately has no `.test.mjs` sync counterpart.

## Reversibility

Medium. The change is internal to `internal/chart/assets`, which both engines already inline at render time, so there is no dependency cycle and no public API commitment. Reverting is mechanically simple but would intentionally recreate the four-piece duplicated preamble and the same two-place drift surface (including the `composer.go` "duplicate string rather than cross-package import" comment).

## Related

- [[ADR-0003]] (parent — iframe bridge). This is its iframe-comms-preamble sibling. §6's symmetric phase-marker protocol stays as-is (both engines still receive the same `{checkedPhases}` shape and assemble markData against their own axis type). §Reversibility's anticipated "third layer" moment is **resolved here without introducing a third layer**: the share stops at pure helpers, control flow stays engine→shared.
- [[ADR-0009]] (PNG download safety pipeline). The iframe-side `handlePngRequest` is the **JS producer-half** of the PNG round-trip; ADR-0009's Go `downloadValidatedPNG` is the **consumer-half**. The two halves split along the postMessage boundary — this ADR owns the iframe side, ADR-0009 owns the Go validation/write side.
- [[ADR-0008]] (delete EChartsGenerator). It keeps CCI + Composer as the two live chart engines but says nothing about preamble-sharing; **this** ADR is what limits the new sharing to the iframe JS preamble and keeps the engines' divergent dispatch bodies separate.
- [[ADR-0005]], [[ADR-0006]], [[ADR-0010]] — deletion-test / shared-kernel family, for cross-reference. This decision is deliberately partial in the same spirit as [[ADR-0010]]: the comms primitives concentrate, while each engine's listener, dispatch order, and chart-specific handlers stay in the adapter.

## Process note

2026-06-03. This ADR documents an implemented change (Tasks 1–3); the design was settled in grilling beforehand. Cross-check against code at write time confirmed: `iframecomms.mjs` exists with the three `window.__chartComms` helpers and the byte-identical runtime-argument shape; `assets.IframeCommsJS` embeds it as a sibling of `assets.PhaseMarkersJS`; both `addCCICustomJS` and `addComposerCustomJS` concat it and call `window.__chartComms.*` while each retains its own message listener and dispatch order; and the origins JSON-array test now lives in `internal/chart/assets/iframecomms_test.go` (with `TestIframeCommsJS_BlockCommentOnly` guarding the newline-strip hazard).

ADR number: the worktree's `docs/adr/` showed numbers only up to `0018`, but `0019` (`0019-subject-output-name-primitive`) and `0020` (`0020-normalized-emg-write-via-csvhandler`) were already claimed by parallel sessions as untracked files in the main working copy (invisible from this worktree — the known worktree-isolation collision trap). This ADR therefore uses `0021`.
