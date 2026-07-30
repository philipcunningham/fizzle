//go:build js && wasm

package wasm

// Each blank import pins a package the Web UI core must keep
// compiling for js/wasm; the TUI and native audio never ship to the
// browser, so they are deliberately absent.
import (
	_ "github.com/philipcunningham/fizzle/pkg/disk"
	_ "github.com/philipcunningham/fizzle/pkg/diskadd"
	_ "github.com/philipcunningham/fizzle/pkg/diskcopy"
	_ "github.com/philipcunningham/fizzle/pkg/diskformat"
	_ "github.com/philipcunningham/fizzle/pkg/diskget"
	_ "github.com/philipcunningham/fizzle/pkg/disklist"
	_ "github.com/philipcunningham/fizzle/pkg/fzbinfo"
	_ "github.com/philipcunningham/fizzle/pkg/fzfeffects"
	_ "github.com/philipcunningham/fizzle/pkg/fzfinfo"
	_ "github.com/philipcunningham/fizzle/pkg/fzfmidi"
	_ "github.com/philipcunningham/fizzle/pkg/fzfoutput"
	_ "github.com/philipcunningham/fizzle/pkg/fzutil"
	_ "github.com/philipcunningham/fizzle/pkg/fzvinfo"
	_ "github.com/philipcunningham/fizzle/pkg/sfz"
	_ "github.com/philipcunningham/fizzle/pkg/sfzconvert"
	_ "github.com/philipcunningham/fizzle/pkg/sfzexport"
	_ "github.com/philipcunningham/fizzle/pkg/voicebuild"
	_ "github.com/philipcunningham/fizzle/pkg/voiceedit"
	_ "github.com/philipcunningham/fizzle/pkg/voiceextract"
	_ "github.com/philipcunningham/fizzle/pkg/voiceimport"
	_ "github.com/philipcunningham/fizzle/pkg/voiceunpack"
	_ "github.com/philipcunningham/fizzle/pkg/wav"
)
