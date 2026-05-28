# Share the LTTB union downsampling kernel between CCI and Chart Composer

**Status**: accepted (2026-05-29)

## Decision

`internal/chart` will own the shared **indices-only** LTTB union kernel for chart downsampling. The kernel will live next to `LTTBDownsample` in `internal/chart/downsampling.go`, likely as `UnionLTTBIndices`, and will own the shared math:

1. run `LTTBDownsample` per valid series,
2. union retained indices,
3. sort indices,
4. cap the union with the existing ceiling-division stride decimation at `threshold * 2`, preserving endpoints.

The kernel returns only `[]int`. CCI and Chart Composer remain adapters around that kernel: CCI validates every pair against `TimeValues`, preserves its fail-fast `ErrPairLengthMismatch` policy, and rebuilds `*CCIAnalysisResult`; Chart Composer pre-filters mismatched series, preserves its graceful skip policy, and rebuilds `map[string][]float64`.

`CONTEXT.md` does not get a new term. `UnionLTTBIndices` is chart implementation language, not EMG analysis domain language.

## Why

`chart.LTTBDownsample(xs, ys, threshold)` is already the shared per-series primitive, but both live chart paths still hand-mirror the wrapper around it. CCI has `downsampleCCIResult -> unionLTTBIndices -> capUnionIndices`; Chart Composer has `downsampleSeriesMap -> capComposerUnionIndices`. The duplicated part is not container reshaping, but the index math: per-series LTTB, union, sort, and the 2x stride cap.

The strongest locality failure is the twin cap functions. The ceiling-division stride fix exists because floor division can exceed the cap, e.g. `len=29999, limit=10000` with `stride=2` produces roughly 15k points. Today that bug fix has to stay mirrored in both `capUnionIndices` and `capComposerUnionIndices`; `composer.go` even documents that it is aligned with the CCI ceiling-division logic. A shared kernel makes that rule one piece of code and one test suite.

This is the downsampling sibling of [[ADR-0003]]: that ADR shared the CCI/Composer iframe transport protocol while keeping adapter-specific chart details out of the bridge. This ADR shares only the downsampling index kernel while keeping adapter-specific error policy and container shape out of the kernel.

## Considered Options

- **Indices-only kernel with internal 2x cap (chosen)**: concentrates the duplicated, bug-prone index math while preserving honest adapter differences. The kernel takes `threshold` and applies `threshold * 2` internally because both current adapters use that policy, and exposing a separate cap parameter would keep an unused knob alive.
- **Kernel returns reshaped data**: rejected. It would couple shared chart math to either `*CCIAnalysisResult` or `map[string][]float64`, or invent a third container abstraction just to cross the seam. The container rebuilds are real adapter differences.
- **Kernel does union+sort but callers cap**: rejected. The cap is the exact drift surface that already required a mirrored bug fix, so leaving it in callers would preserve the most important duplication.
- **Unify mismatch error policy**: rejected. CCI's invariant is fail-fast with `ErrPairLengthMismatch`; Composer's chart-viewer behavior is graceful skip. Those policies are not the shared kernel's responsibility.
- **Add a glossary term**: rejected. Downsampling kernel naming belongs in implementation and ADRs, not in `CONTEXT.md`.

## Test Migration

Move the cap regression coverage from the CCI adapter tests into `internal/chart/downsampling_test.go`, including the ceiling-division reproducer (`29999_cap_10000_codex_repro`) and endpoint preservation. Add kernel tests that prove union behavior preserves peaks from a non-representative series, returns sorted shared indices, caps to `threshold*2 + 1`, and preserves first/last index.

Adapter tests stay adapter-shaped. CCI keeps tests for `ErrPairLengthMismatch` fail-fast and `*CCIAnalysisResult` reshaping. Chart Composer should gain a direct graceful-mismatch-skip test; cross-check found that the policy exists in `downsampleSeriesMap`, but current Composer tests pin downsampling mostly through render behavior and do not directly assert the mismatch skip contract.

## Reversibility

Medium. The change is internal and lives in `internal/chart`, which CCI already imports, so there is no dependency cycle and no public API commitment. Reverting would be mechanically simple, but it would intentionally recreate the duplicated cap logic and the same two-place bug-fix surface.

## Related

- [[ADR-0003]] — same CCI/Composer adapter pair, but for iframe transport rather than downsampling.
- [[ADR-0005]], [[ADR-0006]], and [[ADR-0008]] — same deletion-test family. This decision is deliberately partial: shared index math concentrates, while container rebuilds and mismatch policies stay in adapters.

## Process note

2026-05-29 grill opened by re-checking the architecture report against code. The verified picture was:

1. `internal/chart/downsampling.go` already exposes `LTTBDownsample`, and both CCI and Composer call it.
2. `internal/cci/chart.go` duplicates the wrapper through `unionLTTBIndices` and `capUnionIndices`.
3. `internal/chart/composer.go` duplicates the same wrapper shape through `downsampleSeriesMap` and `capComposerUnionIndices`, with comments explicitly aligning it to CCI.
4. `internal/cci` already imports `internal/chart`; `internal/chart` does not import `internal/cci`, so the shared kernel naturally belongs in `internal/chart`.
5. Both current thresholds are `5000`, but the kernel still takes `threshold` so the adapters can diverge later without changing the kernel contract.
6. `docs/adr/0009-png-download-safety-pipeline-seam.md` already exists in this worktree, so this ADR uses `0010`.

Implementation is intentionally a separate step after this design decision.
