# Shared Chart Iframe-Comms Preamble (`window.__chartComms`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. On execution, copy this plan to `docs/superpowers/plans/2026-06-03-chart-comms-preamble.md` (repo home) — it currently lives in the plan-mode scratch path.

**Goal:** Extract the duplicated chart iframe-comms bootstrap (`postToParent` + origin-guard + PNG handler) from the two Go chart engines into ONE shared embedded `internal/chart/assets/iframecomms.mjs`, sibling to the existing `phasemarkers.mjs`.

**Architecture:** Level-2 share — pure helpers (`postToParent`, `isFromParent`, `handlePngRequest`) attach to `window.__chartComms` via a block-comment-only IIFE `//go:embed`-ed into both chart HTMLs. Each engine KEEPS ownership of its own `window.addEventListener('message', …)` and dispatch order (control flow stays engine→shared, exactly like `phasemarkers.mjs` — no third-layer dispatcher, per ADR-0003 §Reversibility). The `.mjs` is byte-identical for both engines because `myChart` and the reply-type are runtime args.

**Tech Stack:** Go 1.25, `go:embed`, go-echarts (`AddJSFuncStrs`), testify. No frontend/npm involvement (the `.mjs` is Go-embedded under `internal/chart/assets/`, not `frontend/dist`).

---

## Context — why this change

This is **Candidate #01** (the Top Recommendation) of the 2026-06-03 architecture review. Two live Go chart engines — `internal/cci/chart.go` (`addCCICustomJS`) and `internal/chart/composer.go` (`addComposerCustomJS`) — carry near-verbatim copies of the iframe postMessage bootstrap: the `wailsParentOrigins` allowlist, `postToParent` (+ `'*'` dev fallback), the inbound origin-guard, and the PNG `getDataURL`→reply-envelope handler (~40-50 lines ×2). `composer.go:914-916` explicitly flags the duplication as a known "duplicate string rather than cross-package import" tradeoff. The shared seam already exists (`assets.PhaseMarkersJS` is embedded into BOTH HTMLs), so the consolidation mechanism is proven; this deepens it to also cover the comms primitives.

The design was **fully crystallized in a grilling session** (`/improve-codebase-architecture` step 3) and an impl handoff. **This plan does not re-open design** — it operationalizes the settled decisions into a verifiable `step → verify` sequence. Design sources (do not re-derive):
- Design spec: `/tmp/claude-501/handoff-impl-chart-comms-preamble-20260603-132633.md`
- Verified-evidence grill: `/tmp/claude-501/handoff-grill-chart-bootstrap-20260603-114059.md`
- Design memory: `memory/project_chart_comms_preamble_grill_2026_06_03.md`
- ADRs: `docs/adr/0003` (iframe bridge — load-bearing), `0008` (two engines stay), `0009` (PNG Go-side write half), `0010` (LTTB — ADR-form template).

**Intended outcome:** one source of truth for the iframe-comms primitives, the engine-specific quirks untouched, every existing source-string invariant test still green, and a new ADR recording the extraction as the iframe-comms-preamble sibling of ADR-0003.

---

## Cross-check result (done this planning session) — ZERO DRIFT

Every `file:line` in the grill handoff was re-verified against current code on 2026-06-03. **No drift.** Verified anchors the tasks below cite:

| Anchor | File:line | Note |
|---|---|---|
| CCI `addCCICustomJS` | `internal/cci/chart.go:466` | concat at `:467` starts `assets.PhaseMarkersJS + \`` |
| CCI Go const `wailsParentOrigins` | `internal/cci/chart.go:454` | Go raw-string const (testable as a Go value) |
| CCI JS injection of the const | `internal/cci/chart.go:469` | `const wailsParentOrigins = \` + wailsParentOrigins + \`;` |
| CCI inline `postToParent` | `internal/cci/chart.go:470-484` | + `'*'` dev fallback |
| CCI restore/legend | `internal/cci/chart.go:494-499` | `cci-chart-restored` / `cci-chart-legend-changed` |
| CCI origin-guard | `internal/cci/chart.go:507` | `e.source !== window.parent && wailsParentOrigins.indexOf(e.origin) === -1` |
| CCI PNG branch | `internal/cci/chart.go:513-526` | `cci-request-png` → `getDataURL` → `cci-png-result` |
| CCI phase-markers listener | `internal/cci/chart.go:527-610` | KEEP verbatim (category axis) |
| Composer `addComposerCustomJS` | `internal/chart/composer.go:930` | concat at `:931` |
| Composer stale dup-comment | `internal/chart/composer.go:914-916` | becomes FALSE after refactor → update |
| Composer inline origins literal | `internal/chart/composer.go:933` | INLINE JS (no Go const) |
| Composer inline `postToParent` | `internal/chart/composer.go:934-946` | |
| Composer Bug2/3/F | `internal/chart/composer.go:948-969` | KEEP verbatim |
| Composer origin-guard | `internal/chart/composer.go:988` | same negated literal |
| Composer PNG branch | `internal/chart/composer.go:994-1009` | `composer-png-result` |
| Composer standardize-zoom / phase-markers | `internal/chart/composer.go:1010-1038 / 1039-1068` | KEEP verbatim |
| Assets embed | `internal/chart/assets/assets.go:16-17` | `//go:embed phasemarkers.mjs` → `var PhaseMarkersJS string` |
| `phasemarkers.mjs` (mirror template) | `internal/chart/assets/phasemarkers.mjs:1-52` | block-comment IIFE → `window.__phaseMarkers` |
| No assets test exists yet | `internal/chart/assets/` | the new `iframecomms_test.go` is the package's first test |

