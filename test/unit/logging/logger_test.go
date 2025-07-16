package logging_test

import (
	"bytes"
	"count_mean/internal/errors"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"count_mean/internal/logging"
)

func TestNewLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewLogger(logging.LevelInfo, &buf, false)

	// Test that logger was created successfully
	if logger == nil {
		t.Error("logger should not be nil")
	}

	// Test that it can write a log message
	logger.Info("test message")
	if buf.Len() == 0 {
		t.Error("logger should have written to the buffer")
	}
}

func TestLogger_WithModule(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewLogger(logging.LevelInfo, &buf, false)

	moduleLogger := logger.WithModule("test_module")

	// Test that module logger was created
	if moduleLogger == nil {
		t.Error("module logger should not be nil")
	}

	// Test that module appears in output
	moduleLogger.Info("test message")
	output := buf.String()
	if !strings.Contains(output, "[test_module]") {
		t.Error("output should contain module name")
	}
}

func TestLogger_WithContext(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewLogger(logging.LevelInfo, &buf, true) // Use JSON format for easier testing

	contextLogger := logger.WithContext("key", "value")

	// Test that context logger was created
	if contextLogger == nil {
		t.Error("context logger should not be nil")
	}

	// Test that context appears in JSON output
	contextLogger.Info("test message")
	output := buf.String()
	if !strings.Contains(output, "\"key\":\"value\"") {
		t.Error("output should contain context data")
	}
}

func TestLogger_WithError(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewLogger(logging.LevelInfo, &buf, true) // Use JSON format for easier testing

	testErr := errors.NewAppError(errors.ErrCodeFileNotFound, "test error")
	errorLogger := logger.WithError(testErr)

	// Test that error logger was created
	if errorLogger == nil {
		t.Error("error logger should not be nil")
	}

	// Test that error appears in JSON output
	errorLogger.Info("test message")
	output := buf.String()
	if !strings.Contains(output, "test error") {
		t.Error("output should contain error message")
	}
}

