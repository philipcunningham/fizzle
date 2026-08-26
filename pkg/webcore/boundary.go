package webcore

import (
	"fmt"
	"strings"

	"github.com/philipcunningham/fizzle/pkg/disk"
)

// Error is the stable error envelope exposed across the WASM boundary.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Item    string `json:"item,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func errf(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: publicMessage(fmt.Sprintf(format, args...))}
}

// publicMessage removes Go package context from errors crossing the product
// boundary. The stable error code already identifies the failing operation;
// package names are implementation details a browser user cannot act on.
func publicMessage(message string) string {
	for {
		prefix, rest, found := strings.Cut(message, ": ")
		if !found || prefix == "" || strings.ContainsAny(prefix, " /\\") {
			return message
		}
		message = rest
	}
}

func errItemf(code, item, format string, args ...any) *Error {
	e := errf(code, format, args...)
	e.Item = item
	return e
}

const (
	codeNoDisk       = "no-disk"
	codeInvalidImage = "invalid-image"
	codeInvalidField = "invalid-field"
	codeInvalidValue = "invalid-value"
	codeNotFound     = "not-found"
	codePairMismatch = "pair-mismatch"
	codeLastArea     = "last-area"
	codeMissingDisk  = "missing-disk"
)

// FileSnapshot describes one directory entry for the UI.
type FileSnapshot struct {
	Name      string         `json:"name"`
	Type      string         `json:"type"`
	SizeBytes int            `json:"sizeBytes"`
	Params    map[string]any `json:"params,omitempty"`
	Voice     *VoiceDetail   `json:"voice,omitempty"`
}

// DiskSnapshot describes the open disk set for the UI.
type DiskSnapshot struct {
	Label         string              `json:"label"`
	UsedBytes     int                 `json:"usedBytes"`
	CapacityBytes int                 `json:"capacityBytes"`
	AudioBytes    int                 `json:"audioBytes"`
	MemoryBytes   int                 `json:"memoryBytes"`
	Disks         int                 `json:"disks"`
	MissingDisk   int                 `json:"missingDisk,omitempty"`
	Files         []FileSnapshot      `json:"files"`
	Instrument    *InstrumentSnapshot `json:"instrument,omitempty"`
}

// Snapshot is the state the UI renders from.
type Snapshot struct {
	Revision int           `json:"revision"`
	Disk     *DiskSnapshot `json:"disk"`
	CanUndo  bool          `json:"canUndo"`
	CanRedo  bool          `json:"canRedo"`
}

// Snapshot returns a detached state projection for the UI.
func (s *Session) Snapshot() Snapshot {
	snap := Snapshot{Revision: s.revision, CanUndo: len(s.past) > 0, CanRedo: len(s.future) > 0}
	if !s.state.IsOpen() {
		return snap
	}
	disks := 1
	if s.state.HasSecondDisk() {
		disks = 2
	}
	snap.Disk = &DiskSnapshot{
		Label: s.label, UsedBytes: s.used, CapacityBytes: disks * disk.ImageSize,
		AudioBytes: s.audioBytes, MemoryBytes: s.sampleMemory(), Disks: disks,
		MissingDisk: s.missingDisk, Files: cloneFiles(s.files),
		Instrument: cloneInstrument(s.instrument),
	}
	return snap
}

const (
	// SampleMemoryMin is the smallest supported sampler memory size.
	SampleMemoryMin = 1 << 20
	// SampleMemoryMax is the largest supported sampler memory size.
	SampleMemoryMax = 2 << 20
)

func (s *Session) sampleMemory() int {
	if s.memoryBytes == 0 {
		return SampleMemoryMin
	}
	return s.memoryBytes
}

// SetSampleMemory records the sampler's available sample memory.
func (s *Session) SetSampleMemory(bytes int) (Snapshot, *Error) {
	if bytes < SampleMemoryMin || bytes > SampleMemoryMax {
		return s.Snapshot(), errf(codeInvalidValue,
			"sample memory %d is outside the 1 MB to 2 MB an FZ holds", bytes)
	}
	s.memoryBytes = bytes
	return s.Snapshot(), nil
}
