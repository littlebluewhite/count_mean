package validation

import (
	"count_mean/internal/errors"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// InputValidator provides comprehensive input validation functionality
type InputValidator struct {
	// Configuration constraints
	maxFileSize       int64
	allowedExtensions []string
	maxWindowSize     int
	maxPrecision      int
}

// NewInputValidator creates a new input validator with default constraints
func NewInputValidator() *InputValidator {
	return &InputValidator{
		maxFileSize:       100 * 1024 * 1024, // 100MB
		allowedExtensions: []string{".csv"},
		maxWindowSize:     10000,
		maxPrecision:      15,
	}
}

// WithMaxFileSize sets the maximum allowed file size
func (v *InputValidator) WithMaxFileSize(size int64) *InputValidator {
	v.maxFileSize = size
	return v
}

// WithAllowedExtensions sets the allowed file extensions
func (v *InputValidator) WithAllowedExtensions(extensions []string) *InputValidator {
	v.allowedExtensions = extensions
	return v
}

// NumericRange defines validation ranges for different numeric types
type NumericRange struct {
	MinInt64   int64
	MaxInt64   int64
	MinFloat64 float64
	MaxFloat64 float64
}

// GetSafeNumericRanges returns predefined safe ranges to prevent overflow attacks
func GetSafeNumericRanges() *NumericRange {
	return &NumericRange{
		MinInt64:   -9223372036854775807,     // Safe int64 min (leaving room for calculations)
		MaxInt64:   9223372036854775806,      // Safe int64 max (leaving room for calculations)
		MinFloat64: -1.7976931348623157e+307, // Safe float64 min
		MaxFloat64: 1.7976931348623157e+307,  // Safe float64 max
	}
}

// ValidateInteger validates integer input with overflow protection
func (v *InputValidator) ValidateInteger(value string, fieldName string, minValue, maxValue int64) (int64, error) {
	if value == "" {
		return 0, errors.NewValidationError(fieldName, value,
			fmt.Sprintf("%s 不能為空", fieldName))
	}

	// Clean input
	value = strings.TrimSpace(value)

	// Check for malicious patterns
	maliciousPatterns := []string{
		"0x", "0X", "0b", "0B", "0o", "0O", // Different base encodings
		"e+", "E+", "e-", "E-", // Scientific notation (suspicious in integers)
		"++", "--", "+-", "-+", // Multiple signs
		"Infinity", "infinity", "INFINITY", "inf", "INF",
		"NaN", "nan", "NAN",
	}

	for _, pattern := range maliciousPatterns {
		if strings.Contains(strings.ToLower(value), strings.ToLower(pattern)) {
			return 0, errors.NewValidationError(fieldName, value,
				fmt.Sprintf("%s 包含可疑的數值模式: %s", fieldName, pattern))
		}
	}

	// Check for excessive length (potential DoS attack)
	if len(value) > 20 {
		return 0, errors.NewValidationError(fieldName, value,
			fmt.Sprintf("%s 數值過長 (最大 20 字符)", fieldName))
	}

	// Parse integer
	intValue, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, errors.NewValidationError(fieldName, value,
			fmt.Sprintf("%s 必須是有效的整數", fieldName))
	}

	// Check bounds
	if intValue < minValue || intValue > maxValue {
		return 0, errors.NewValidationError(fieldName, intValue,
			fmt.Sprintf("%s 必須在 %d 到 %d 範圍內", fieldName, minValue, maxValue))
	}

	return intValue, nil
}

