package calculator

import (
	"testing"

	"count_mean/internal/models"

	"github.com/stretchr/testify/assert"
)

// TestGenerateOutputFileName_LiteralCharacterization 逐字釘住自動檔名,作為
// ADR-0019 把本函數改呼 filename.SubjectOutputName 前的 byte-identity gate。
// 不可用 GenerateOutputFileName 自身算 expected(套套邏輯,bug 會兩邊同變)。
func TestGenerateOutputFileName_LiteralCharacterization(t *testing.T) {
	// PhasePoint 是 type PhasePoint string(PhaseP0="P0"、PhaseL="L"、PhaseS="S"、PhaseT="T")。
	assert.Equal(t,
		"subject_01_P0-L_statistics.csv",
		GenerateOutputFileName("subject_01", models.PhaseP0, models.PhaseL),
	)
	// subject 帶分隔符 → Sanitize 收斂單一 segment(安全不變量逐字)。
	assert.Equal(t,
		"a_b_S-T_statistics.csv",
		GenerateOutputFileName("a/b", models.PhaseS, models.PhaseT),
	)
}
