// Package sfz implements a minimal parser for the SFZ sampler format.
// It handles the subset of the format needed to convert an SFZ instrument
// into an FZ series full dump.
//
// Supported:
//   - <global>, <group>, <master>, <region> headers with opcode inheritance
//   - <control> header with default_path= and #define directives
//   - #include for file composition
//   - key= shorthand (sets lokey, hikey, pitch_keycenter)
//   - Note name values (c4, c#4, db4, etc.)
//   - mutegroup=N for monophonic voice groups
//   - // line comments and /* */ block comments
//   - Opcodes on the same line as headers
//   - Sample paths with spaces
//
// Unsupported opcodes are collected as warnings. SFZ v2 headers not listed
// above (<curve>, <effect>, <midi>, <sample>) are silently skipped.
package sfz

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/philipcunningham/fizzle/pkg/disk"
	"github.com/philipcunningham/fizzle/pkg/fzutil"
)

const (
	// maxRegions matches the FZ-1 hardware limit of 64 voices per bank.
	maxRegions           = disk.MaxVoices
	maxSFZFileSize int64 = 16 << 20

	// DefaultLoVel is the minimum velocity for an SFZ region.
	DefaultLoVel = 1
	// DefaultHiVel is the maximum velocity for an SFZ region.
	DefaultHiVel = 127

	// defaultKeycenter is the SFZ v1 spec default for pitch_keycenter (C4).
	defaultKeycenter = 60

	// MaxTranspose bounds the SFZ transpose= opcode (in semitones) per the
	// SFZ v1 spec. ±127 is also the widest range that survives the FZ-1 dcp
	// field's signed 16-bit encoding at 1/256-semitone resolution
	// (±127 * 256 = ±32512, within int16's ±32767).
	MaxTranspose = 127
	// MinTranspose is the lower bound for the SFZ transpose= opcode.
	MinTranspose = -127

	// MaxTune bounds the SFZ tune= opcode (in cents) per the SFZ v1 spec.
	MaxTune = 100
	// MinTune is the lower bound for the SFZ tune= opcode.
	MinTune = -100
)

// Region holds the resolved opcodes for a single SFZ region, with
// group, master, and global inheritance already applied.
//
// Cutoff, Resonance, LoopStart, and LoopEnd use -1 as an in-band "opcode
// absent" sentinel: downstream code in pkg/sfzconvert only applies these
// fields when the value is >= 0. Construct via NewRegion to get the
// sentinels in place; the Go zero value of 0 is a valid (and dangerous)
// hardware setting for these fields.
type Region struct {
	Sample         string
	LoKey          uint8
	HiKey          uint8
	PitchKeycenter uint8
	LoVel          uint8
	HiVel          uint8
	Transpose      int
	Tune           int
	// MuteGroup implements monophonic grouping (SFZ mutegroup= opcode, as
	// exported by Renoise). A new note cuts off any playing note in the same
	// group. HasMuteGroup distinguishes "opcode present with value 0" from
	// "opcode absent". Only regions with HasMuteGroup=true are monophonic.
	MuteGroup    int
	HasMuteGroup bool
	OneShot      bool
	Cutoff       int
	Resonance    int
	LoopStart    int
	LoopEnd      int
}

// NewRegion returns a Region with optional-opcode fields pre-set to their
// "absent" sentinels (-1) and velocity range set to the SFZ defaults
// (1..127). Callers should override the fields that the SFZ source
// actually specifies and leave the rest alone.
//
// Sample, LoKey, HiKey, and PitchKeycenter have no useful default and
// must be set by the caller.
func NewRegion() Region {
	return Region{
		LoVel:     DefaultLoVel,
		HiVel:     DefaultHiVel,
		Cutoff:    -1,
		Resonance: -1,
		LoopStart: -1,
		LoopEnd:   -1,
	}
}

// Warning describes an opcode or condition handled gracefully rather than
// treated as an error.
type Warning struct {
	Region  int
	Message string
}

// String returns the warning as a human-readable message.
func (w Warning) String() string {
	if w.Region >= 0 {
		return fmt.Sprintf("region %d: %s", w.Region+1, w.Message)
	}
	return w.Message
}

// noteOffsets maps note letter names to their semitone offset from C.
var noteOffsets = map[byte]int{'c': 0, 'd': 2, 'e': 4, 'f': 5, 'g': 7, 'a': 9, 'b': 11}

// knownOpcodes is the set we map to FZ-1 parameters. Everything else warns.
var knownOpcodes = map[string]struct{}{
	"sample": {}, "lokey": {}, "hikey": {}, "key": {},
	"pitch_keycenter": {}, "lovel": {}, "hivel": {},
	"transpose": {}, "tune": {},
	"mutegroup": {}, "loop_mode": {},
	"cutoff": {}, "resonance": {},
	"loop_start": {}, "loop_end": {},
}