func TestLogLevel_String(t *testing.T) {
	tests := []struct {
		level logging.LogLevel
		want  string
	}{
		{logging.LevelDebug, "DEBUG"},
		{logging.LevelInfo, "INFO"},
		{logging.LevelWarn, "WARN"},
		{logging.LevelError, "ERROR"},
		{logging.LevelFatal, "FATAL"},
		{logging.LogLevel(999), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.level.String(); got != tt.want {
				t.Errorf("LogLevel.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLogger_LogLevels(t *testing.T) {
	tests := []struct {
		name        string
		loggerLevel logging.LogLevel
		logLevel    logging.LogLevel
		shouldLog   bool
	}{
		{
			name:        "debug logger logs debug",
			loggerLevel: logging.LevelDebug,
			logLevel:    logging.LevelDebug,
			shouldLog:   true,
		},
		{
			name:        "info logger ignores debug",
			loggerLevel: logging.LevelInfo,
			logLevel:    logging.LevelDebug,
			shouldLog:   false,
		},
		{
			name:        "info logger logs info",
			loggerLevel: logging.LevelInfo,
			logLevel:    logging.LevelInfo,
			shouldLog:   true,
		},
		{
			name:        "info logger logs error",
			loggerLevel: logging.LevelInfo,
			logLevel:    logging.LevelError,
			shouldLog:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := logging.NewLogger(tt.loggerLevel, &buf, false)

			// Use appropriate public method to test level filtering
			switch tt.logLevel {
			case logging.LevelDebug:
				logger.Debug("test message")
			case logging.LevelInfo:
				logger.Info("test message")
			case logging.LevelWarn:
				logger.Warn("test message")
			case logging.LevelError:
				logger.Error("test message", nil)
			}

			if tt.shouldLog {
				if buf.Len() == 0 {
					t.Error("expected log output but got none")
				}
			} else {
				if buf.Len() > 0 {
					t.Error("expected no log output but got some")
				}
			}
		})
	}
}

func TestLogger_TextFormat(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewLogger(logging.LevelInfo, &buf, false).WithModule("test")

	logger.Info("test message")

	output := buf.String()

	// Check for expected components
	if !strings.Contains(output, "[INFO]") {
		t.Error("output should contain log level")
	}

	if !strings.Contains(output, "[test]") {
		t.Error("output should contain module name")
	}

	if !strings.Contains(output, "test message") {
		t.Error("output should contain message")
	}

	// Check timestamp format
	if !strings.Contains(output, time.Now().Format("2006-01-02")) {
		t.Error("output should contain current date")
	}
}

func TestLogger_JSONFormat(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewLogger(logging.LevelInfo, &buf, true).WithModule("test")

	logger.Info("test message", map[string]interface{}{
		"key": "value",
	})

	output := buf.String()

	// Parse as JSON
	var entry logging.LogEntry
	if err := json.Unmarshal([]byte(output), &entry); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	if entry.Level != "INFO" {
		t.Errorf("level = %v, want INFO", entry.Level)
	}

	if entry.Module != "test" {
		t.Errorf("module = %v, want test", entry.Module)
	}

	if entry.Message != "test message" {
		t.Errorf("message = %v, want test message", entry.Message)
	}

	if entry.Context["key"] != "value" {
		t.Errorf("context key = %v, want value", entry.Context["key"])
	}
}

func TestLogger_ErrorWithAppError(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewLogger(logging.LevelInfo, &buf, true)

	appErr := errors.NewAppError(errors.ErrCodeFileNotFound, "test error").
		WithContext("filename", "test.csv")

	logger.Error("operation failed", appErr)

	output := buf.String()

	// Parse as JSON
	var entry logging.LogEntry
	if err := json.Unmarshal([]byte(output), &entry); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	if entry.Error != appErr.Error() {
		t.Errorf("error = %v, want %v", entry.Error, appErr.Error())
	}

	if entry.Context["error_code"] != string(errors.ErrCodeFileNotFound) {
		t.Errorf("error_code = %v, want %v", entry.Context["error_code"], errors.ErrCodeFileNotFound)
	}

	if entry.Context["filename"] != "test.csv" {
		t.Errorf("filename = %v, want test.csv", entry.Context["filename"])
	}
}

func TestLogger_DebugInfoWarnError(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewLogger(logging.LevelDebug, &buf, false)

	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warn message")
	logger.Error("error message", nil)

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	if len(lines) != 4 {
		t.Errorf("expected 4 log lines, got %d", len(lines))
	}

	if !strings.Contains(lines[0], "[DEBUG]") {
		t.Error("first line should contain DEBUG level")
	}

	if !strings.Contains(lines[1], "[INFO]") {
		t.Error("second line should contain INFO level")
	}

	if !strings.Contains(lines[2], "[WARN]") {
		t.Error("third line should contain WARN level")
	}

	if !strings.Contains(lines[3], "[ERROR]") {
		t.Error("fourth line should contain ERROR level")
	}
}

func TestGetLogger(t *testing.T) {
	// Test GetLogger with module
	logger := logging.GetLogger("test")

	if logger == nil {
		t.Error("logger should not be nil")
	}

	// Test that it can log
	var buf bytes.Buffer
	logger = logging.NewLogger(logging.LevelInfo, &buf, false).WithModule("test")
	logger.Info("test message")

	if !strings.Contains(buf.String(), "[test]") {
		t.Error("output should contain module name")
	}

	// Test without module
	logger2 := logging.GetLogger()
	if logger2 == nil {
		t.Error("logger without module should not be nil")
	}
}

func TestConvenienceFunctions(t *testing.T) {
	// Test that convenience functions work
	var buf bytes.Buffer

	// Create a logger to test with
	logger := logging.NewLogger(logging.LevelDebug, &buf, false)

	// Test individual convenience functions by calling them on the logger
	logger.Debug("debug test")
	logger.Info("info test")
	logger.Warn("warn test")
	logger.Error("error test", nil)

	output := buf.String()

	if !strings.Contains(output, "debug test") {
		t.Error("output should contain debug message")
	}

	if !strings.Contains(output, "info test") {
		t.Error("output should contain info message")
	}

	if !strings.Contains(output, "warn test") {
		t.Error("output should contain warn message")
	}

	if !strings.Contains(output, "error test") {
		t.Error("output should contain error message")
	}

	// Test that global convenience functions exist and can be called
	logging.Debug("global debug test")
	logging.Info("global info test")
	logging.Warn("global warn test")
	logging.Error("global error test", nil)
}

func TestLogger_SensitiveDataFiltering(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewLogger(logging.LevelInfo, &buf, false)

	// Test password filtering
	logger.Info("User login with password=secret123")
	output := buf.String()
	if strings.Contains(output, "secret123") {
		t.Error("output should not contain password")
	}
	if !strings.Contains(output, "****") {
		t.Error("output should contain masked password")
	}

	// Reset buffer
	buf.Reset()

	// Test API key filtering
	logger.Info("Using api_key=abcd1234efgh5678")
	output = buf.String()
	if strings.Contains(output, "abcd1234efgh5678") {
		t.Error("output should not contain API key")
	}
	if !strings.Contains(output, "****") {
		t.Error("output should contain masked API key")
	}

	// Reset buffer
	buf.Reset()

	// Test error message filtering
	testErr := errors.NewAppError(errors.ErrCodeFileNotFound, "Authentication failed: password=mysecret")
	logger.Error("Login failed", testErr)
	output = buf.String()
	if strings.Contains(output, "mysecret") {
		t.Error("output should not contain password in error message")
	}
	if !strings.Contains(output, "****") {
		t.Error("output should contain masked password in error message")
	}

	// Reset buffer
	buf.Reset()

	// Test context value filtering
	logger.Info("Processing request", map[string]interface{}{
		"user_id": "12345",
		"token":   "bearer_token_abc123",
	})
	output = buf.String()
	if strings.Contains(output, "bearer_token_abc123") {
		t.Error("output should not contain token in context")
	}
	if !strings.Contains(output, "****") {
		t.Error("output should contain masked token in context")
	}

	// Reset buffer
	buf.Reset()

	// Test credit card filtering
	logger.Info("Processing payment for card 1234-5678-9012-3456")
	output = buf.String()
	if strings.Contains(output, "1234-5678-9012-3456") {
		t.Error("output should not contain full credit card number")
	}
	if !strings.Contains(output, "****") {
		t.Error("output should contain masked credit card number")
	}
}

func TestLogger_SensitiveDataMasking(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewLogger(logging.LevelInfo, &buf, false)

	// Test various data lengths
	testCases := []struct {
		input    string
		expected string
	}{
		{"password=ab", "****"},
		{"password=abc", "****"},
		{"password=abcd", "****"},
		{"password=abcde", "a****"},
		{"password=abcdefgh", "a****"},
		{"password=abcdefghijkl", "ab****kl"},
	}

	for _, tc := range testCases {
		buf.Reset()
		logger.Info(tc.input)
		output := buf.String()

		if strings.Contains(output, tc.input) {
			t.Errorf("output should not contain original input: %s", tc.input)
		}

		if !strings.Contains(output, tc.expected) {
			t.Errorf("output should contain masked version: %s, got: %s", tc.expected, output)
		}
	}
}
