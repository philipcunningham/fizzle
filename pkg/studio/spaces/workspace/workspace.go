// Package workspace is the studio Workspace space: a directory
// browser scoped to file types studio can act on. Subdirectories
// always appear (so the user can traverse); regular files appear only
// when their extension matches one of the supported types
// (.img, .fzf, .fzv, .wav). Pressing Enter on a row descends into a
// directory or emits an Intent describing the action that should run;
// the App routes the intent to the loader (for disks) or to the pool
// (for voices and samples).
package workspace

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"

	"github.com/philipcunningham/fizzle/pkg/studio/nav"
	"github.com/philipcunningham/fizzle/pkg/studio/theme"
	"github.com/philipcunningham/fizzle/pkg/studio/widgets/hint"
)

// FileKind tags each row by the action a Confirm gesture produces.
type FileKind int

const (
	// KindUnknown is the zero value for files whose extension isn't
	// one of the workspace's supported types.
	KindUnknown FileKind = iota
	// KindDir is a sub-directory; Enter descends into it.
	KindDir
	// KindDisk is a .img FZ-1 floppy disk image.
	KindDisk
	// KindDump is a .fzf FZ Full Dump.
	KindDump
	// KindVoice is a .fzv single voice.
	KindVoice
	// KindSample is a .wav raw sample to be wrapped on import.
	KindSample
)

func classify(name string, isDir bool) FileKind {
	if isDir {
		return KindDir
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".img":
		return KindDisk
	case ".fzf":
		return KindDump
	case ".fzv":
		return KindVoice
	case ".wav":
		return KindSample
	}
	return KindUnknown
}

// IntentKind tags the App-level action a confirmed file produces.
type IntentKind int

const (
	// IntentNone is the zero value; the App takes no transition.
	IntentNone IntentKind = iota
	// IntentOpenContainer signals the App should load the file as a
	// container (.img or .fzf) and route it to Layout.
	IntentOpenContainer
	// IntentAddVoiceToPool signals the App should import the .fzv
	// at Path into the Pool.
	IntentAddVoiceToPool
	// IntentAddSampleToPool signals the App should import the .wav
	// at Path into the Pool.
	IntentAddSampleToPool
)

// Intent carries the data the App needs to act on a confirmed file.
type Intent struct {
	Kind IntentKind
	Path string
}

// Entry is one visible row in the browser.
type Entry struct {
	Name string
	Path string
	Kind FileKind
}

// Model is the Workspace space state.
type Model struct {
	directory string  // current directory shown in the header
	cwd       string  // absolute directory the listing reflects
	entries   []Entry // sorted: dirs first, then files; alpha within each group
	cursor    int     // index into entries
}

// New scans the directory and returns a populated Model. The
// directory is resolved to an absolute path so the ascend-boundary
// check works regardless of whether the caller passed a relative or
// absolute path.
func New(directory string) Model {
	abs := directory
	if a, err := filepath.Abs(directory); err == nil {
		abs = a
	}
	m := Model{directory: abs, cwd: abs}
	m.reload()
	return m
}

// Refresh re-reads the current directory from disk. Use it after the
// filesystem has changed externally (a file was dropped in, removed,
// or renamed by another process) so the browser reflects the new
// state.
func (m *Model) Refresh() { m.reload() }