// opcodes is a simple string-to-string map for a single header scope.
type opcodes map[string]string

// Parse reads the SFZ file at path and returns the resolved regions and any
// warnings generated during parsing. Sample paths are resolved to absolute
// paths relative to the SFZ file's directory (or default_path if set).
//
// Path confinement: paths referenced from the SFZ (#include, default_path,
// sample=) that resolve outside the top-level SFZ's directory tree emit
// warnings but are still read. The intent is to flag accidental and
// adversarial paths without breaking legitimate SFZ packs that share WAVs
// across sibling directories. The check is lexical (filepath.Rel) and does
// not resolve symlinks; an attacker with write access to a symlink inside
// the root can escape detection.
func Parse(path string) ([]Region, []Warning, error) {
	p := &parser{
		defines:  map[string]string{},
		included: map[string]bool{},
		global:   opcodes{},
		master:   opcodes{},
		group:    opcodes{},
	}
	p.current = p.global

	// Resolve the top-level root before parsing so #include checks can use
	// it. We tolerate filepath.Abs errors here because parseFile re-runs
	// the same call and surfaces the error there.
	if abs, err := filepath.Abs(path); err == nil {
		p.rootDir = filepath.Dir(abs)
	}

	if err := p.parseFile(path); err != nil {
		return nil, p.warnings, err
	}
	if len(p.regions) == 0 && !p.sawRegionTag {
		return nil, p.warnings, fmt.Errorf("sfz: no regions found in %q", path)
	}
	if len(p.regions) > maxRegions {
		return nil, p.warnings, fmt.Errorf("sfz: %d regions exceeds maximum of %d", len(p.regions), maxRegions)
	}
	return p.regions, p.warnings, nil
}

type parser struct {
	defines      map[string]string
	included     map[string]bool
	warnings     []Warning
	regions      []Region
	global       opcodes
	master       opcodes
	group        opcodes
	current      opcodes
	defaultPath  string
	currentDir   string
	rootDir      string // absolute directory of the top-level SFZ; used for path-confinement warnings
	inRegion     bool
	inControl    bool
	sawRegionTag bool // true if at least one <region> tag was encountered
}

// isOutsideRoot reports whether target resolves outside p.rootDir. The
// comparison is lexical: target and root are both made absolute and
// filepath.Rel is consulted. A symlinked path inside the root that points
// outside is not detected. Returns false (i.e., "inside, no warning") if
// the root is empty or either path cannot be made absolute, since failing
// open is safer than spamming spurious warnings.
func (p *parser) isOutsideRoot(target string) bool {
	if p.rootDir == "" {
		return false
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(p.rootDir, absTarget)
	if err != nil {
		// Different volumes (Windows) -> definitely outside.
		return true
	}
	// "..", "../foo", or any path starting with parent-of-root is outside.
	// An absolute result indicates Rel could not produce a relative path,
	// which also means outside.
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return true
	}
	if filepath.IsAbs(rel) {
		return true
	}
	return false
}

func (p *parser) parseFile(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("sfz: resolving %q: %w", path, err)
	}
	path = abs
	if p.included[path] {
		p.warnings = append(p.warnings, Warning{
			Region:  -1,
			Message: fmt.Sprintf("ignoring repeated #include of %q (include cycle or duplicate)", filepath.Base(path)),
		})
		return nil
	}
	p.included[path] = true

	// Path-confinement: warn if an #include resolves outside the top-level
	// SFZ directory tree. The top-level file itself sets rootDir (in Parse)
	// so the first call here is always inside its own root and skipped.
	if p.isOutsideRoot(path) {
		p.warnings = append(p.warnings, Warning{
			Region:  -1,
			Message: fmt.Sprintf("#include %q resolves outside the SFZ root directory", path),
		})
	}

	raw, err := fzutil.ReadBounded(path, maxSFZFileSize)
	if err != nil {
		return fmt.Errorf("sfz: reading %q: %w", path, err)
	}
	dir := filepath.Dir(path)
	prevDir := p.currentDir
	p.currentDir = dir
	text := stripComments(string(raw))
	err = p.parseText(text, dir)
	p.currentDir = prevDir
	return err
}

