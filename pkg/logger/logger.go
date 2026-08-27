// Package logger configures application logging.
package logger

import (
	"io"
	"os"
	"strings"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Init configures console logging to stderr.
func Init(debug bool) {
	initLogger(debug, os.Stderr, !isTerminal())
}

// InitWithWriter configures colorless console logging to w.
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

// Silence discards logs until the returned restore function runs.
func Silence() func() {
	prev := log.Logger
	log.Logger = zerolog.New(io.Discard)
	return func() {
		log.Logger = prev
	}
}

// Event aliases a chainable zerolog event.
type Event = zerolog.Event

// Debug returns an event at DEBUG level.
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
