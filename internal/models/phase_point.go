package models

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ErrInvalidPhasePoint 表示 ParsePhasePoint 收到不在 10 個合法值內的字串。
var ErrInvalidPhasePoint = errors.New("invalid phase point")

// PhasePoint identifies one of the 10 canonical phase points in jump-biomechanics
// analysis. The string value is the stable wire identifier shared with the
// frontend and persisted JSON; never change the literal values.
//
// 注意：PhasePoint 是 defined string type，直接 cast `PhasePoint("invalid")` 不會
// 觸發 IsValid 檢查。要從外部字串（前端傳入、CSV 欄位、設定檔）安全地建構，請改用
// ParsePhasePoint constructor — 它會在 cast 同時驗證並回傳 error。直接 cast 應僅用於
// 已知 compile-time 合法的常數來源（如 phase_manifest 內部固定字串）。
type PhasePoint string

// Phase point constants. Order reflects temporal progression P0 → ... → L.
const (
	PhaseP0 PhasePoint = "P0"
	PhaseP1 PhasePoint = "P1"
	PhaseP2 PhasePoint = "P2"
	PhaseS  PhasePoint = "S"
	PhaseC  PhasePoint = "C"
	PhaseD  PhasePoint = "D"
	PhaseT0 PhasePoint = "T0"
	PhaseT  PhasePoint = "T"
	PhaseO  PhasePoint = "O"
	PhaseL  PhasePoint = "L"
)

// allPhasesOrdered is the canonical temporal ordering; index == Order() value.
//
//nolint:gochecknoglobals // canonical ordering, treated as immutable
var allPhasesOrdered = []PhasePoint{
	PhaseP0, PhaseP1, PhaseP2, PhaseS, PhaseC, PhaseD, PhaseT0, PhaseT, PhaseO, PhaseL,
}

// AllPhases returns a fresh slice with the 10 canonical phase points in order.
// The returned slice is a defensive copy; mutating it does not affect future calls.
func AllPhases() []PhasePoint {
	phases := make([]PhasePoint, len(allPhasesOrdered))
	copy(phases, allPhasesOrdered)

	return phases
}

// IsValid reports whether p is one of the 10 canonical phase points.
func (p PhasePoint) IsValid() bool {
	for _, valid := range allPhasesOrdered {
		if p == valid {
			return true
		}
	}

	return false
}

// IsMotionIndex reports whether p represents a motion-capture index value
// (rather than a force-plate time). D 與 O 為 motion index,其餘 8 點為力板時間。
// 此 domain invariant 過去散在 phase_manifest_parser.go、cci/analyzer.go、
// phase_calculator.go 三處,集中於此避免遺漏一處導致 silent bug。
func (p PhasePoint) IsMotionIndex() bool {
	return p == PhaseD || p == PhaseO
}

// Order returns the 0-based temporal order index of p (P0 = 0 ... L = 9).
// Returns -1 if p is not a canonical phase point.
func (p PhasePoint) Order() int {
	for i, valid := range allPhasesOrdered {
		if p == valid {
			return i
		}
	}

	return -1
}

// ParsePhasePoint 從外部字串建構 PhasePoint 並驗證 — 是所有「字串轉 PhasePoint」
// 路徑應該走的入口（前端傳入、CSV/JSON 解析、manifest 欄位等）。不合法的字串
// 回傳 error 而非靜默回傳零值，避免「PhasePoint(任意字串)」cast 繞過 type 安全。
//
// cross-compare review P2：先前外部字串會直接 cast PhasePoint(raw) 後使用，
// 觸發 IsValid() 才會發現問題；改成這個 constructor 後，誤用會在邊界立即被 caller
// 看見而非延後到 IsValid call site 才暴露。
func ParsePhasePoint(s string) (PhasePoint, error) {
	p := PhasePoint(s)
	if !p.IsValid() {
		return "", fmt.Errorf("%w %q: must be one of P0/P1/P2/S/C/D/T0/T/O/L", ErrInvalidPhasePoint, s)
	}
	return p, nil
}

// UnmarshalJSON 把 JSON 字串 unmarshal 成 PhasePoint,delegate 到 ParsePhasePoint
// 做驗證 — 不合法的字串會回 error 並讓整個 struct unmarshal 失敗,而非靜默通過。
//
// Wave 6 PR4 (cross-compare review PR4 finding P0 收尾): 過去 PhasePoint 沒有
// 自訂 UnmarshalJSON, Go 預設行為對 `type PhasePoint string` 是「直接 cast 任何
// 字串到 PhasePoint」 — 前端傳「Z9」/「p0」會被 unmarshal 成 PhasePoint("Z9")
// 而非 error,僅靠下游 (gui/app.go AnalyzePhaseSync) 的 IsValid() 才被攔截。
// 加上 UnmarshalJSON delegate 到 ParsePhasePoint 後:
//  1. 錯誤發生位置從 RPC 邏輯深處提前到 JSON 解析邊界(fail-fast)
//  2. ParsePhasePoint 從「裝飾品(0 production caller)」變成真有 caller — 不會
//     被未來重構誤刪
//  3. 任何新增的 `models.PhasePoint` 欄位自動受保護,不需 caller 各自呼叫 IsValid()
func (p *PhasePoint) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("phase point JSON unmarshal: %w", err)
	}
	pp, err := ParsePhasePoint(s)
	if err != nil {
		return err
	}
	*p = pp
	return nil
}
