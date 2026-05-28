package logger

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Not parallel: mutates global zerolog level and log.Logger.
func TestInitDebugLevel(t *testing.T) {
	oldLevel := zerolog.GlobalLevel()
	oldLogger := log.Logger
	t.Cleanup(func() {
		zerolog.SetGlobalLevel(oldLevel)
		log.Logger = oldLogger
	})
	Init(true)
	if zerolog.GlobalLevel() != zerolog.DebugLevel {
		t.Errorf("expected DebugLevel, got %v", zerolog.GlobalLevel())
	}
}

// Not parallel: mutates global zerolog level and log.Logger.
func TestInitInfoLevel(t *testing.T) {
	oldLevel := zerolog.GlobalLevel()
	oldLogger := log.Logger
	t.Cleanup(func() {
		zerolog.SetGlobalLevel(oldLevel)
		log.Logger = oldLogger
	})
	Init(false)
	if zerolog.GlobalLevel() != zerolog.InfoLevel {
		t.Errorf("expected InfoLevel, got %v", zerolog.GlobalLevel())
	}
}

// Not parallel: mutates global zerolog level and log.Logger.
func TestInitWithWriter(t *testing.T) {
	oldLevel := zerolog.GlobalLevel()
	oldLogger := log.Logger
	t.Cleanup(func() {
		zerolog.SetGlobalLevel(oldLevel)
		log.Logger = oldLogger
	})
	var buf bytes.Buffer
	InitWithWriter(false, &buf)
	log.Info().Msg("test message")
	if !strings.Contains(buf.String(), "test message") {
		t.Errorf("expected log output to contain 'test message', got: %s", buf.String())
	}
}

// Not parallel: mutates global zerolog level and log.Logger.
func TestInitWritesToStderr(t *testing.T) {
	oldLevel := zerolog.GlobalLevel()
	oldLogger := log.Logger
	t.Cleanup(func() {
		zerolog.SetGlobalLevel(oldLevel)
		log.Logger = oldLogger
	})
	Init(false)
	var buf bytes.Buffer
	log.Logger = log.Output(&buf)
	log.Info().Msg("test message")
	if !strings.Contains(buf.String(), "test message") {
		t.Error("expected log output to contain test message")
	}
}