func (p *parser) parseText(text, dir string) error {
	tokens := tokenise(text)
	i := 0
	for i < len(tokens) {
		tok := tokens[i]

		if tok == "#define" {
			i++
			if i+1 >= len(tokens) {
				break
			}
			name := tokens[i]
			i++
			p.defines[name] = tokens[i]
			i++
			continue
		}

		if tok == "#include" {
			i++
			// Collect all tokens until we have a complete quoted string.
			var incParts []string
			for i < len(tokens) {
				part := tokens[i]
				incParts = append(incParts, part)
				i++
				// A complete include path is wrapped in quotes.
				joined := strings.Join(incParts, " ")
				if strings.HasPrefix(joined, `"`) && strings.HasSuffix(joined, `"`) {
					break
				}
				// No quotes: single bare token.
				if !strings.HasPrefix(joined, `"`) {
					break
				}
			}
			inc := strings.Trim(strings.Join(incParts, " "), `"`)
			incPath := filepath.Join(dir, filepath.FromSlash(inc))
			if err := p.parseFile(incPath); err != nil {
				return err
			}
			continue
		}

		if strings.HasPrefix(tok, "<") && strings.HasSuffix(tok, ">") {
			p.flushRegion()
			tag := strings.ToLower(tok[1 : len(tok)-1])
			switch tag {
			case "global":
				p.global = opcodes{}
				p.master = opcodes{}
				p.group = opcodes{}
				p.current = p.global
				p.inRegion = false
				p.inControl = false
			case "master":
				p.master = opcodes{}
				p.group = opcodes{}
				p.current = p.master
				p.inRegion = false
				p.inControl = false
			case "group":
				p.group = opcodes{}
				p.current = p.group
				p.inRegion = false
				p.inControl = false
			case "region":
				p.current = opcodes{}
				p.inRegion = true
				p.inControl = false
				p.sawRegionTag = true
			case "control":
				p.current = opcodes{}
				p.inRegion = false
				p.inControl = true
			default:
				p.warnings = append(p.warnings, Warning{
					Region:  -1,
					Message: fmt.Sprintf("ignoring unsupported header <%s>; opcodes inside are discarded", tag),
				})
				p.current = nil
				p.inRegion = false
				p.inControl = false
			}
			i++
			continue
		}

		// key=value pair
		keyRaw, value, ok := strings.Cut(tok, "=")
		if !ok {
			i++
			continue
		}
		key := strings.ToLower(keyRaw)

		// The value may continue across subsequent tokens until we hit
		// another key=value token or a header tag.
		for i+1 < len(tokens) {
			next := tokens[i+1]
			if strings.HasPrefix(next, "<") || strings.Contains(next, "=") || strings.HasPrefix(next, "#") {
				break
			}
			value += " " + next
			i++
		}
		value = strings.TrimSpace(value)

		// Apply #define substitution.
		if sub, ok := p.defines[value]; ok {
			value = sub
		}

		p.applyOpcode(key, value, dir)
		i++
	}
	p.flushRegion()
	return nil
}

func (p *parser) applyOpcode(key, value, dir string) {
	if p.current == nil {
		return
	}

	// <control> special opcodes.
	if p.inControl {
		if key == "default_path" {
			p.defaultPath = filepath.Join(dir, filepath.FromSlash(value))
			if p.isOutsideRoot(p.defaultPath) {
				p.warnings = append(p.warnings, Warning{
					Region:  -1,
					Message: fmt.Sprintf("default_path=%q resolves outside the SFZ root directory", value),
				})
			}
			return
		}
	}

	p.current[key] = value
}

func (p *parser) lookup(key string) (string, bool) {
	if p.current != nil {
		if v, ok := p.current[key]; ok {
			return v, true
		}
	}
	if v, ok := p.group[key]; ok {
		return v, true
	}
	if v, ok := p.master[key]; ok {
		return v, true
	}
	if v, ok := p.global[key]; ok {
		return v, true
	}
	return "", false
}

// clampVel clamps a velocity value to the SFZ-spec range [0, 127] and emits
// a warning when clamping occurs. Mirrors the transpose/tune clamp warning
// in flushRegion so out-of-range velocity values are surfaced rather than
// silently swallowed.
func (p *parser) clampVel(key string, n int) uint8 {
	if n < 0 || n > disk.MaxMIDINote {
		clamped := n
		if clamped < 0 {
			clamped = 0
		}
		if clamped > disk.MaxMIDINote {
			clamped = disk.MaxMIDINote
		}
		p.warnings = append(p.warnings, Warning{
			Region:  len(p.regions),
			Message: fmt.Sprintf("%s=%d out of range [0, %d]; clamped to %d", key, n, disk.MaxMIDINote, clamped),
		})
		n = clamped
	}
	return uint8(n) //nolint:gosec // G115: bounded by the clamp above
}

