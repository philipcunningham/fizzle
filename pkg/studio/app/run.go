package app

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/philipcunningham/fizzle/pkg/logger"
)

// Run launches the studio TUI with directory as its workspace
// root. If directory is empty, the current working directory is
// used. studio is workspace-oriented: it browses a directory of
// `.img` / `.fzf` / `.fzv` / `.wav` files. To open a specific
// file, point studio at the directory that contains it and select
// the file in the Workspace browser.
//
// A non-existent directory returns an error; a path that points
// at a file (rather than a directory) also returns an error.
func Run(directory string) error {
	workspace, err := resolveWorkspaceDir(directory)
	if err != nil {
		return err
	}
	// Silence library logging for the lifetime of the TUI. Otherwise a
	// stray log.Info() from a library call (e.g. diskformat.Format on
	// the new-disk save path) writes to stderr and bleeds onto the
	// rendered screen, since the TUI owns the terminal (F-A).
	restoreLog := logger.Silence()
	defer restoreLog()

	app := New(workspace)
	p := tea.NewProgram(app)
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("studio: %w", err)
	}
	return nil
}

// resolveWorkspaceDir normalises directory: empty means cwd; a
// real directory is returned as-is; anything else (missing path,
// or a path to a file) is an error so the user gets a clear
// message rather than dropping into a half-loaded TUI.
func resolveWorkspaceDir(directory string) (string, error) {
	if directory == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("studio: getwd: %w", err)
		}
		return wd, nil
	}
	info, statErr := os.Stat(directory)
	if statErr != nil {
		return "", fmt.Errorf("studio: %w", statErr)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("studio: %s is not a directory; studio browses a workspace of files, point it at the parent directory", directory)
	}
	return directory, nil
}