// ValidateFloat validates float input with overflow protection
func (v *InputValidator) ValidateFloat(value string, fieldName string, minValue, maxValue float64) (float64, error) {
	if value == "" {
		return 0, errors.NewValidationError(fieldName, value,
			fmt.Sprintf("%s 不能為空", fieldName))
	}

	// Clean input
	value = strings.TrimSpace(value)

	// Check for malicious patterns
	maliciousPatterns := []string{
		"0x", "0X", "0b", "0B", "0o", "0O", // Different base encodings
		"++", "--", "+-", "-+", // Multiple signs
		"ee", "EE", "e+e", "E+E", "e-e", "E-E", // Invalid scientific notation
		"...", "..", // Multiple decimal points
		"Infinity", "infinity", "INFINITY", "+Infinity", "-Infinity",
		"NaN", "nan", "NAN", "+NaN", "-NaN",
	}

	for _, pattern := range maliciousPatterns {
		if strings.Contains(strings.ToLower(value), strings.ToLower(pattern)) {
			return 0, errors.NewValidationError(fieldName, value,
				fmt.Sprintf("%s 包含可疑的數值模式: %s", fieldName, pattern))
		}
	}

	// Check for excessive length
	if len(value) > 50 {
		return 0, errors.NewValidationError(fieldName, value,
			fmt.Sprintf("%s 數值過長 (最大 50 字符)", fieldName))
	}

	// Count decimal points
	decimalCount := strings.Count(value, ".")
	if decimalCount > 1 {
		return 0, errors.NewValidationError(fieldName, value,
			fmt.Sprintf("%s 包含多個小數點", fieldName))
	}

	// Parse float
	floatValue, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, errors.NewValidationError(fieldName, value,
			fmt.Sprintf("%s 必須是有效的浮點數", fieldName))
	}

	// Check for infinity and NaN
	if math.IsInf(floatValue, 0) {
		return 0, errors.NewValidationError(fieldName, value,
			fmt.Sprintf("%s 不能是無窮大", fieldName))
	}

	if math.IsNaN(floatValue) {
		return 0, errors.NewValidationError(fieldName, value,
			fmt.Sprintf("%s 不能是 NaN", fieldName))
	}

	// Check bounds
	if floatValue < minValue || floatValue > maxValue {
		return 0, errors.NewValidationError(fieldName, floatValue,
			fmt.Sprintf("%s 必須在 %f 到 %f 範圍內", fieldName, minValue, maxValue))
	}

	return floatValue, nil
}

// ValidatePositiveInteger validates positive integer with safe bounds
func (v *InputValidator) ValidatePositiveInteger(value string, fieldName string, maxValue int64) (int64, error) {
	ranges := GetSafeNumericRanges()
	if maxValue <= 0 || maxValue > ranges.MaxInt64 {
		maxValue = ranges.MaxInt64
	}

	result, err := v.ValidateInteger(value, fieldName, 1, maxValue)
	if err != nil {
		return 0, err
	}

	if result <= 0 {
		return 0, errors.NewValidationError(fieldName, result,
			fmt.Sprintf("%s 必須是正整數", fieldName))
	}

	return result, nil
}

// ValidatePositiveFloat validates positive float with safe bounds
func (v *InputValidator) ValidatePositiveFloat(value string, fieldName string, maxValue float64) (float64, error) {
	ranges := GetSafeNumericRanges()
	if maxValue <= 0 || maxValue > ranges.MaxFloat64 {
		maxValue = ranges.MaxFloat64
	}

	result, err := v.ValidateFloat(value, fieldName, 0.0, maxValue)
	if err != nil {
		return 0, err
	}

	if result <= 0 {
		return 0, errors.NewValidationError(fieldName, result,
			fmt.Sprintf("%s 必須是正數", fieldName))
	}

	return result, nil
}

// ValidateFilename validates a filename for safety and correctness
func (v *InputValidator) ValidateFilename(filename string) error {
	if filename == "" {
		return errors.NewValidationError("filename", filename, "檔案名稱不能為空")
	}

	// Remove leading/trailing whitespace
	filename = strings.TrimSpace(filename)

	// Check for null bytes and control characters
	for _, r := range filename {
		if r == 0 || (unicode.IsControl(r) && r != '\t') {
			return errors.NewValidationError("filename", filename, "檔案名稱包含非法字符")
		}
	}

	// Check for dangerous characters - Enhanced security check
	dangerousChars := []string{
		"<", ">", ":", "\"", "|", "?", "*", "\x00",
		// Command injection prevention
		";", "&", "&&", "||", "`", "$", "(", ")", "[", "]", "{", "}",
		// SQL injection prevention
		"'", "--", "/*", "*/", "@@", "xp_", "sp_",
		// Script injection prevention
		"<script", "</script", "javascript:", "vbscript:", "onload=", "onerror=",
		// Path traversal prevention (additional patterns)
		"../", "..\\", ".../", "...\\", "....//", "....\\\\",
		// URL encoded dangerous chars
		"%2e%2e", "%2f", "%5c", "%00", "%0a", "%0d",
		// Unicode variants
		"\u002e\u002e", "\u002f", "\u005c", "\u0000",
		// Other control characters
		"\x01", "\x02", "\x03", "\x04", "\x05", "\x06", "\x07", "\x08", "\x0b", "\x0c", "\x0e", "\x0f",
		"\x10", "\x11", "\x12", "\x13", "\x14", "\x15", "\x16", "\x17", "\x18", "\x19", "\x1a", "\x1b", "\x1c", "\x1d", "\x1e", "\x1f",
	}

	// Convert to lowercase for case-insensitive checking
	filenameLower := strings.ToLower(filename)

	for _, char := range dangerousChars {
		if strings.Contains(filenameLower, strings.ToLower(char)) {
			return errors.NewValidationError("filename", filename,
				fmt.Sprintf("檔案名稱包含非法字符: %s", char))
		}
	}

	// Check for reserved names (Windows)
	reservedNames := []string{
		"CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9",
	}

	baseName := strings.ToUpper(strings.TrimSuffix(filename, filepath.Ext(filename)))
	for _, reserved := range reservedNames {
		if baseName == reserved {
			return errors.NewValidationError("filename", filename,
				fmt.Sprintf("檔案名稱不能使用保留字: %s", reserved))
		}
	}

	// Check length
	if len(filename) > 255 {
		return errors.NewValidationError("filename", filename, "檔案名稱過長 (最大 255 字符)")
	}

	// Check extension
	ext := strings.ToLower(filepath.Ext(filename))
	if ext != "" {
		validExt := false
		for _, allowedExt := range v.allowedExtensions {
			if ext == allowedExt {
				validExt = true
				break
			}
		}
		if !validExt {
			return errors.NewValidationError("filename", filename,
				fmt.Sprintf("不支援的檔案副檔名: %s", ext))
		}
	}

	return nil
}

