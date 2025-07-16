package logging

import (
	"count_mean/internal/errors"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// LogLevel represents the severity level of a log entry
type LogLevel int

const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
)

// String returns the string representation of the log level
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

// LogEntry represents a structured log entry
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

// Logger provides structured logging functionality
type Logger struct {
	level             LogLevel
	output            io.Writer
	jsonFormat        bool
	module            string
	contextData       map[string]interface{}
	sensitivePatterns []*regexp.Regexp
}

// NewLogger creates a new logger instance
func NewLogger(level LogLevel, output io.Writer, jsonFormat bool) *Logger {
	return &Logger{
		level:             level,
		output:            output,
		jsonFormat:        jsonFormat,
		contextData:       make(map[string]interface{}),
		sensitivePatterns: initSensitivePatterns(),
	}
}

// NewFileLogger creates a logger that writes to a file
func NewFileLogger(level LogLevel, logDir, filename string, jsonFormat bool) (*Logger, error) {
	// Ensure log directory exists
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("無法創建日誌目錄: %w", err)
	}

	logPath := filepath.Join(logDir, filename)
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0640)
	if err != nil {
		return nil, fmt.Errorf("無法創建日誌檔案: %w", err)
	}

	return NewLogger(level, file, jsonFormat), nil
}

// WithModule returns a logger with a specific module context
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

// WithContext adds context data to the logger
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

// initSensitivePatterns initializes patterns for sensitive data detection
func initSensitivePatterns() []*regexp.Regexp {
	patterns := []string{
		// Passwords and secrets
		`(?i)password\s*[=:]\s*[^\s]+`,
		`(?i)passwd\s*[=:]\s*[^\s]+`,
		`(?i)secret\s*[=:]\s*[^\s]+`,
		`(?i)token\s*[=:]\s*[^\s]+`,
		`(?i)key\s*[=:]\s*[^\s]+`,
		`(?i)auth\s*[=:]\s*[^\s]+`,
		`(?i)credential\s*[=:]\s*[^\s]+`,

		// API keys and tokens
		`(?i)api[-_]?key\s*[=:]\s*[^\s]+`,
		`(?i)access[-_]?token\s*[=:]\s*[^\s]+`,
		`(?i)refresh[-_]?token\s*[=:]\s*[^\s]+`,
		`(?i)bearer\s+[a-zA-Z0-9\-_\.]+`,

		// Database connection strings
		`(?i)connection[-_]?string\s*[=:]\s*[^\s]+`,
		`(?i)database[-_]?url\s*[=:]\s*[^\s]+`,
		`(?i)db[-_]?password\s*[=:]\s*[^\s]+`,

		// Credit card numbers (basic pattern)
		`\b\d{4}[-\s]?\d{4}[-\s]?\d{4}[-\s]?\d{4}\b`,

		// Social security numbers (basic pattern)
		`\b\d{3}-\d{2}-\d{4}\b`,

		// Email addresses (partial masking)
		`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`,

		// IP addresses (optional masking)
		`\b(?:\d{1,3}\.){3}\d{1,3}\b`,

		// File paths that might contain sensitive info
		`(?i)[\\/].*(?:password|secret|key|token|credential).*[\\/]`,

		// Common sensitive keywords in various formats
		`(?i)"[^"]*(?:password|secret|key|token|credential)[^"]*"`,
		`(?i)'[^']*(?:password|secret|key|token|credential)[^']*'`,

		// Context value patterns
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

// sanitizeMessage removes sensitive information from log messages
func (l *Logger) sanitizeMessage(message string) string {
	if l.sensitivePatterns == nil {
		return message
	}

	sanitized := message
	for _, pattern := range l.sensitivePatterns {
		sanitized = pattern.ReplaceAllStringFunc(sanitized, func(match string) string {
			return l.maskSensitiveData(match)
		})
	}

	return sanitized
}

// sanitizeContextValue removes sensitive information from context values
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
		// For other types, convert to string and sanitize
		return l.sanitizeMessage(fmt.Sprintf("%v", value))
	}
}

// maskSensitiveData masks sensitive data with asterisks
func (l *Logger) maskSensitiveData(data string) string {
	if len(data) <= 4 {
		return "****"
	}

	// Look for key=value pattern
	parts := strings.Split(data, "=")
	if len(parts) == 2 {
		key := parts[0]
		value := parts[1]

		if len(value) <= 4 {
			return key + "=****"
		}

		// For longer values, show first 2 and last 2 characters
		if len(value) > 8 {
			return key + "=" + value[:2] + "****" + value[len(value)-2:]
		}

		// For medium values, show first character
		return key + "=" + value[:1] + "****"
	}

	// For non-key=value patterns, apply general masking
	if len(data) > 8 {
		return data[:2] + "****" + data[len(data)-2:]
	}

	// For medium strings, show first character
	return data[:1] + "****"
}

// WithError adds error context to the logger
func (l *Logger) WithError(err error) *Logger {
	return l.WithContext("error", err.Error())
}

