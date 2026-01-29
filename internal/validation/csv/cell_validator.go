package csv

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"count_mean/internal/errors"
	"count_mean/internal/validation/patterns"
)

// CellContext provides context information for cell validation.
type CellContext struct {
	Row      int
	Col      int
	Filename string
}

// NewCellContext creates a new CellContext.
func NewCellContext(row, col int, filename string) *CellContext {
	return &CellContext{
		Row:      row,
		Col:      col,
		Filename: filename,
	}
}

// CellValidator provides CSV cell validation functionality.
type CellValidator struct {
	detector *patterns.InjectionDetectorImpl
}

// NewCellValidator creates a new cell validator.
func NewCellValidator() *CellValidator {
	return &CellValidator{
		detector: patterns.NewInjectionDetector(),
	}
}

// ValidateCell validates a single CSV cell for malicious content.
func (v *CellValidator) ValidateCell(cell string, ctx *CellContext) error {
	// Check for excessive cell content (potential DoS attack)
	if err := checkCellLength(cell, ctx); err != nil {
		return err
	}

	// Check for non-UTF8 content
	if err := checkUTF8(cell, ctx); err != nil {
		return err
	}

	// Check for formula injection
	if err := checkFormulaInjection(cell, ctx); err != nil {
		return err
	}

	// Check for dangerous functions
	if err := v.checkDangerousFunctions(cell, ctx); err != nil {
		return err
	}

	// Check for script injection
	if err := v.checkScriptInjection(cell, ctx); err != nil {
		return err
	}

	// Check for SQL injection
	if err := v.checkSQLInjection(cell, ctx); err != nil {
		return err
	}

	// Check for command injection
	if err := v.checkCommandInjection(cell, ctx); err != nil {
		return err
	}

	// Check for control characters
	if err := checkControlChars(cell, ctx); err != nil {
		return err
	}

	// Check for suspicious file extensions
	return v.checkSuspiciousExtensions(cell, ctx)
}

func checkCellLength(cell string, ctx *CellContext) error {
	// 32KB per cell limit to prevent DoS attacks
	if len(cell) > 32768 { //nolint:mnd // 32768 is 32KB, standard cell size limit
		return errors.NewValidationError("csv_cell",
			map[string]interface{}{
				"row":      ctx.Row,
				"col":      ctx.Col,
				"filename": ctx.Filename,
				"length":   len(cell),
			},
			fmt.Sprintf("第 %d 行第 %d 欄的內容過長 (最大 32KB)", ctx.Row, ctx.Col))
	}

	return nil
}

func checkUTF8(cell string, ctx *CellContext) error {
	if !utf8.ValidString(cell) {
		return errors.NewValidationError("csv_cell",
			map[string]interface{}{
				"row":      ctx.Row,
				"col":      ctx.Col,
				"filename": ctx.Filename,
			},
			fmt.Sprintf("第 %d 行第 %d 欄包含非 UTF-8 字符", ctx.Row, ctx.Col))
	}

	return nil
}

func checkFormulaInjection(cell string, ctx *CellContext) error {
	// Check direct formula starters
	formulaStarters := []string{"=", "@", "\t=", "\r=", "\n="}
	for _, starter := range formulaStarters {
		if strings.HasPrefix(cell, starter) {
			return errors.NewValidationError("csv_cell",
				map[string]interface{}{
					"row":      ctx.Row,
					"col":      ctx.Col,
					"filename": ctx.Filename,
					"content":  cell,
				},
				fmt.Sprintf("第 %d 行第 %d 欄疑似包含 CSV 公式注入攻擊", ctx.Row, ctx.Col))
		}
	}

	// Check for + or - prefixed content that is not a valid number
	if strings.HasPrefix(cell, "+") || strings.HasPrefix(cell, "-") {
		if _, err := strconv.ParseFloat(cell, 64); err != nil {
			if strings.ContainsAny(cell, "=@|!") {
				return errors.NewValidationError("csv_cell",
					map[string]interface{}{
						"row":      ctx.Row,
						"col":      ctx.Col,
						"filename": ctx.Filename,
						"content":  cell,
					},
					fmt.Sprintf("第 %d 行第 %d 欄疑似包含 CSV 公式注入攻擊", ctx.Row, ctx.Col))
			}
		}
	}

	return nil
}