// reload re-reads m.cwd from disk, applies the supported-types filter,
// sorts (directories first, alphabetical within group), and clamps the
// cursor to the new entry count.
func (m *Model) reload() {
	files, err := os.ReadDir(m.cwd)
	if err != nil {
		m.entries = nil
		m.cursor = 0
		return
	}
	entries := make([]Entry, 0, len(files))
	for _, f := range files {
		name := f.Name()
		if strings.HasPrefix(name, ".") {
			continue // hide dotfiles
		}
		kind := classify(name, f.IsDir())
		if kind == KindUnknown {
			continue
		}
		entries = append(entries, Entry{
			Name: name,
			Path: filepath.Join(m.cwd, name),
			Kind: kind,
		})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		// Directories first.
		if (entries[i].Kind == KindDir) != (entries[j].Kind == KindDir) {
			return entries[i].Kind == KindDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	m.entries = entries
	if m.cursor >= len(entries) {
		if len(entries) == 0 {
			m.cursor = 0
		} else {
			m.cursor = len(entries) - 1
		}
	}
}

// Apply handles a navigation action.
func (m *Model) Apply(a nav.Action) (statusMsg string, intent Intent) {
	switch a { //nolint:exhaustive // workspace only consumes a subset of nav actions; default is no-op
	case nav.NavUp:
		if m.cursor > 0 {
			m.cursor--
		}
	case nav.NavDown:
		if m.cursor < len(m.entries)-1 {
			m.cursor++
		}
	case nav.NavLeft, nav.Cancel:
		// Ascend, but never above the directory the App started in;
		// surface a hint if the user tries. Use filepath.Rel for the
		// containment test so it works regardless of whether the
		// caller passed absolute or relative paths.
		if m.cwd == m.directory {
			return "Cannot ascend past the workspace root", Intent{}
		}
		parent := filepath.Dir(m.cwd)
		if parent == m.cwd {
			return "Already at filesystem root", Intent{}
		}
		if rel, err := filepath.Rel(m.directory, parent); err != nil ||
			rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "Cannot ascend past the workspace root", Intent{}
		}
		m.cwd = parent
		m.reload()
	case nav.NavRight, nav.Confirm:
		if len(m.entries) == 0 {
			return "", Intent{}
		}
		row := m.entries[m.cursor]
		if row.Kind == KindDir {
			m.cwd = row.Path
			m.cursor = 0
			m.reload()
			return "", Intent{}
		}
		switch row.Kind {
		case KindDisk, KindDump:
			return "", Intent{Kind: IntentOpenContainer, Path: row.Path}
		case KindVoice:
			return "", Intent{Kind: IntentAddVoiceToPool, Path: row.Path}
		case KindSample:
			return "", Intent{Kind: IntentAddSampleToPool, Path: row.Path}
		case KindUnknown, KindDir:
			// Directory drill-in is handled above; unknown kinds are inert.
		}
	default:
		// Other nav actions are not meaningful in the workspace browser.
	}
	return "", Intent{}
}

// Directory returns the workspace directory passed to New (the root
// the browser is anchored at; user can't ascend above it).
func (m Model) Directory() string { return m.directory }

// CurrentDirectory returns the directory the browser is currently
// showing (may be a subdirectory of Directory()).
func (m Model) CurrentDirectory() string { return m.cwd }

// Cursor returns the current row index (used by tests).
func (m Model) Cursor() int { return m.cursor }

// HighlightedPath returns the full path of the file or directory the
// cursor is currently on, or "" when the directory is empty.
func (m Model) HighlightedPath() string {
	if m.cursor < 0 || m.cursor >= len(m.entries) {
		return ""
	}
	return m.entries[m.cursor].Path
}

// SubstituteDirectoryPrefix rewrites m.directory and m.cwd by replacing
// origDir's prefix with label. Used by snapshot tests to mask the
// random tempdir prefix so the rendered header is laid out from the
// shorter label (post-render substitution can't fix layout width
// drift). A no-op when origDir is empty or not a prefix.
func (m *Model) SubstituteDirectoryPrefix(origDir, label string) {
	if origDir == "" {
		return
	}
	if strings.HasPrefix(m.directory, origDir) {
		m.directory = label + strings.TrimPrefix(m.directory, origDir)
	}
	if strings.HasPrefix(m.cwd, origDir) {
		m.cwd = label + strings.TrimPrefix(m.cwd, origDir)
	}
	// Entry paths also carry the prefix; rewrite them in lockstep so
	// any path-bearing assertion in a test sees the stable form.
	for i := range m.entries {
		if strings.HasPrefix(m.entries[i].Path, origDir) {
			m.entries[i].Path = label + strings.TrimPrefix(m.entries[i].Path, origDir)
		}
	}
}

// View renders the Workspace pane.
func (m Model) View(width, _ int) string {
	header := theme.Heading.Render("Workspace") +
		theme.DimText.Render("   "+m.displayPath())
	body := m.renderBody()
	hintBlock := hint.View(width,
		"Browse disks, dumps, voices, and samples; disks open into the editor, voices and samples drop into the pool.")
	footer := theme.DimText.Render(
		"up/down move  •  Enter to descend or open  •  Left or Esc to go up  •  Ctrl-R refresh  •  SHIFT+down to Pool")
	return lipgloss.JoinVertical(lipgloss.Left, header, "", body, "", footer, "", hintBlock)
}

// displayPath returns the cwd shown in the header. When cwd is inside
// the workspace root, render the root + a relative suffix so the bar
// stays scannable on deeply nested paths. The root itself renders by
// its absolute path so the user knows where the browser is anchored.
func (m Model) displayPath() string {
	if m.cwd == m.directory {
		return m.directory
	}
	rel, err := filepath.Rel(m.directory, m.cwd)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return m.cwd
	}
	return m.directory + string(filepath.Separator) + rel
}

func (m Model) renderBody() string {
	if len(m.entries) == 0 {
		return theme.SilverText.Render(
			"No supported files here (.img, .fzf, .fzv, .wav) and no\n" +
				"subdirectories. Left or Esc to go up.")
	}
	rows := make([][]string, 0, len(m.entries))
	for i, e := range m.entries {
		marker := ""
		if i == m.cursor {
			marker = "▶"
		}
		name := e.Name
		if e.Kind == KindDir {
			name += "/"
		}
		rows = append(rows, []string{marker, kindBadge(e.Kind), name, kindLabel(e.Kind)})
	}
	cursor := m.cursor
	return table.New().
		Border(lipgloss.HiddenBorder()).
		BorderTop(false).BorderBottom(false).
		BorderLeft(false).BorderRight(false).
		BorderHeader(false).BorderColumn(false).BorderRow(false).
		Headers("", "", "name", "type").
		Rows(rows...).
		StyleFunc(func(rowIdx, col int) lipgloss.Style {
			if rowIdx == table.HeaderRow {
				return theme.DimText.Padding(0, 1)
			}
			if rowIdx == cursor {
				if col == 2 {
					return theme.Heading.Padding(0, 1)
				}
				return theme.AccentText.Padding(0, 1)
			}
			if col == 1 {
				return theme.AccentText.Padding(0, 1)
			}
			if col == 2 {
				return theme.PrimaryText.Padding(0, 1)
			}
			return theme.DimText.Padding(0, 1)
		}).
		Render()
}

func kindBadge(k FileKind) string {
	switch k {
	case KindDir:
		return "[/]"
	case KindDisk:
		return "[D]"
	case KindDump:
		return "[F]"
	case KindVoice:
		return "[V]"
	case KindSample:
		return "[W]"
	case KindUnknown:
		return "[?]"
	default:
		return "[?]"
	}
}

func kindLabel(k FileKind) string {
	switch k {
	case KindDir:
		return "dir"
	case KindDisk:
		return "disk"
	case KindDump:
		return "dump"
	case KindVoice:
		return "voice"
	case KindSample:
		return "sample"
	case KindUnknown:
		return ""
	default:
		return ""
	}
}