// log writes a log entry
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

	// Add caller information
	if pc, file, line, ok := runtime.Caller(2); ok {
		entry.File = filepath.Base(file)
		entry.Line = line
		if fn := runtime.FuncForPC(pc); fn != nil {
			entry.Function = filepath.Base(fn.Name())
		}
	}

	// Add error information
	if err != nil {
		entry.Error = l.sanitizeMessage(err.Error())

		// Add structured error information if it's an AppError
		if appErr, ok := err.(*errors.AppError); ok {
			entry.Context["error_code"] = appErr.Code
			entry.Context["recoverable"] = appErr.Recoverable
			if appErr.Context != nil {
				for k, v := range appErr.Context {
					entry.Context[k] = l.sanitizeContextValue(v)
				}
			}
		}
	}

	// Add logger context
	for k, v := range l.contextData {
		entry.Context[k] = l.sanitizeContextValue(v)
	}

	// Add additional context
	for k, v := range context {
		entry.Context[k] = l.sanitizeContextValue(v)
	}

	// Write the log entry
	if l.jsonFormat {
		l.writeJSON(entry)
	} else {
		l.writeText(entry)
	}
}

// writeJSON writes the log entry in JSON format
func (l *Logger) writeJSON(entry LogEntry) {
	data, err := json.Marshal(entry)
	if err != nil {
		// Fallback to simple text logging if JSON marshal fails
		l.writeText(entry)
		return
	}

	fmt.Fprintf(l.output, "%s\n", data)
}

// writeText writes the log entry in human-readable text format
func (l *Logger) writeText(entry LogEntry) {
	var parts []string

	// Timestamp and level
	parts = append(parts, entry.Timestamp.Format("2006-01-02 15:04:05"))
	parts = append(parts, fmt.Sprintf("[%s]", entry.Level))

	// Module
	if entry.Module != "" {
		parts = append(parts, fmt.Sprintf("[%s]", entry.Module))
	}

	// Message
	parts = append(parts, entry.Message)

	// File and line
	if entry.File != "" && entry.Line > 0 {
		parts = append(parts, fmt.Sprintf("(%s:%d)", entry.File, entry.Line))
	}

	// Error
	if entry.Error != "" {
		parts = append(parts, fmt.Sprintf("error=%s", entry.Error))
	}

	// Context
	if len(entry.Context) > 0 {
		var contextParts []string
		for k, v := range entry.Context {
			// Additional sanitization for context formatting
			contextStr := fmt.Sprintf("%s=%v", k, v)
			contextParts = append(contextParts, l.sanitizeMessage(contextStr))
		}
		parts = append(parts, fmt.Sprintf("context=[%s]", strings.Join(contextParts, " ")))
	}

	fmt.Fprintf(l.output, "%s\n", strings.Join(parts, " "))
}

// Debug logs a debug message
func (l *Logger) Debug(message string, context ...map[string]interface{}) {
	var ctx map[string]interface{}
	if len(context) > 0 {
		ctx = context[0]
	}
	l.log(LevelDebug, message, nil, ctx)
}

// Info logs an info message
func (l *Logger) Info(message string, context ...map[string]interface{}) {
	var ctx map[string]interface{}
	if len(context) > 0 {
		ctx = context[0]
	}
	l.log(LevelInfo, message, nil, ctx)
}

// Warn logs a warning message
func (l *Logger) Warn(message string, context ...map[string]interface{}) {
	var ctx map[string]interface{}
	if len(context) > 0 {
		ctx = context[0]
	}
	l.log(LevelWarn, message, nil, ctx)
}

// Error logs an error message
func (l *Logger) Error(message string, err error, context ...map[string]interface{}) {
	var ctx map[string]interface{}
	if len(context) > 0 {
		ctx = context[0]
	}
	l.log(LevelError, message, err, ctx)
}

// Fatal logs a fatal message and exits
func (l *Logger) Fatal(message string, err error, context ...map[string]interface{}) {
	var ctx map[string]interface{}
	if len(context) > 0 {
		ctx = context[0]
	}
	l.log(LevelFatal, message, err, ctx)
	os.Exit(1)
}

// Default logger instance
var defaultLogger *Logger

// InitLogger initializes the default logger
func InitLogger(level LogLevel, logDir string, jsonFormat bool) error {
	// Create logger that writes to both file and stdout
	fileLogger, err := NewFileLogger(level, logDir, "app.log", jsonFormat)
	if err != nil {
		return err
	}

	// Create a multi-writer that writes to both file and stdout
	multiWriter := io.MultiWriter(fileLogger.output, os.Stdout)
	defaultLogger = NewLogger(level, multiWriter, jsonFormat)

	return nil
}

// GetLogger returns the default logger with optional module context
func GetLogger(module ...string) *Logger {
	if defaultLogger == nil {
		// Fallback to stderr logger if not initialized
		defaultLogger = NewLogger(LevelInfo, os.Stderr, false)
	}

	if len(module) > 0 {
		return defaultLogger.WithModule(module[0])
	}

	return defaultLogger
}

// Convenience functions using the default logger
func Debug(message string, context ...map[string]interface{}) {
	GetLogger().Debug(message, context...)
}

func Info(message string, context ...map[string]interface{}) {
	GetLogger().Info(message, context...)
}

func Warn(message string, context ...map[string]interface{}) {
	GetLogger().Warn(message, context...)
}

func Error(message string, err error, context ...map[string]interface{}) {
	GetLogger().Error(message, err, context...)
}

func Fatal(message string, err error, context ...map[string]interface{}) {
	GetLogger().Fatal(message, err, context...)
}