func (v *CellValidator) checkDangerousFunctions(cell string, ctx *CellContext) error {
	if detected, fn := v.detector.DetectFormula(cell); detected {
		// Only report if it's a dangerous function (not a formula starter)
		if !strings.HasPrefix(cell, fn) {
			return errors.NewValidationError("csv_cell",
				map[string]interface{}{
					"row":      ctx.Row,
					"col":      ctx.Col,
					"filename": ctx.Filename,
					"function": fn,
				},
				fmt.Sprintf("第 %d 行第 %d 欄包含危險函數: %s", ctx.Row, ctx.Col, fn))
		}
	}

	return nil
}

func (v *CellValidator) checkScriptInjection(cell string, ctx *CellContext) error {
	if detected, pattern := v.detector.DetectScript(cell); detected {
		return errors.NewValidationError("csv_cell",
			map[string]interface{}{
				"row":      ctx.Row,
				"col":      ctx.Col,
				"filename": ctx.Filename,
				"pattern":  pattern,
			},
			fmt.Sprintf("第 %d 行第 %d 欄包含腳本注入模式: %s", ctx.Row, ctx.Col, pattern))
	}

	return nil
}

func (v *CellValidator) checkSQLInjection(cell string, ctx *CellContext) error {
	if detected, pattern := v.detector.DetectSQL(cell); detected {
		return errors.NewValidationError("csv_cell",
			map[string]interface{}{
				"row":      ctx.Row,
				"col":      ctx.Col,
				"filename": ctx.Filename,
				"pattern":  pattern,
			},
			fmt.Sprintf("第 %d 行第 %d 欄包含 SQL 注入模式: %s", ctx.Row, ctx.Col, pattern))
	}

	return nil
}

func (v *CellValidator) checkCommandInjection(cell string, ctx *CellContext) error {
	if detected, pattern := v.detector.DetectCommand(cell); detected {
		return errors.NewValidationError("csv_cell",
			map[string]interface{}{
				"row":      ctx.Row,
				"col":      ctx.Col,
				"filename": ctx.Filename,
				"pattern":  pattern,
			},
			fmt.Sprintf("第 %d 行第 %d 欄包含命令注入模式: %s", ctx.Row, ctx.Col, pattern))
	}

	return nil
}

func checkControlChars(cell string, ctx *CellContext) error {
	for _, r := range cell {
		if r == 0 {
			return errors.NewValidationError("csv_cell",
				map[string]interface{}{
					"row":      ctx.Row,
					"col":      ctx.Col,
					"filename": ctx.Filename,
				},
				fmt.Sprintf("第 %d 行第 %d 欄包含空字符", ctx.Row, ctx.Col))
		}

		if unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r' {
			return errors.NewValidationError("csv_cell",
				map[string]interface{}{
					"row":      ctx.Row,
					"col":      ctx.Col,
					"filename": ctx.Filename,
					"char":     fmt.Sprintf("U+%04X", r),
				},
				fmt.Sprintf("第 %d 行第 %d 欄包含控制字符: U+%04X", ctx.Row, ctx.Col, r))
		}
	}

	return nil
}

func (v *CellValidator) checkSuspiciousExtensions(cell string, ctx *CellContext) error {
	if detected, ext := v.detector.DetectSuspiciousExtension(cell); detected {
		return errors.NewValidationError("csv_cell",
			map[string]interface{}{
				"row":       ctx.Row,
				"col":       ctx.Col,
				"filename":  ctx.Filename,
				"extension": ext,
			},
			fmt.Sprintf("第 %d 行第 %d 欄包含可疑的檔案副檔名: %s", ctx.Row, ctx.Col, ext))
	}

	return nil
}
