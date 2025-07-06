package logging

import (
	"count_mean/internal/errors"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	level       LogLevel
	output      io.Writer
	jsonFormat  bool
	module      string
	contextData map[string]interface{}
}

// NewLogger creates a new logger instance
func NewLogger(level LogLevel, output io.Writer, jsonFormat bool) *Logger {
	return &Logger{
		level:       level,
		output:      output,
		jsonFormat:  jsonFormat,
		contextData: make(map[string]interface{}),
	}
}

// NewFileLogger creates a logger that writes to a file
func NewFileLogger(level LogLevel, logDir, filename string, jsonFormat bool) (*Logger, error) {
	// Ensure log directory exists
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("無法創建日誌目錄: %w", err)
	}

	logPath := filepath.Join(logDir, filename)
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
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
	return &newLogger
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
		Message:   message,
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
		entry.Error = err.Error()

		// Add structured error information if it's an AppError
		if appErr, ok := err.(*errors.AppError); ok {
			entry.Context["error_code"] = appErr.Code
			entry.Context["recoverable"] = appErr.Recoverable
			if appErr.Context != nil {
				for k, v := range appErr.Context {
					entry.Context[k] = v
				}
			}
		}
	}

	// Add logger context
	for k, v := range l.contextData {
		entry.Context[k] = v
	}

	// Add additional context
	for k, v := range context {
		entry.Context[k] = v
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
			contextParts = append(contextParts, fmt.Sprintf("%s=%v", k, v))
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