// ValidateWindowSize validates the window size parameter
func (v *InputValidator) ValidateWindowSize(windowSizeStr string) (int, error) {
	// Use secure validation method
	windowSize64, err := v.ValidatePositiveInteger(windowSizeStr, "window_size", int64(v.maxWindowSize))
	if err != nil {
		return 0, err
	}

	return int(windowSize64), nil
}

// ValidateTimeRange validates time range parameters
func (v *InputValidator) ValidateTimeRange(startRangeStr, endRangeStr string) (float64, float64, bool, error) {
	var startRange, endRange float64
	var useCustomRange bool
	var err error

	// Validate start range using secure method
	if startRangeStr != "" {
		startRange, err = v.ValidatePositiveFloat(startRangeStr, "start_range", 1e10) // 10 billion max
		if err != nil {
			return 0, 0, false, err
		}
		useCustomRange = true
	}

	// Validate end range using secure method
	if endRangeStr != "" {
		endRange, err = v.ValidatePositiveFloat(endRangeStr, "end_range", 1e10) // 10 billion max
		if err != nil {
			return 0, 0, false, err
		}
		useCustomRange = true
	}

	// Validate range consistency
	if useCustomRange && startRangeStr != "" && endRangeStr != "" {
		if startRange >= endRange {
			return 0, 0, false, errors.NewValidationError("time_range",
				map[string]float64{"start": startRange, "end": endRange},
				"開始範圍必須小於結束範圍")
		}
	}

	return startRange, endRange, useCustomRange, nil
}

// ValidateScalingFactor validates the scaling factor parameter
func (v *InputValidator) ValidateScalingFactor(scalingFactorStr string) (int, error) {
	// Use secure validation method
	scalingFactor64, err := v.ValidatePositiveInteger(scalingFactorStr, "scaling_factor", 20)
	if err != nil {
		return 0, err
	}

	return int(scalingFactor64), nil
}

// ValidatePrecision validates the precision parameter
func (v *InputValidator) ValidatePrecision(precisionStr string) (int, error) {
	// Use secure validation method (precision can be 0, so use ValidateInteger instead of ValidatePositiveInteger)
	precision64, err := v.ValidateInteger(precisionStr, "precision", 0, int64(v.maxPrecision))
	if err != nil {
		return 0, err
	}

	return int(precision64), nil
}

// ValidatePhaseLabels validates phase label input
func (v *InputValidator) ValidatePhaseLabels(phaseLabelsText string) ([]string, error) {
	if strings.TrimSpace(phaseLabelsText) == "" {
		return nil, errors.NewValidationError("phase_labels", phaseLabelsText,
			"階段標籤不能為空")
	}

	lines := strings.Split(phaseLabelsText, "\n")
	var cleanLabels []string

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue // Skip empty lines
		}

		// Validate individual label
		if err := v.validateSinglePhaseLabel(trimmed, i+1); err != nil {
			return nil, err
		}

		cleanLabels = append(cleanLabels, trimmed)
	}

	if len(cleanLabels) == 0 {
		return nil, errors.NewValidationError("phase_labels", phaseLabelsText,
			"至少需要一個有效的階段標籤")
	}

	if len(cleanLabels) > 50 {
		return nil, errors.NewValidationError("phase_labels", cleanLabels,
			"階段標籤數量不能超過 50 個")
	}

	return cleanLabels, nil
}