func (p *parser) flushRegion() {
	if !p.inRegion {
		return
	}
	p.inRegion = false

	sampleRaw, ok := p.lookup("sample")
	if !ok || sampleRaw == "" {
		p.warnings = append(p.warnings, Warning{Region: len(p.regions), Message: "no sample opcode, skipping"})
		return
	}

	// Resolve sample path to absolute using default_path or the SFZ file's directory.
	samplePath := filepath.FromSlash(sampleRaw)
	if !filepath.IsAbs(samplePath) {
		if p.defaultPath != "" {
			samplePath = filepath.Join(p.defaultPath, samplePath)
		} else {
			samplePath = filepath.Join(p.currentDir, samplePath)
		}
	}

	// Path-confinement: warn (softly) if the sample resolves outside the
	// top-level SFZ directory tree. Real SFZ packs sometimes share WAVs
	// across sibling directories so this is not treated as an error.
	if p.isOutsideRoot(samplePath) {
		p.warnings = append(p.warnings, Warning{
			Region:  len(p.regions),
			Message: fmt.Sprintf("sample=%q resolves outside the SFZ root directory", sampleRaw),
		})
	}

	parseInt := func(key string, def int) int {
		v, ok := p.lookup(key)
		if !ok || v == "" {
			return def
		}
		n, err := parseKeyValue(v)
		if err != nil {
			p.warnings = append(p.warnings, Warning{
				Region:  len(p.regions),
				Message: fmt.Sprintf("malformed %s=%q: %v; using default %d", key, v, err, def),
			})
			return def
		}
		return n
	}
	// clamp restricts n to the SFZ key/velocity domain of 0..127 and
	// returns it as the uint8 the hardware structs expect.
	clamp := func(n int) uint8 {
		if n < 0 {
			n = 0
		}
		if n > disk.MaxMIDINote {
			n = disk.MaxMIDINote
		}
		return uint8(n)
	}

	var lokey, hikey, keycenter uint8
	keycentreSet := false

	// key= is shorthand for lokey=hikey=pitch_keycenter=
	if kv, ok := p.lookup("key"); ok && kv != "" {
		n, err := parseKeyValue(kv)
		if err == nil {
			lokey = clamp(n)
			hikey = lokey
			keycenter = lokey
		}
	} else {
		lokey = clamp(parseInt("lokey", 0))
		hikey = clamp(parseInt("hikey", disk.MaxMIDINote))
		if kc, ok := p.lookup("pitch_keycenter"); ok && kc != "" {
			n, err := parseKeyValue(kc)
			if err == nil {
				keycenter = clamp(n)
				keycentreSet = true
			}
		}
		if !keycentreSet {
			keycenter = defaultKeycenter
		}
	}

	// SFZ velocity range is 0..127. Don't clamp to DefaultLoVel: a region
	// exported with (0, 0) must survive round-trip rather than be silently
	// promoted to (1, 1). The DefaultLoVel/DefaultHiVel constants are still
	// used as fallbacks when the opcode is absent (see parseInt).
	//
	// Out-of-range values are clamped to [0, 127] and warned about, mirroring
	// the transpose/tune handling below. The SFZ spec defines velocity as
	// 0..127 so lovel=200 is malformed.
	lovel := p.clampVel("lovel", parseInt("lovel", DefaultLoVel))
	hivel := p.clampVel("hivel", parseInt("hivel", DefaultHiVel))
	if hivel < lovel {
		p.warnings = append(p.warnings, Warning{
			Region:  len(p.regions),
			Message: fmt.Sprintf("hivel=%d < lovel=%d, region will never trigger", hivel, lovel),
		})
	} else if lovel == 0 && hivel == 0 {
		// Spec §1-5 says htch/ltch range is 1-127. MIDI note-on velocity is
		// also 1-127 (vel 0 is note-off), so (0,0) cannot match any note-on
		// and the voice will be silent on hardware.
		p.warnings = append(p.warnings, Warning{
			Region:  len(p.regions),
			Message: "lovel=0 and hivel=0; voice will be silent (FZ-1 velocity range is 1-127)",
		})
	}
	transpose := parseInt("transpose", 0)
	if transpose < MinTranspose || transpose > MaxTranspose {
		clamped := transpose
		if clamped < MinTranspose {
			clamped = MinTranspose
		}
		if clamped > MaxTranspose {
			clamped = MaxTranspose
		}
		p.warnings = append(p.warnings, Warning{
			Region:  len(p.regions),
			Message: fmt.Sprintf("transpose=%d out of range [%d, %d]; clamped to %d", transpose, MinTranspose, MaxTranspose, clamped),
		})
		transpose = clamped
	}
	tune := parseInt("tune", 0)
	if tune < MinTune || tune > MaxTune {
		clamped := tune
		if clamped < MinTune {
			clamped = MinTune
		}
		if clamped > MaxTune {
			clamped = MaxTune
		}
		p.warnings = append(p.warnings, Warning{
			Region:  len(p.regions),
			Message: fmt.Sprintf("tune=%d out of range [%d, %d]; clamped to %d", tune, MinTune, MaxTune, clamped),
		})
		tune = clamped
	}
	_, hasMuteGroup := p.lookup("mutegroup")
	muteGroup := parseInt("mutegroup", 0)
	loopMode, _ := p.lookup("loop_mode")
	oneShot := strings.EqualFold(loopMode, "one_shot")

	cutoff := parseInt("cutoff", -1)
	if cutoff > disk.MaxMIDINote {
		cutoff = disk.MaxMIDINote
	}
	resonance := parseInt("resonance", -1)
	if resonance > disk.MaxResonance {
		resonance = disk.MaxResonance
	}

	loopStart := parseInt("loop_start", -1)
	loopEnd := parseInt("loop_end", -1)

	// Warn on opcodes we recognise but don't map.
	for k := range p.current {
		if _, known := knownOpcodes[k]; !known {
			p.warnings = append(p.warnings, Warning{
				Region:  len(p.regions),
				Message: fmt.Sprintf("unsupported opcode %q ignored", k),
			})
		}
	}

	r := NewRegion()
	r.Sample = samplePath
	r.LoKey = lokey
	r.HiKey = hikey
	r.PitchKeycenter = keycenter
	r.LoVel = lovel
	r.HiVel = hivel
	r.Transpose = transpose
	r.Tune = tune
	r.MuteGroup = muteGroup
	r.HasMuteGroup = hasMuteGroup
	r.OneShot = oneShot
	r.Cutoff = cutoff
	r.Resonance = resonance
	r.LoopStart = loopStart
	r.LoopEnd = loopEnd
	p.regions = append(p.regions, r)
}

