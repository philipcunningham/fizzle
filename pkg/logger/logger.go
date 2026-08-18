// Package logger configures zerolog as the application logger.
// Call Init once from main before running any commands.
package logger

import (
	"io"
	"os"
	"strings"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Init configures the global zerolog logger with a human-readable console
// writer directed to stderr. If debug is true, DEBUG-level messages are
// shown; otherwise only INFO and above.
func Init(debug bool) {
	initLogger(debug, os.Stderr, !isTerminal())
}

// InitWithWriter configures the global zerolog logger with a human-readable
// console writer directed to w, colour disabled. Tests use it to capture
// log output without mutating stderr.
func InitWithWriter(debug bool, w io.Writer) {
	initLogger(debug, w, true)
}

func initLogger(debug bool, w io.Writer, noColor bool) {
	level := zerolog.InfoLevel
	if debug {
		level = zerolog.DebugLevel
	}

	zerolog.SetGlobalLevel(level)

	output := zerolog.ConsoleWriter{
		Out:     w,
		NoColor: noColor,
		FormatTimestamp: func(_ any) string {
			return ""
		},
		FormatLevel: formatLevel,
	}

	log.Logger = zerolog.New(output).Level(level)
}

// Silence swaps the global zerolog logger for one that discards output and
// returns a function restoring the previous logger. Callers such as
// benchmarks use it to mute library log noise without redirecting their
// own stderr; defer the returned function so the swap reverses on exit.
func Silence() func() {
	prev := log.Logger
	log.Logger = zerolog.New(io.Discard)
	return func() {
		log.Logger = prev
	}
}

// Event is the chainable log event the level constructors below return.
// Aliasing zerolog's own event lets callers write the familiar
// .Str(...).Int(...).Err(...).Msg(...) chain without importing zerolog.
type Event = zerolog.Event

// Debug returns a chainable log event at DEBUG level. The event is a no-op
// unless the global level (set by Init) is DEBUG or below.
func Debug() *Event { return log.Debug() }

// Info returns a chainable log event at INFO level.
func Info() *Event { return log.Info() }

// Warn returns a chainable log event at WARN level.
func Warn() *Event { return log.Warn() }

// Error returns a chainable log event at ERROR level.
func Error() *Event { return log.Error() }

func formatLevel(i any) string {
	s, ok := i.(string)
	if !ok {
		return ""
	}
	switch strings.ToUpper(s) {
	case "DEBUG":
		return "DEBUG"
	case "INFO":
		return "INFO "
	case "WARN":
		return "WARN "
	case "ERROR":
		return "ERROR"
	default:
		return strings.ToUpper(s)
	}
}

func isTerminal() bool {
	fi, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