// validateSinglePhaseLabel validates a single phase label
func (v *InputValidator) validateSinglePhaseLabel(label string, lineNum int) error {
	// Check length
	if len(label) > 100 {
		return errors.NewValidationError("phase_label", label,
			fmt.Sprintf("第 %d 行的階段標籤過長 (最大 100 字符)", lineNum))
	}

	// Check for control characters
	for _, r := range label {
		if unicode.IsControl(r) && r != '\t' {
			return errors.NewValidationError("phase_label", label,
				fmt.Sprintf("第 %d 行的階段標籤包含非法字符", lineNum))
		}
	}

	return nil
}

// ValidateDirectoryPath validates directory path input
func (v *InputValidator) ValidateDirectoryPath(path string) error {
	if path == "" {
		return errors.NewValidationError("directory_path", path, "目錄路徑不能為空")
	}

	path = strings.TrimSpace(path)

	// Check for null bytes and dangerous characters
	if strings.Contains(path, "\x00") {
		return errors.NewValidationError("directory_path", path, "路徑包含非法字符")
	}

	// Check length
	if len(path) > 4096 {
		return errors.NewValidationError("directory_path", path, "路徑過長")
	}

	return nil
}

// SanitizeString removes dangerous characters from string input
func (v *InputValidator) SanitizeString(input string) string {
	// Remove null bytes
	input = strings.ReplaceAll(input, "\x00", "")

	// Remove other control characters except tab, newline, and carriage return
	var result strings.Builder
	for _, r := range input {
		if !unicode.IsControl(r) || r == '\t' || r == '\n' || r == '\r' {
			result.WriteRune(r)
		}
	}

	return result.String()
}

// ValidateOutputFormat validates output format selection
func (v *InputValidator) ValidateOutputFormat(format string) error {
	if format == "" {
		return errors.NewValidationError("output_format", format, "輸出格式不能為空")
	}

	validFormats := map[string]bool{
		"csv":  true,
		"json": true,
		"xlsx": true,
	}

	if !validFormats[strings.ToLower(format)] {
		return errors.NewValidationError("output_format", format,
			fmt.Sprintf("不支援的輸出格式: %s", format))
	}

	return nil
}