**Re-verify at execution start anyway** (repo norm — line numbers drift). The verify is the first task step.

---

## Invariants the plan MUST preserve (easy to silently break)

1. **Keep the JS var name `wailsParentOrigins`** in the `.mjs` (not `ORIGINS`) — it is the audit/grep token AND is asserted by tests (CCI `PostMessageOriginAllowlisted`, the migrated assets test).
2. **Pass the FULL reply type** (`'cci-png-result'` / `'composer-png-result'`) to `handlePngRequest`, never a bare prefix — request/reply suffixes are asymmetric (`-request-png` vs `-png-result`), and the full literal keeps `CustomJSSyntaxValid` / `HasRequestPngListener` green.
3. **`.mjs` is block-comment-only** (`/* */`). The whole concatenated customJS is newline-stripped by go-echarts `AddJSFuncStrs`; a `//` comment eats to the next newline and blanks the chart (image-#5 history). NOTE: the brace-balance + URL-intact source-string tests count braces *lexically* and do **not** reliably catch a stray `//` (a commented-out `}` is still counted), so Task 1 adds a direct `TestIframeCommsJS_BlockCommentOnly` guard on the `.mjs` source as the real tripwire.
4. **Do NOT move engine-specific code into the shared module.** Stays home: CCI restore/legend + `cci-update-phase-markers`; Composer Bug2/3/F + `composer-standardize-zoom` + `composer-update-phase-markers`; both phase-marker listeners (ADR-0003 §6).
5. **keydown-`R` and `resize` handlers stay duplicated inline — OUT OF SCOPE.** They are not comms primitives; the design scoped the share to comms + PNG only. Do NOT pull them into the `.mjs` (scope creep) and do NOT treat their remaining duplication as an incomplete refactor.
6. **CONTEXT.md: ZERO changes** (triple-confirmed by the ADR-0009/0010 precedents — `__chartComms` is transport plumbing, not an EMG domain term).
7. **Do NOT amend ADR-0003/0008/0009.** The repo consolidates via `## Related` links + "sibling of" framing, never by editing accepted ADRs.

---

## File Structure

| File | Responsibility | Action |
|---|---|---|
| `internal/chart/assets/iframecomms.mjs` | The shared comms IIFE — `wailsParentOrigins`, `postToParent`, `isFromParent`, `handlePngRequest` on `window.__chartComms`. Pure helpers, block-comment-only. | **Create** |
| `internal/chart/assets/assets.go` | Add `//go:embed iframecomms.mjs` → `var IframeCommsJS string` + doc note (no frontend twin). | Modify (`:17`→add) |
| `internal/chart/assets/iframecomms_test.go` | Migration home for the origins-allowlist unit test, asserting against `IframeCommsJS`. | **Create** |
| `internal/cci/chart.go` | `addCCICustomJS`: concat `IframeCommsJS`; delete Go const + JS injection + inline `postToParent`; rewire restore/legend + guard + PNG branch to `__chartComms.*`. | Modify (`:454`, `:460-484`, `:494-526`) |
| `internal/cci/chart_origin_test.go` | Delete migrated `TestWailsParentOrigins_IsValidJSONArray`; add `TestAddCCICustomJS_ChartCommsInjected`. | Modify (`:77-93` delete, add) |
| `internal/chart/composer.go` | `addComposerCustomJS`: concat `IframeCommsJS`; delete inline origins + `postToParent`; update stale dup-comment; rewire guard + PNG branch. | Modify (`:914-916`, `:931-946`, `:987-1009`) |
| `internal/chart/composer_test.go` | Rewrite `TestRenderComposer_RelaxedOriginCheck` (inversion); add `TestRenderComposer_ChartCommsInjected`. | Modify (`:483-494`, add) |
| `docs/adr/0021-chart-iframe-comms-preamble.md` | New ADR, sibling-of-0003 framing. `0021` because 0019/0020 are already taken (untracked, parallel sessions) — re-verify the next-free number at write time. | **Create** |

---

## Dependency / parallelism

```
Task 0 (baseline) ─▶ Task 1 (.mjs + embed + migrated test)
                              │
                    ┌─────────┴─────────┐
                    ▼                   ▼
            Task 2 (CCI rewire)   Task 3 (Composer rewire)   ← INDEPENDENT, parallelizable
                    └─────────┬─────────┘
                              ▼
                       Task 4 (ADR)   ← any time after design; write with the code
                              ▼
                       Task 5 (full verify + finishing)
```

- **Task 1 must complete before 2 and 3** (the shared artifact + its test must exist first).
- **Tasks 2 and 3 are independent** (CCI and Composer never touch each other) → two parallel subagents in the execute phase, both depending on Task 1. If parallelized in worktrees, watch for the `feedback_worktree_subagent_detached_head` trap.
- **Orphan cleanup folds INTO Task 2** (not a separate step): deleting the CCI Go const and removing `TestWailsParentOrigins_IsValidJSONArray` must land in the same commit, or the `cci` package won't compile.

---

## Task 0: Baseline + cross-check

**Files:** none (read-only verification).

- [ ] **Step 1: Re-verify line anchors**

Re-confirm the Cross-check table above against current code (line numbers drift). Spot-check the four load-bearing anchors:

Run:
```bash
grep -n 'wailsParentOrigins' internal/cci/chart.go internal/chart/composer.go
grep -n 'func addCCICustomJS\|func addComposerCustomJS' internal/cci/chart.go internal/chart/composer.go
grep -n 'go:embed' internal/chart/assets/assets.go
```
Expected: const at `cci/chart.go:454`, injection at `:469`; functions at `cci:466` / `composer:930`; one embed at `assets.go:16`. If drifted, update the task line citations before proceeding.

- [ ] **Step 2: Record a green baseline**

Run:
```bash
go test ./internal/cci/... ./internal/chart/... ./internal/chart/assets/...
```
Expected: PASS (all green). Record the output — later breakage is then attributable to this work.

---

## Task 1: Shared `.mjs` asset + embed + migrated origins test

**Files:**
- Create: `internal/chart/assets/iframecomms.mjs`
- Create: `internal/chart/assets/iframecomms_test.go`
- Modify: `internal/chart/assets/assets.go:17`

- [ ] **Step 1: Write the failing assets test**

Create `internal/chart/assets/iframecomms_test.go`. This is the migration home for CCI's `TestWailsParentOrigins_IsValidJSONArray` — the origins allowlist moved from a CCI Go const into the shared `.mjs`, so the unit guard follows it (asserting the embedded string that actually ships is stronger than the old Go-const check).

```go
package assets

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIframeCommsJS_WailsParentOriginsIsValidJSONArray migrates the former
// internal/cci TestWailsParentOrigins_IsValidJSONArray. The wailsParentOrigins
// allowlist moved from a CCI Go const into this shared .mjs; assert it against
// the embedded string so the guard tracks what actually ships in the iframe.
func TestIframeCommsJS_WailsParentOriginsIsValidJSONArray(t *testing.T) {
	expectedSchemes := []string{
		`"wails://wails"`,
		`"http://wails.localhost"`,
		`"https://wails.localhost"`,
	}
	for _, scheme := range expectedSchemes {
		assert.Contains(t, IframeCommsJS, scheme,
			"origin %s 必須以 quoted string 形式存在於 .mjs allowlist", scheme)
	}
	assert.Contains(t, IframeCommsJS,
		`var wailsParentOrigins = ["wails://wails", "http://wails.localhost", "https://wails.localhost"]`,
		"wailsParentOrigins 必須是 well-formed JS array literal")
}

// TestIframeCommsJS_ExposesHelpers asserts the three primitives land on the
// shared window namespace.
func TestIframeCommsJS_ExposesHelpers(t *testing.T) {
	for _, frag := range []string{
		`window.__chartComms`,
		`function postToParent`,
		`function isFromParent`,
		`function handlePngRequest`,
	} {
		assert.Contains(t, IframeCommsJS, frag, "IframeCommsJS 必須含 %s", frag)
	}
}

// TestIframeCommsJS_BlockCommentOnly is the real block-comment-only tripwire.
// go-echarts AddJSFuncStrs strips newlines/tabs from the concatenated customJS;
// a // line comment then eats to the next newline (or to </script>) and blanks
// the chart silently (image #5). The brace-balance syntax tests count braces
// lexically and would NOT catch this. Strip /* */ block comments + neutralize
// URL schemes (://), then assert no // line comment remains.
func TestIframeCommsJS_BlockCommentOnly(t *testing.T) {
	src := IframeCommsJS
	for {
		start := strings.Index(src, "/*")
		if start == -1 {
			break
		}
		rel := strings.Index(src[start:], "*/")
		require.NotEqual(t, -1, rel,
			"iframecomms.mjs 有未閉合的 /* */ block comment(無效 JS,會使圖表空白)")
		src = src[:start] + src[start+rel+2:]
	}
	src = strings.ReplaceAll(src, "://", ":@@") // wails:// http:// https:// are legit
	assert.NotContains(t, src, "//",
		"iframecomms.mjs 只能用 /* */ block comments;// line comment 會被 newlineTabPat 吃掉並使圖表空白")
}
```

- [ ] **Step 2: Run test to verify it fails (does not compile)**

Run: `go test ./internal/chart/assets/ -run TestIframeCommsJS -v`
Expected: FAIL — compile error `undefined: IframeCommsJS`.

- [ ] **Step 3: Create the shared `.mjs`**

Create `internal/chart/assets/iframecomms.mjs`. **Block-comment-only** (mirrors `phasemarkers.mjs` exactly). The array literal must match the test assertion byte-for-byte (spaces after commas).

```js
/* chart iframe-comms primitives (ADR-0003 family).

   Loaded into chart iframe customJS via //go:embed by assets.go; helpers
   exposed at window.__chartComms.{postToParent, isFromParent, handlePngRequest}
   for the inline customJS body (CCI + Composer) to consume. Pure helpers — all
   inputs (myChart, the event, the reply-type) are runtime args, which is what
   makes this file byte-identical for both engines.

   No frontend twin: unlike phasemarkers.mjs (mirrored in frontend/src and
   sync-tested), the parent side of this protocol is a different impl
   (frontend/src/charts/iframeBridge.mjs), so there is no .test.mjs sync test.

   IMPORTANT: block-comment-only (slash-star ... star-slash). The whole
   concatenated customJS is newline-stripped by go-echarts AddJSFuncStrs; a
   line comment would eat to the next newline and blank the chart (image #5). */
(function () {
  /* KEEP this exact var name wailsParentOrigins — audit/grep target + asserted
     by internal/chart/assets/iframecomms_test.go and CCI source-string tests. */
  var wailsParentOrigins = ["wails://wails", "http://wails.localhost", "https://wails.localhost"];
  function postToParent(msg) {
    for (var i = 0; i < wailsParentOrigins.length; i++) {
      try { window.parent.postMessage(msg, wailsParentOrigins[i]); } catch (e) {}
    }
    /* dev fallback: wails dev parent origin (http://localhost:34115, dynamic
       port) is not in the allowlist; post once with '*', the parent listener
       validates e.origin itself. */
    try { window.parent.postMessage(msg, '*'); } catch (e) {}
  }
  function isFromParent(e) {
    return e.source === window.parent || wailsParentOrigins.indexOf(e.origin) !== -1;
  }
  function handlePngRequest(myChart, e, resultType) {
    var id = e.data.requestId;
    try {
      var url = myChart.getDataURL({ type: 'png', pixelRatio: 2, backgroundColor: '#fff' });
      postToParent({ type: resultType, requestId: id, payload: { dataURL: url } });
    } catch (err) {
      postToParent({ type: resultType, requestId: id, error: String(err) });
    }
  }
  window.__chartComms = { postToParent: postToParent, isFromParent: isFromParent,
                          handlePngRequest: handlePngRequest };
})();
```

- [ ] **Step 4: Add the embed to `assets.go`**

Modify `internal/chart/assets/assets.go` — after the existing `PhaseMarkersJS` block (line 17), append:

```go
//go:embed iframecomms.mjs
// IframeCommsJS is the iframe-side shared comms preamble (ADR-0003 family):
// postToParent / isFromParent / handlePngRequest on window.__chartComms,
// concatenated into BOTH chart customJS strings. Unlike PhaseMarkersJS it has
// no frontend twin, so no sync test is required.
var IframeCommsJS string
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/chart/assets/ -v`
Expected: PASS (`TestIframeCommsJS_WailsParentOriginsIsValidJSONArray`, `TestIframeCommsJS_ExposesHelpers`, `TestIframeCommsJS_BlockCommentOnly`).

- [ ] **Step 6: Commit**

```bash
git add internal/chart/assets/iframecomms.mjs internal/chart/assets/iframecomms_test.go internal/chart/assets/assets.go
git commit -m "feat(assets): add shared iframe-comms preamble (window.__chartComms)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Rewire CCI (`addCCICustomJS`) + fold in orphan cleanup

**Files:**
- Modify: `internal/cci/chart.go:454`, `:460-484`, `:494-526` (+ concat `:467`)
- Test: `internal/cci/chart_origin_test.go` (delete migrated test `:77-93`; add `ChartCommsInjected`)

- [ ] **Step 1: Write the new failing test + delete the migrated one**

In `internal/cci/chart_origin_test.go`:

(a) **Delete** `TestWailsParentOrigins_IsValidJSONArray` (currently `:77-93`) — it references the Go const we delete in Step 3, and was migrated to the assets package in Task 1. (This deletion must ride with the const deletion, else `cci` won't compile.)

(b) **Add** (next to `TestAddCCICustomJS_PhaseMarkersInjected`, mirroring its shape):

```go
// TestAddCCICustomJS_ChartCommsInjected mirrors PhaseMarkersInjected: the
// shared assets.IframeCommsJS IIFE must be concatenated into the CCI customJS
// so the inline body can call window.__chartComms.* (ADR-0003 family).
func TestAddCCICustomJS_ChartCommsInjected(t *testing.T) {
	html := renderCCIToString(t)

	assert.Contains(t, html, `window.__chartComms`,
		"IframeCommsJS IIFE 必須 attach helpers 至 window.__chartComms")
	assert.Contains(t, html, `function postToParent`,
		"IframeCommsJS 必須含 postToParent declaration")
	assert.Contains(t, html, `function isFromParent`,
		"IframeCommsJS 必須含 isFromParent declaration")
	assert.Contains(t, html, `function handlePngRequest`,
		"IframeCommsJS 必須含 handlePngRequest declaration")
	// Pin the migration symmetrically: CCI's old inline negated guard must be
	// GONE too (same partial-refactor false-pass risk as composer). (codex R2.)
	assert.NotContains(t, html, `e.source !== window.parent`,
		"CCI 不可保留舊 inline negated guard;origin 驗證必須走 window.__chartComms.isFromParent")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cci/ -run TestAddCCICustomJS_ChartCommsInjected -v`
Expected: FAIL — `window.__chartComms` not in HTML (IframeCommsJS not yet concatenated).

- [ ] **Step 3: Rewire `addCCICustomJS`**

In `internal/cci/chart.go`:

(a) **Concat** (`:467`): `assets.PhaseMarkersJS + \`` → `assets.PhaseMarkersJS + assets.IframeCommsJS + \``

(b) **Delete** the Go const + its doc block: line `454` (`const wailsParentOrigins = …`) and the `// allowlist 過嚴…` doc-comment lines immediately above it (back to the blank line).

(c) **Delete** the JS injection line `469`:
```go
		const wailsParentOrigins = ` + wailsParentOrigins + `;
```

(d) **Delete** the inline `postToParent` block, lines `470-484` (`function postToParent(msg) { … try { window.parent.postMessage(msg, '*'); } catch (e) {} }`). After this, `let myChart = %MY_ECHARTS%;` (`:468`) is immediately followed by `if (myChart) {` (`:485`).

(e) **Update** the function doc comment (`:460-462`) so it no longer points at the deleted const's doc — replace with: origin validation now lives in `assets.IframeCommsJS` `isFromParent`. KEEP the newlineTabPat warning (`:464-465`).

(f) **Rewire** restore/legend calls (`:495`, `:498`):
```js
		myChart.on('restore', function() {
			window.__chartComms.postToParent({type: 'cci-chart-restored'});
		});
		myChart.on('legendselectchanged', function() {
			window.__chartComms.postToParent({type: 'cci-chart-legend-changed'});
		});
```

(g) **Rewire** the inbound origin-guard (`:507-509`):
```js
		window.addEventListener('message', function(e) {
			if (!window.__chartComms.isFromParent(e)) {
				return;
			}
			if (!e.data || typeof e.data !== 'object') {
				return;
			}
```

(h) **Rewire** the PNG branch (`:513-526`) — replace the whole `if (e.data.type === 'cci-request-png') { … }` block with:
```js
			if (e.data.type === 'cci-request-png') {
				window.__chartComms.handlePngRequest(myChart, e, 'cci-png-result');
				return;
			}
```

(i) **KEEP verbatim:** `let myChart` (`:468`), keydown-`R` + resize (`:485-493`), the `cci-update-phase-markers` listener (`:527-610`), and everything after.

- [ ] **Step 4: Run CCI tests to verify they pass**

Run: `go test ./internal/cci/ -v`
Expected: PASS — including the new `ChartCommsInjected`, and the SURVIVE-unchanged set still green:
- `TestAddCCICustomJS_PostMessageOriginAllowlisted` — `assert.Contains` is substring-based, so `window.__chartComms.postToParent({type: 'cci-chart-restored'})` still contains the asserted `postToParent({type: 'cci-chart-restored'})`; the `wailsParentOrigins` token + 3 URLs survive via `IframeCommsJS`.
- `HasRequestPngListener` (`cci-request-png` / `cci-png-result` / `getDataURL` — last two now in `IframeCommsJS` + the engine's request-type match), `SyntaxValid` (3 URLs + brace balance), `HasUpdatePhaseMarkersListener`, `PhaseMarkersInjected`.

If `getDataURL` assertion in `HasRequestPngListener` fails: confirm `IframeCommsJS` (which contains `getDataURL`) is in the rendered HTML — it must be, via the concat.

- [ ] **Step 5: Commit**

```bash
git add internal/cci/chart.go internal/cci/chart_origin_test.go
git commit -m "refactor(cci): use shared __chartComms preamble; drop inline comms dup

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Rewire Composer (`addComposerCustomJS`)

> Independent of Task 2; both depend only on Task 1.

**Files:**
- Modify: `internal/chart/composer.go:914-916`, `:931-946`, `:987-1009`
- Test: `internal/chart/composer_test.go` (rewrite `RelaxedOriginCheck` `:483-494`; add `ChartCommsInjected`)

- [ ] **Step 1: Write the new failing test**

In `internal/chart/composer_test.go`, add (use the same `ComposerInput` shape as `RelaxedOriginCheck`):

```go
// TestRenderComposer_ChartCommsInjected asserts the shared assets.IframeCommsJS
// IIFE is concatenated into the Composer customJS (ADR-0003 family).
func TestRenderComposer_ChartCommsInjected(t *testing.T) {
	in := ComposerInput{
		Subject:          "S",
		EMGDataset:       makeEMGDataset(50, "RA"),
		SelectedChannels: []string{"RA"},
		MotionData:       makeMotionData(50, "knee"),
	}
	html := renderToString(t, context.Background(), in)

	assert.Contains(t, html, `window.__chartComms`,
		"IframeCommsJS IIFE 必須 attach helpers 至 window.__chartComms")
	assert.Contains(t, html, `function postToParent`, "IframeCommsJS 必須含 postToParent")
	assert.Contains(t, html, `function isFromParent`, "IframeCommsJS 必須含 isFromParent")
	assert.Contains(t, html, `function handlePngRequest`, "IframeCommsJS 必須含 handlePngRequest")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/chart/ -run TestRenderComposer_ChartCommsInjected -v`
Expected: FAIL — `window.__chartComms` not yet in HTML.

- [ ] **Step 3: Rewire `addComposerCustomJS`**

In `internal/chart/composer.go`:

(a) **Concat** (`:931`): add `assets.IframeCommsJS`:
```go
	customJS := assets.PhaseMarkersJS + assets.IframeCommsJS + `
```

(b) **Update** the now-FALSE duplication comment (`:914-916`) — it argued for duplicating rather than importing; that decision is reversed. Replace with:
```go
// postToParent + wailsParentOrigins allowlist now live in the shared
// assets.IframeCommsJS preamble (window.__chartComms), concatenated below —
// see docs/adr/0003 + the iframe-comms-preamble ADR. KEEP the newlineTabPat
// discipline (block comments only) for the remaining inline body.
```
KEEP the newlineTabPat doc (`:918-929`) verbatim.

(c) **Delete** the inline origins literal (`:933`) and the inline `postToParent` block (`:934-946`). After this, `let myChart = %MY_ECHARTS%;` (`:932`) is immediately followed by `if (myChart) {` (`:947`).

(d) **Rewire** the inbound origin-guard (`:988`):
```js
			window.addEventListener('message', function(e) {
				if (!window.__chartComms.isFromParent(e)) {
					return;
				}
				if (!e.data || typeof e.data !== 'object') {
					return;
				}
```

(e) **Rewire** the PNG branch (`:994-1009`) — replace the whole `if (e.data.type === 'composer-request-png') { … }` block with:
```js
				if (e.data.type === 'composer-request-png') {
					window.__chartComms.handlePngRequest(myChart, e, 'composer-png-result');
					return;
				}
```

(f) **KEEP verbatim:** Bug 2 / Bug 3 / Bug F (`:948-969`), keydown-`R` + resize (`:970-977`), `composer-standardize-zoom` (`:1010-1038`), `composer-update-phase-markers` (`:1039-1068`).

- [ ] **Step 4: Rewrite the MUST-CHANGE test `TestRenderComposer_RelaxedOriginCheck`**

The old assertion checks the literal `e.source !== window.parent` (the engine's inline negated guard). That negation moved into `isFromParent` and is now expressed POSITIVELY in the `.mjs` as `e.source === window.parent`; the engine calls `!window.__chartComms.isFromParent(e)`. Replace the body (`:483-494`):

```go
func TestRenderComposer_RelaxedOriginCheck(t *testing.T) {
	in := ComposerInput{
		Subject:          "S",
		EMGDataset:       makeEMGDataset(50, "RA"),
		SelectedChannels: []string{"RA"},
		MotionData:       makeMotionData(50, "knee"),
	}
	html := renderToString(t, context.Background(), in)

	// INVERSION (ADR-0003 family): the wails-dev relaxed-origin fallback moved
	// from the engine's inline negated guard `e.source !== window.parent` into
	// the shared assets.IframeCommsJS isFromParent, expressed POSITIVELY as
	// `e.source === window.parent`; the engine now calls
	// `!window.__chartComms.isFromParent(e)`. Invariant preserved — only the
	// spelling/location of the fallback changed, not its existence.
	assert.Contains(t, html, `e.source === window.parent`,
		"isFromParent 必須保留 e.source === window.parent 退路(wails dev 動態 port)")
	assert.Contains(t, html, `window.__chartComms.isFromParent`,
		"composer customJS 必須透過共用 isFromParent 做 origin 驗證")
	// Pin the migration: the OLD inline negated guard must be GONE — a partial
	// refactor (inject IframeCommsJS but forget to rewire the guard) would leave
	// both literals and false-pass. (codex review R2 finding.)
	assert.NotContains(t, html, `e.source !== window.parent`,
		"composer 不可保留舊 inline negated guard;origin 驗證必須走 window.__chartComms.isFromParent")
}
```

- [ ] **Step 5: Run Composer tests to verify they pass**

Run: `go test ./internal/chart/ -v`
Expected: PASS — including `ChartCommsInjected` and the rewritten `RelaxedOriginCheck`, and the SURVIVE-unchanged set:
- `TestRenderComposer_CustomJSSyntaxValid` — 3 URLs + `composer-request-png` (engine match) + `composer-png-result` (the full reply-type arg) + `getDataURL` (in `IframeCommsJS`) + brace balance all still present.
- `HasComposerPhaseMarkersListener`, `BodyMarginReset`, `DataZoomSliderPosition`, `ToolboxDataZoomXOnly`, `PhaseMarkLines`.

- [ ] **Step 6: Commit**

```bash
git add internal/chart/composer.go internal/chart/composer_test.go
git commit -m "refactor(composer): use shared __chartComms preamble; drop inline comms dup

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: New ADR — iframe-comms-preamble sibling of ADR-0003

**Files:**
- Create: `docs/adr/0021-chart-iframe-comms-preamble.md`

- [ ] **Step 1: Fix the ADR number**

Run: `ls docs/adr/` (include untracked — `git status --porcelain docs/adr/`). **As of this plan's review, `0019-subject-output-name-primitive.md` and `0020-normalized-emg-write-via-csvhandler.md` already exist as UNTRACKED files** — parallel arch-review sessions landing other candidates, exactly the `feedback_adr_number_collision` / `feedback_scan_parallel_forks_before_finish` race (ADR-0010's Process note documents catching this before). So the next free number is **`0021`** — but **re-run the check at write time**, since more parallel ADRs may have landed; pick the lowest unused number ≥ 0021. Read `docs/adr/0010-*.md` for the structural template and `docs/adr/0003-*.md` for the parent framing.

- [ ] **Step 2: Write the ADR**

Self-frame as the **iframe-comms-preamble sibling of ADR-0003** (parallel to how ADR-0010 calls itself "the downsampling sibling of ADR-0003"). Sections (per `improve-codebase-architecture`'s `grill-with-docs/ADR-FORMAT.md`):

- **Context:** the duplicated comms bootstrap across both engines; the existing `PhaseMarkersJS` shared-seam precedent; ADR-0003 §Reversibility's anticipated "third layer" moment.
- **Decision:** Level-2 share — pure helpers on `window.__chartComms` via a byte-identical block-comment-only `//go:embed` `.mjs`; engines keep their own message listener + dispatch (control flow stays engine→shared). Full reply-type passed as a runtime arg.
- **Considered Options (record all three branches):**
  1. **Depth** Level 1 (primitives only) / **Level 2 (chosen — max depth that respects no-third-layer)** / Level 3 (shared dispatcher — rejected: inverts control to shared→engine, the forbidden third layer).
  2. **Mechanism** — pure helpers ⟹ byte-identical `.mjs`; this kills the `%PREFIX%` token (no substitution site) and a Go builder (abandons the `.mjs` precedent). Both rejected.
  3. **Call convention** — reject prefix-compose; pass the full reply-type (`-request-png`/`-png-result` are asymmetric; full literal keeps the source-string tests green).
- **`## Related`:** `0003` (§6 symmetric phase-marker protocol stays; §Reversibility — this resolves the third-layer moment WITHOUT a third layer), `0009` (the iframe `handlePngRequest` is the JS producer-half of the PNG round-trip feeding 0009's Go consumer-half `downloadValidatedPNG` — split along the postMessage boundary), `0008` (keeps CCI + Composer as the two live engines; *this* ADR is what limits the new sharing to the iframe JS preamble — 0008 itself says nothing about preamble-sharing), deletion-test family (`0005`/`0006`/`0008`/`0010`).
- **Do NOT amend ADR-0003/0008/0009. CONTEXT.md: ZERO changes.**

- [ ] **Step 3: Verify + commit**

Run: `ls docs/adr/ | grep -c '^0021-'` → expect `1` (number unique; adjust if the write-time number shifted). Confirm no edits to `0003`/`0008`/`0009` and no `CONTEXT.md` diff:
```bash
git status --porcelain docs/adr/0003* docs/adr/0008* docs/adr/0009* CONTEXT.md
```
Expected: empty.
```bash
git add docs/adr/0021-chart-iframe-comms-preamble.md
git commit -m "docs(adr): ADR-0021 chart iframe-comms preamble (sibling of 0003)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Full verify + finishing

**Files:** none (verification + integration).

- [ ] **Step 1: Full test suite**

Run: `go test ./internal/...`
Expected: PASS (all green). Then `go vet ./internal/...` — expect no unused-symbol / dead-code reports (the CCI const + JS injection + both inline `postToParent`s are gone with no dangling references).

- [ ] **Step 2: Tripwire confirmation**

The real block-comment-only tripwire is `TestIframeCommsJS_BlockCommentOnly` (Task 1) — it strips block comments + URL schemes and asserts no `//` remains. The brace-balance + URL-intact assertions in `SyntaxValid` / `CustomJSSyntaxValid` are a *secondary* check: they count braces lexically, so they catch gross syntax breakage but would NOT catch a stray `//` on their own. Confirm all three are green in the Step 1 pass.

- [ ] **Step 3: Lint**

Run: `make lint`
Expected: clean for the touched files. `make lint` noise from stale `.claude/worktrees` copies is expected (memory `make_lint_stale_worktree_noise`) — judge by issue-path ownership, not exit code. No GOOS-tagged files are touched, so no `GOOS=` cross-lint needed.

- [ ] **Step 4: GUI smoke (native webview) — the only real check of the injection**

The customJS renders INSIDE the iframe; a syntax slip blanks the chart silently. Smoke via the **native window DevTools**, not external Chrome (memory `feedback_wails_dev_browser_binding_gap`). With real NSF1:
- PNG download — both engines (exercises `handlePngRequest` + the full reply-type round-trip to 0009's Go half).
- Phase-checkbox toggle — both engines (exercises the kept phase-marker listeners through the rewired guard).
- Restore / legend — CCI (exercises the rewired `__chartComms.postToParent`).

GUI smoke has been repeatedly deferred in this repo and the user may authorize merge without it — but it is the only check of the actual injection. **State honestly in the PR whether it ran** (memory `feedback_pr_body_verification_integrity`); leave the checkbox unchecked if not run.

- [ ] **Step 5: Finishing flow → PR**

Per memory `feedback_pre_pr_finishing_flow` + `feedback_scan_parallel_forks_before_finish`:
```bash
git worktree list && git branch --contains HEAD   # scan for parallel forks of this work first
git fetch origin && git rebase origin/main
```
Then `codex-review-fix` ×2 (two rounds; findings should not overlap). This repo blocks direct push to `main` → push the feature branch and open a PR. Before push, verify `HEAD == branch` with all commits (memory `feedback_worktree_subagent_detached_head`).

---

## Verification (end-to-end summary)

| Check | Command | Expected |
|---|---|---|
| Assets unit | `go test ./internal/chart/assets/ -v` | migrated origins test + helpers test green |
| CCI | `go test ./internal/cci/ -v` | `ChartCommsInjected` + all survive-unchanged green |
| Composer | `go test ./internal/chart/ -v` | `ChartCommsInjected` + rewritten `RelaxedOriginCheck` + survive-unchanged green |
| Full | `go test ./internal/...` | all green; `go vet` clean |
| Tripwires | (within above) | `SyntaxValid` / `CustomJSSyntaxValid` brace-balance + URLs green |
| Lint | `make lint` | clean for touched paths |
| GUI smoke | native webview DevTools, real NSF1 | PNG (both) + phase toggle (both) + restore/legend (CCI) — or honestly-unchecked |
| No collateral | `git status --porcelain docs/adr/0003* docs/adr/0008* docs/adr/0009* CONTEXT.md` | empty |

---

## Self-review (run against the design spec)

- **Spec coverage:** 6 decisions → Task 1 (`.mjs` byte-identical, pure helpers, `window.__chartComms`, full reply-type literal) + Tasks 2/3 (Level-2 engine-keeps-listener rewire) + Task 4 (ADR sibling-of-0003, `## Related`, CONTEXT zero-change, no amend). Test punch-list → Task 1 (migrate `IsValidJSONArray`), Tasks 2/3 (add both `ChartCommsInjected`, rewrite `RelaxedOriginCheck`, survive-unchanged list enumerated). ✅
- **Placeholder scan:** the ADR number is concretely set to `0021` (Task 4 Step 1 — 0019/0020 are already taken by parallel sessions; re-verify at write time per repo norm). Code/test comments reference the concrete `ADR-0003` family to avoid dangling refs. ✅
- **Block-comment-only is directly guarded:** `TestIframeCommsJS_BlockCommentOnly` (Task 1) asserts the `.mjs` source carries no `//` line comment — the brace-balance tests count braces lexically and would miss a stray `//` (codex review R1 finding). ✅
- **Type/symbol consistency:** `IframeCommsJS` (exported, package `assets`) referenced identically in Task 1 test, Task 1 `assets.go`, and via `assets.IframeCommsJS` concat in Tasks 2/3. `window.__chartComms.{postToParent,isFromParent,handlePngRequest}` spelled identically in `.mjs`, both engine rewires, and all three injection tests. Full reply-types `'cci-png-result'` / `'composer-png-result'` consistent. ✅
- **Compile-dependency caught:** the CCI Go-const deletion and `TestWailsParentOrigins_IsValidJSONArray` removal are co-located in Task 2 (same commit) — a separate-task split would leave `cci` uncompilable. ✅
- **Scope guard:** keydown-`R` / `resize` explicitly marked KEEP-inline / out-of-scope (Invariant 5) so the executor neither moves them nor flags them as incomplete. ✅

---

## Process notes for the execute session

- **This plan is the deliverable of a plan-only session.** Execution is a SEPARATE fresh-worktree session (user pattern, memory `feedback_handoff_after_design`) — do not continue inline from the planning agent.
- **TDD is natural here:** the source-string tests ARE the spec. Each task bakes red→green.
- **Tasks 2 and 3 parallelize** as two subagents both depending on Task 1 (the repo norm is subagent-driven multi-task + two-stage review).
- On execution, copy this file to `docs/superpowers/plans/2026-06-03-chart-comms-preamble.md`.
