// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package logger

import (
	"log/slog"
	"os"
	"path/filepath"
)

var (
	log     *slog.Logger
	logFile *os.File
)

func init() {
	// Default to no-op logger (discard all output).
	log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

// Init opens the log file and configures the package-level logger to write
// JSON at Debug level. Call Close() when done.
func Init(logFilePath string) error {
	if err := os.MkdirAll(filepath.Dir(logFilePath), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	logFile = f
	log = slog.New(slog.NewJSONHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return nil
}

// Close flushes and closes the log file.
func Close() {
	if logFile != nil {
		logFile.Close()
		logFile = nil
	}
}

// Debug logs at debug level.
func Debug(msg string, args ...any) { log.Debug(msg, args...) }

// Info logs at info level.
func Info(msg string, args ...any) { log.Info(msg, args...) }

// Warn logs at warn level.
func Warn(msg string, args ...any) { log.Warn(msg, args...) }

// Error logs at error level.
func Error(msg string, args ...any) { log.Error(msg, args...) }
