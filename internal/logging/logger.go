// Package logging provides structured logging functionality with support for
// JSON and text output formats, sensitive data masking, and context-aware logging.
package logging

import (
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"count_mean/internal/errors"
)

// LogLevel represents the severity level of a log entry.
type LogLevel int

// Log level constants define the severity levels for logging.
const (
	LevelDebug LogLevel = iota // LevelDebug represents debug-level logging
	LevelInfo                  // LevelInfo represents info-level logging
	LevelWarn                  // LevelWarn represents warning-level logging
	LevelError                 // LevelError represents error-level logging
	LevelFatal                 // LevelFatal represents fatal-level logging
)

// File permission constants.
const (
	logDirPermission  = 0o750 // Directory permission for log directory
	logFilePermission = 0o600 // File permission for log files
)

// Masking thresholds.
const (
	minMaskLength    = 4 // Minimum length before masking
	mediumMaskLength = 8 // Threshold for medium-length masking
)

// String returns the string representation of the log level.
func (l LogLevel) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	case LevelFatal:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

// LogEntry represents a structured log entry.
type LogEntry struct {
	Timestamp time.Time              `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Module    string                 `json:"module,omitempty"`
	Function  string                 `json:"function,omitempty"`
	File      string                 `json:"file,omitempty"`
	Line      int                    `json:"line,omitempty"`
	Error     string                 `json:"error,omitempty"`
	Context   map[string]interface{} `json:"context,omitempty"`
}

// Logger provides structured logging functionality.
type Logger struct {
	level             LogLevel
	output            io.Writer
	jsonFormat        bool
	module            string
	contextData       map[string]interface{}
	sensitivePatterns []*regexp.Regexp
}

// NewLogger creates a new logger instance.
func NewLogger(level LogLevel, output io.Writer, jsonFormat bool) *Logger {
	return &Logger{
		level:             level,
		output:            output,
		jsonFormat:        jsonFormat,
		contextData:       make(map[string]interface{}),
		sensitivePatterns: initSensitivePatterns(),
	}
}

// NewFileLogger creates a logger that writes to a file.
func NewFileLogger(level LogLevel, logDir, filename string, jsonFormat bool) (*Logger, error) {
	if err := os.MkdirAll(logDir, logDirPermission); err != nil {
		return nil, fmt.Errorf("cannot create log directory: %w", err)
	}

	logPath := filepath.Join(logDir, filename)

	//nolint:gosec // G304: logPath is constructed from logDir parameter
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, logFilePermission)
	if err != nil {
		return nil, fmt.Errorf("cannot create log file: %w", err)
	}

	return NewLogger(level, file, jsonFormat), nil
}

// WithModule returns a logger with a specific module context.
func (l *Logger) WithModule(module string) *Logger {
	newLogger := *l
	newLogger.module = module
	newLogger.contextData = make(map[string]interface{})

	for k, v := range l.contextData {
		newLogger.contextData[k] = v
	}

	newLogger.sensitivePatterns = l.sensitivePatterns

	return &newLogger
}

// WithContext adds context data to the logger.
func (l *Logger) WithContext(key string, value interface{}) *Logger {
	newLogger := *l
	newLogger.contextData = make(map[string]interface{})

	for k, v := range l.contextData {
		newLogger.contextData[k] = v
	}

	newLogger.contextData[key] = value
	newLogger.sensitivePatterns = l.sensitivePatterns

	return &newLogger
}

// initSensitivePatterns initializes patterns for sensitive data detection.
func initSensitivePatterns() []*regexp.Regexp {
	patterns := []string{
		`(?i)password\s*[=:]\s*[^\s]+`,
		`(?i)passwd\s*[=:]\s*[^\s]+`,
		`(?i)secret\s*[=:]\s*[^\s]+`,
		`(?i)token\s*[=:]\s*[^\s]+`,
		`(?i)key\s*[=:]\s*[^\s]+`,
		`(?i)auth\s*[=:]\s*[^\s]+`,
		`(?i)credential\s*[=:]\s*[^\s]+`,
		`(?i)api[-_]?key\s*[=:]\s*[^\s]+`,
		`(?i)access[-_]?token\s*[=:]\s*[^\s]+`,
		`(?i)refresh[-_]?token\s*[=:]\s*[^\s]+`,
		`(?i)bearer\s+[a-zA-Z0-9\-_\.]+`,
		`(?i)connection[-_]?string\s*[=:]\s*[^\s]+`,
		`(?i)database[-_]?url\s*[=:]\s*[^\s]+`,
		`(?i)db[-_]?password\s*[=:]\s*[^\s]+`,
		`\b\d{4}[-\s]?\d{4}[-\s]?\d{4}[-\s]?\d{4}\b`,
		`\b\d{3}-\d{2}-\d{4}\b`,
		`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`,
		`\b(?:\d{1,3}\.){3}\d{1,3}\b`,
		`(?i)[\\/].*(?:password|secret|key|token|credential).*[\\/]`,
		`(?i)"[^"]*(?:password|secret|key|token|credential)[^"]*"`,
		`(?i)'[^']*(?:password|secret|key|token|credential)[^']*'`,
		`(?i)\btoken\b[^=:]*[=:][^,}\s]+`,
		`(?i)\bbearer_token[^=:]*[=:][^,}\s]+`,
	}

	var compiledPatterns []*regexp.Regexp

	for _, pattern := range patterns {
		if re, err := regexp.Compile(pattern); err == nil {
			compiledPatterns = append(compiledPatterns, re)
		}
	}

	return compiledPatterns
}

// sanitizeMessage removes sensitive information from log messages.
func (l *Logger) sanitizeMessage(message string) string {
	if l.sensitivePatterns == nil {
		return message
	}

	sanitized := message
	for _, pattern := range l.sensitivePatterns {
		sanitized = pattern.ReplaceAllStringFunc(sanitized, maskSensitiveData)
	}

	return sanitized
}

// sanitizeContextValue removes sensitive information from context values.
func (l *Logger) sanitizeContextValue(value interface{}) interface{} {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case string:
		return l.sanitizeMessage(v)
	case map[string]interface{}:
		sanitized := make(map[string]interface{})
		for k, val := range v {
			sanitized[k] = l.sanitizeContextValue(val)
		}

		return sanitized
	case []interface{}:
		sanitized := make([]interface{}, len(v))
		for i, val := range v {
			sanitized[i] = l.sanitizeContextValue(val)
		}

		return sanitized
	default:
		return l.sanitizeMessage(fmt.Sprintf("%v", value))
	}
}

// maskSensitiveData masks sensitive data with asterisks.
func maskSensitiveData(data string) string {
	if len(data) <= minMaskLength {
		return "****"
	}

	parts := strings.Split(data, "=")
	if len(parts) == 2 {
		key := parts[0]
		value := parts[1]

		if len(value) <= minMaskLength {
			return key + "=****"
		}

		if len(value) > mediumMaskLength {
			return key + "=" + value[:2] + "****" + value[len(value)-2:]
		}

		return key + "=" + value[:1] + "****"
	}

	if len(data) > mediumMaskLength {
		return data[:2] + "****" + data[len(data)-2:]
	}

	return data[:1] + "****"
}

// WithError adds error context to the logger.
func (l *Logger) WithError(err error) *Logger {
	return l.WithContext("error", err.Error())
}

// log writes a log entry.
func (l *Logger) log(level LogLevel, message string, err error, context map[string]interface{}) {
	if level < l.level {
		return
	}

	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     level.String(),
		Message:   l.sanitizeMessage(message),
		Module:    l.module,
		Context:   make(map[string]interface{}),
	}

	addCallerInfo(&entry)
	l.addErrorInfo(&entry, err)

	for k, v := range l.contextData {
		entry.Context[k] = l.sanitizeContextValue(v)
	}

	for k, v := range context {
		entry.Context[k] = l.sanitizeContextValue(v)
	}

	if l.jsonFormat {
		l.writeJSON(&entry)
	} else {
		l.writeText(&entry)
	}
}

// addCallerInfo adds caller information to the log entry.
func addCallerInfo(entry *LogEntry) {
	const callerSkip = 4

	if pc, file, line, ok := runtime.Caller(callerSkip); ok {
		entry.File = filepath.Base(file)
		entry.Line = line

		if fn := runtime.FuncForPC(pc); fn != nil {
			entry.Function = filepath.Base(fn.Name())
		}
	}
}

// addErrorInfo adds error information to the log entry.
func (l *Logger) addErrorInfo(entry *LogEntry, err error) {
	if err == nil {
		return
	}

	entry.Error = l.sanitizeMessage(err.Error())

	var appErr *errors.AppError

	if stderrors.As(err, &appErr) {
		entry.Context["error_code"] = appErr.Code
		entry.Context["recoverable"] = appErr.Recoverable

		if appErr.Context != nil {
			for k, v := range appErr.Context {
				entry.Context[k] = l.sanitizeContextValue(v)
			}
		}
	}
}

// writeJSON writes the log entry in JSON format.
//
//nolint:errcheck,revive // log output write errors are intentionally ignored
func (l *Logger) writeJSON(entry *LogEntry) {
	data, err := json.Marshal(entry)
	if err != nil {
		l.writeText(entry)
		return
	}

	fmt.Fprintf(l.output, "%s\n", data)
}

// writeText writes the log entry in human-readable text format.
//
//nolint:errcheck,revive // log output write errors are intentionally ignored
func (l *Logger) writeText(entry *LogEntry) {
	parts := []string{
		entry.Timestamp.Format("2006-01-02 15:04:05"),
		fmt.Sprintf("[%s]", entry.Level),
	}

	if entry.Module != "" {
		parts = append(parts, fmt.Sprintf("[%s]", entry.Module))
	}

	parts = append(parts, entry.Message)

	if entry.File != "" && entry.Line > 0 {
		parts = append(parts, fmt.Sprintf("(%s:%d)", entry.File, entry.Line))
	}

	if entry.Error != "" {
		parts = append(parts, fmt.Sprintf("error=%s", entry.Error))
	}

	if len(entry.Context) > 0 {
		var contextParts []string

		for k, v := range entry.Context {
			contextStr := fmt.Sprintf("%s=%v", k, v)
			contextParts = append(contextParts, l.sanitizeMessage(contextStr))
		}

		parts = append(parts, fmt.Sprintf("context=[%s]", strings.Join(contextParts, " ")))
	}

	fmt.Fprintf(l.output, "%s\n", strings.Join(parts, " "))
}

// Debug logs a debug message.
func (l *Logger) Debug(message string, context ...map[string]interface{}) {
	var ctx map[string]interface{}
	if len(context) > 0 {
		ctx = context[0]
	}

	l.log(LevelDebug, message, nil, ctx)
}

// Info logs an info message.
func (l *Logger) Info(message string, context ...map[string]interface{}) {
	var ctx map[string]interface{}
	if len(context) > 0 {
		ctx = context[0]
	}

	l.log(LevelInfo, message, nil, ctx)
}

// Warn logs a warning message.
func (l *Logger) Warn(message string, context ...map[string]interface{}) {
	var ctx map[string]interface{}
	if len(context) > 0 {
		ctx = context[0]
	}

	l.log(LevelWarn, message, nil, ctx)
}

// Error logs an error message.
func (l *Logger) Error(message string, err error, context ...map[string]interface{}) {
	var ctx map[string]interface{}
	if len(context) > 0 {
		ctx = context[0]
	}

	l.log(LevelError, message, err, ctx)
}

// Fatal logs a fatal message and exits.
func (l *Logger) Fatal(message string, err error, context ...map[string]interface{}) {
	var ctx map[string]interface{}
	if len(context) > 0 {
		ctx = context[0]
	}

	l.log(LevelFatal, message, err, ctx)
	//nolint:revive // deep-exit is intentional for Fatal level logging
	os.Exit(1)
}

// Default logger instance.
//
//nolint:gochecknoglobals // Global logger instance is intentional for convenience
var defaultLogger *Logger

// InitLogger initializes the default logger.
func InitLogger(level LogLevel, logDir string, jsonFormat bool) error {
	fileLogger, err := NewFileLogger(level, logDir, "app.log", jsonFormat)
	if err != nil {
		return err
	}

	multiWriter := io.MultiWriter(fileLogger.output, os.Stdout)
	defaultLogger = NewLogger(level, multiWriter, jsonFormat)

	return nil
}

// GetLogger returns the default logger with optional module context.
func GetLogger(module ...string) *Logger {
	if defaultLogger == nil {
		defaultLogger = NewLogger(LevelInfo, os.Stderr, false)
	}

	if len(module) > 0 {
		return defaultLogger.WithModule(module[0])
	}

	return defaultLogger
}

// Debug logs a debug message using the default logger.
func Debug(message string, context ...map[string]interface{}) {
	GetLogger().Debug(message, context...)
}

// Info logs an info message using the default logger.
func Info(message string, context ...map[string]interface{}) {
	GetLogger().Info(message, context...)
}

// Warn logs a warning message using the default logger.
func Warn(message string, context ...map[string]interface{}) {
	GetLogger().Warn(message, context...)
}

// Error logs an error message using the default logger.
func Error(message string, err error, context ...map[string]interface{}) {
	GetLogger().Error(message, err, context...)
}

// Fatal logs a fatal message using the default logger and exits.
func Fatal(message string, err error, context ...map[string]interface{}) {
	GetLogger().Fatal(message, err, context...)
}
