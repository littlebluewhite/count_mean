// Package assets exposes embedded JavaScript assets that are inlined into
// the chart iframe customJS at HTML render time (see internal/chart and
// internal/cci callers).
//
// PhaseMarkersJS is the iframe-side canonical phase-marker helper module
// (ADR-0003). Its algorithm must stay byte-identical (whitespace-normalized)
// with frontend/src/charts/phaseMarkers.mjs — the frontend test in
// frontend/src/phaseMarkers.test.mjs enforces the sync. The IIFE form
// attaches the helpers to window.__phaseMarkers so that the surrounding
// chart customJS body (newline-stripped by go-echarts opts/js.go) can call
// them without polluting the iframe global scope.
package assets

import _ "embed"

//go:embed phasemarkers.mjs
var PhaseMarkersJS string