// tokenise splits SFZ text (with comments already stripped) into whitespace-
// separated tokens. Header tags (<region> etc.) and directives (#define,
// #include) are emitted as-is. Key=value pairs where the value contains
// spaces (e.g. sample=JUNGLE Samples/foo.wav) are assembled by the caller
// in parseText using the presence of '=' as a delimiter.
func tokenise(text string) []string {
	return strings.Fields(text)
}

// stripComments removes // line comments and /* */ block comments from s.
func stripComments(s string) string {
	var out strings.Builder
	out.Grow(len(s))
	i := 0
	for i < len(s) {
		if i+1 < len(s) && s[i] == '/' && s[i+1] == '/' {
			// Skip to end of line.
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		if i+1 < len(s) && s[i] == '/' && s[i+1] == '*' {
			i += 2
			for i+1 < len(s) && (s[i] != '*' || s[i+1] != '/') {
				i++
			}
			if i+1 < len(s) {
				i += 2
			} else {
				i = len(s)
			}
			continue
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
}

// parseKeyValue parses a MIDI key value that may be a note number ("60")
// or a note name ("c4", "c#4", "db4", "C4", etc.).
func parseKeyValue(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("sfz: empty value")
	}
	// Try plain integer first.
	if n, err := strconv.Atoi(s); err == nil {
		return n, nil
	}
	// Try note name: letter [# or b] octave
	// e.g. c4=60, c#4=61, db4=61, a#3=46
	s = strings.ToLower(s)
	if len(s) < 2 {
		return 0, fmt.Errorf("sfz: cannot parse key %q", s)
	}
	base, ok := noteOffsets[s[0]]
	if !ok {
		return 0, fmt.Errorf("sfz: cannot parse key %q", s)
	}
	rest := s[1:]
	accidental := 0
	if strings.HasPrefix(rest, "#") || strings.HasPrefix(rest, "s") {
		accidental = 1
		rest = rest[1:]
	} else if strings.HasPrefix(rest, "b") && len(rest) > 1 && unicode.IsDigit(rune(rest[1])) {
		accidental = -1
		rest = rest[1:]
	}
	// Handle negative octaves (e.g. c-1 = MIDI 0).
	octave, err := strconv.Atoi(rest)
	if err != nil {
		return 0, fmt.Errorf("sfz: cannot parse octave in %q", s)
	}
	// SFZ convention: C4 = MIDI 60, so C-1 = 0.
	midi := (octave+1)*disk.SemitonesPerOctave + base + accidental
	if midi < 0 || midi > disk.MaxMIDINote {
		return 0, fmt.Errorf("sfz: note %q out of MIDI range", s)
	}
	return midi, nil
}
