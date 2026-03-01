// Package logger provides structured logging for the build-server application.
// All logs are written to stdout with timestamps and proper log levels.
package logger

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

// LogLevel represents the severity of a log message
type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
)

func (l LogLevel) String() string {
	switch l {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARN:
		return "WARN"
	case ERROR:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

var (
	globalLogger *Logger
	once         sync.Once
)

// Logger provides structured logging functionality
type Logger struct {
	level  LogLevel
	output *log.Logger
}

// Init initializes the global logger with the specified minimum log level.
// This should be called once at application startup.
func Init(minLevel LogLevel) {
	once.Do(func() {
		globalLogger = &Logger{
			level:  minLevel,
			output: log.New(os.Stdout, "", 0),
		}
	})
}

// SetLevel changes the minimum log level at runtime
func SetLevel(level LogLevel) {
	if globalLogger != nil {
		globalLogger.level = level
	}
}

// log writes a log message if the level is >= the minimum level
func (l *Logger) log(level LogLevel, format string, args ...interface{}) {
	if level < l.level {
		return
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	message := fmt.Sprintf(format, args...)

	// Handle multi-line messages by indenting continuation lines
	lines := strings.Split(message, "\n")
	for i, line := range lines {
		if i == 0 {
			l.output.Printf("[%s] %s %s", level.String(), timestamp, line)
		} else {
			l.output.Printf("[%s] %s     %s", level.String(), timestamp, line)
		}
	}
}

// Debug logs a debug message
func Debug(format string, args ...interface{}) {
	if globalLogger != nil {
		globalLogger.log(DEBUG, format, args...)
	}
}

// Info logs an info message
func Info(format string, args ...interface{}) {
	if globalLogger != nil {
		globalLogger.log(INFO, format, args...)
	}
}

// Warn logs a warning message
func Warn(format string, args ...interface{}) {
	if globalLogger != nil {
		globalLogger.log(WARN, format, args...)
	}
}

// Error logs an error message
func Error(format string, args ...interface{}) {
	if globalLogger != nil {
		globalLogger.log(ERROR, format, args...)
	}
}

// Fatal logs an error message and exits the application
func Fatal(format string, args ...interface{}) {
	if globalLogger != nil {
		globalLogger.log(ERROR, format, args...)
	}
	os.Exit(1)
}