// ValidateCSVData validates CSV data structure and detects malicious content
func (v *InputValidator) ValidateCSVData(records [][]string, filename string) error {
	if len(records) == 0 {
		return errors.NewValidationError("csv_data", len(records),
			"CSV 檔案為空")
	}

	if len(records) < 2 {
		return errors.NewValidationError("csv_data", len(records),
			"CSV 檔案至少需要包含標題行和一行資料")
	}

	// Validate header row
	if len(records[0]) == 0 {
		return errors.NewValidationError("csv_data", records[0],
			"CSV 標題行為空")
	}

	expectedColumns := len(records[0])

	// Check for excessive columns (potential DoS attack)
	if expectedColumns > 1000 {
		return errors.NewValidationError("csv_data", expectedColumns,
			"CSV 欄位數量過多 (最大 1000 欄)")
	}

	// Check for excessive rows (potential DoS attack)
	if len(records) > 1000000 {
		return errors.NewValidationError("csv_data", len(records),
			"CSV 資料行數過多 (最大 1,000,000 行)")
	}

	// Validate data consistency and detect malicious content
	for i, record := range records {
		if len(record) != expectedColumns {
			return errors.NewValidationError("csv_data",
				map[string]interface{}{
					"row":           i + 1,
					"expected_cols": expectedColumns,
					"actual_cols":   len(record),
					"filename":      filename,
				},
				fmt.Sprintf("第 %d 行的欄位數量不一致", i+1))
		}

		// Check each cell for malicious content
		for j, cell := range record {
			if err := v.validateCSVCell(cell, i+1, j+1, filename); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateCSVCell validates individual CSV cell content for malicious patterns
func (v *InputValidator) validateCSVCell(cell string, row, col int, filename string) error {
	// Check for excessive cell content (potential DoS attack)
	if len(cell) > 32768 { // 32KB per cell
		return errors.NewValidationError("csv_cell",
			map[string]interface{}{
				"row":      row,
				"col":      col,
				"filename": filename,
				"length":   len(cell),
			},
			fmt.Sprintf("第 %d 行第 %d 欄的內容過長 (最大 32KB)", row, col))
	}

	// Check for non-UTF8 content
	if !utf8.ValidString(cell) {
		return errors.NewValidationError("csv_cell",
			map[string]interface{}{
				"row":      row,
				"col":      col,
				"filename": filename,
			},
			fmt.Sprintf("第 %d 行第 %d 欄包含非 UTF-8 字符", row, col))
	}

	// CSV Formula Injection Detection (Critical Security Issue)
	formulaStarters := []string{"=", "+", "-", "@", "\t=", "\r=", "\n="}
	for _, starter := range formulaStarters {
		if strings.HasPrefix(cell, starter) {
			return errors.NewValidationError("csv_cell",
				map[string]interface{}{
					"row":      row,
					"col":      col,
					"filename": filename,
					"content":  cell,
				},
				fmt.Sprintf("第 %d 行第 %d 欄疑似包含 CSV 公式注入攻擊", row, col))
		}
	}

	// Dangerous function patterns (Excel/LibreOffice)
	dangerousFunctions := []string{
		"cmd", "CMD", "powershell", "POWERSHELL", "bash", "sh", "system", "SYSTEM",
		"exec", "EXEC", "eval", "EVAL", "spawn", "SPAWN", "shell", "SHELL",
		"hyperlink", "HYPERLINK", "importxml", "IMPORTXML", "importhtml", "IMPORTHTML",
		"importrange", "IMPORTRANGE", "importdata", "IMPORTDATA", "importfeed", "IMPORTFEED",
		"webservice", "WEBSERVICE", "filterxml", "FILTERXML", "document", "DOCUMENT",
		"indirect", "INDIRECT", "offset", "OFFSET", "choose", "CHOOSE",
		"dde", "DDE", "msqry", "MSQRY", "odbc", "ODBC", "query", "QUERY",
	}

	cellLower := strings.ToLower(cell)
	for _, dangerous := range dangerousFunctions {
		if strings.Contains(cellLower, strings.ToLower(dangerous)) {
			return errors.NewValidationError("csv_cell",
				map[string]interface{}{
					"row":      row,
					"col":      col,
					"filename": filename,
					"function": dangerous,
				},
				fmt.Sprintf("第 %d 行第 %d 欄包含危險函數: %s", row, col, dangerous))
		}
	}

	// Script injection patterns
	scriptPatterns := []string{
		"<script", "</script", "javascript:", "vbscript:", "data:text/html",
		"data:text/javascript", "data:application/javascript", "data:text/vbscript",
		"onload=", "onerror=", "onclick=", "onmouseover=", "onfocus=", "onblur=",
		"eval(", "setTimeout(", "setInterval(", "Function(", "constructor(",
		"alert(", "confirm(", "prompt(", "document.write", "document.writeln",
		"innerHTML", "outerHTML", "insertAdjacentHTML", "execScript",
	}

	for _, pattern := range scriptPatterns {
		if strings.Contains(cellLower, strings.ToLower(pattern)) {
			return errors.NewValidationError("csv_cell",
				map[string]interface{}{
					"row":      row,
					"col":      col,
					"filename": filename,
					"pattern":  pattern,
				},
				fmt.Sprintf("第 %d 行第 %d 欄包含腳本注入模式: %s", row, col, pattern))
		}
	}

	// SQL injection patterns
	sqlPatterns := []string{
		"union select", "UNION SELECT", "drop table", "DROP TABLE", "delete from", "DELETE FROM",
		"insert into", "INSERT INTO", "update set", "UPDATE SET", "create table", "CREATE TABLE",
		"alter table", "ALTER TABLE", "truncate table", "TRUNCATE TABLE", "grant all", "GRANT ALL",
		"revoke all", "REVOKE ALL", "show tables", "SHOW TABLES", "show databases", "SHOW DATABASES",
		"information_schema", "INFORMATION_SCHEMA", "sys.tables", "SYS.TABLES", "sysobjects", "SYSOBJECTS",
		"xp_cmdshell", "XP_CMDSHELL", "sp_executesql", "SP_EXECUTESQL", "openrowset", "OPENROWSET",
		"bulk insert", "BULK INSERT", "load_file", "LOAD_FILE", "into outfile", "INTO OUTFILE",
		"'or'1'='1", "\"or\"1\"=\"1", "'or 1=1", "\"or 1=1", "admin'--", "admin\"--",
		"'; drop", "\"; drop", "'; delete", "\"; delete", "'; insert", "\"; insert",
		"'; update", "\"; update", "'; create", "\"; create", "'; alter", "\"; alter",
	}

	for _, pattern := range sqlPatterns {
		if strings.Contains(cellLower, strings.ToLower(pattern)) {
			return errors.NewValidationError("csv_cell",
				map[string]interface{}{
					"row":      row,
					"col":      col,
					"filename": filename,
					"pattern":  pattern,
				},
				fmt.Sprintf("第 %d 行第 %d 欄包含 SQL 注入模式: %s", row, col, pattern))
		}
	}

	// Command injection patterns
	commandPatterns := []string{
		"curl ", "wget ", "nc ", "netcat ", "telnet ", "ssh ", "scp ", "rsync ",
		"cat /", "ls /", "pwd", "whoami", "id", "uname", "ps aux", "netstat",
		"iptables", "firewall", "sudo", "su -", "chmod", "chown", "mount",
		"umount", "kill", "killall", "pkill", "service", "systemctl",
		"crontab", "at ", "batch", "nohup", "screen", "tmux", "disown",
		"../", "..\\", "/etc/", "C:\\", "/bin/", "/usr/", "/var/", "/tmp/",
		"/home/", "/root/", "C:\\Windows\\", "C:\\Users\\", "C:\\Program Files\\",
	}

	for _, pattern := range commandPatterns {
		if strings.Contains(cell, pattern) {
			return errors.NewValidationError("csv_cell",
				map[string]interface{}{
					"row":      row,
					"col":      col,
					"filename": filename,
					"pattern":  pattern,
				},
				fmt.Sprintf("第 %d 行第 %d 欄包含命令注入模式: %s", row, col, pattern))
		}
	}

	// Check for null bytes and control characters
	for _, r := range cell {
		if r == 0 {
			return errors.NewValidationError("csv_cell",
				map[string]interface{}{
					"row":      row,
					"col":      col,
					"filename": filename,
				},
				fmt.Sprintf("第 %d 行第 %d 欄包含空字符", row, col))
		}

		// Allow common control characters (tab, newline, carriage return)
		if unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r' {
			return errors.NewValidationError("csv_cell",
				map[string]interface{}{
					"row":      row,
					"col":      col,
					"filename": filename,
					"char":     fmt.Sprintf("U+%04X", r),
				},
				fmt.Sprintf("第 %d 行第 %d 欄包含控制字符: U+%04X", row, col, r))
		}
	}

	// Check for suspicious file extensions or paths
	suspiciousExtensions := []string{
		".exe", ".bat", ".cmd", ".com", ".pif", ".scr", ".vbs", ".vbe", ".js", ".jse",
		".wsf", ".wsh", ".msi", ".msp", ".reg", ".scf", ".lnk", ".inf", ".dll", ".sys",
		".drv", ".ocx", ".cpl", ".jar", ".app", ".deb", ".rpm", ".dmg", ".pkg",
		".ps1", ".psm1", ".psd1", ".ps1xml", ".pssc", ".psrc", ".cdxml", ".sh",
		".bash", ".zsh", ".fish", ".ksh", ".tcsh", ".py", ".rb", ".pl", ".php",
	}

	for _, ext := range suspiciousExtensions {
		if strings.Contains(cellLower, ext) {
			return errors.NewValidationError("csv_cell",
				map[string]interface{}{
					"row":       row,
					"col":       col,
					"filename":  filename,
					"extension": ext,
				},
				fmt.Sprintf("第 %d 行第 %d 欄包含可疑的檔案副檔名: %s", row, col, ext))
		}
	}

	return nil
}

// EmailPattern for email validation (if needed for future features)
var EmailPattern = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)

// ValidateEmail validates email address format
func (v *InputValidator) ValidateEmail(email string) error {
	if email == "" {
		return errors.NewValidationError("email", email, "電子郵件地址不能為空")
	}

	email = strings.TrimSpace(email)

	if !EmailPattern.MatchString(email) {
		return errors.NewValidationError("email", email, "無效的電子郵件地址格式")
	}

	if len(email) > 254 {
		return errors.NewValidationError("email", email, "電子郵件地址過長")
	}

	return nil
}
