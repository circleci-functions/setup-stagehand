package chrome

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// NeedsHeadless reports whether Chrome has no display to open a window on: on
// Linux with neither X nor Wayland. Stagehand launches Chrome with a window
// unless the script asks for headless, which fails there.
func NeedsHeadless() bool {
	return runtime.GOOS == "linux" && os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == ""
}

// WriteHeadlessWrapper writes, at path, a script that runs chrome headless
// with whatever arguments it is given, so that a script launching Chrome with
// a window still works where there is no display. Chrome's headless mode is
// the full browser, extensions and all, which Stagehand needs.
func WriteHeadlessWrapper(path, chrome string) error {
	if filepath.Clean(path) == filepath.Clean(chrome) {
		return fmt.Errorf("the headless wrapper %s would run itself", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	script := "#!/bin/sh\n" +
		"# Written by setup-stagehand: there is no display, so Chrome runs headless.\n" +
		"exec " + shellQuote(chrome) + " --headless=new \"$@\"\n"
	return os.WriteFile(path, []byte(script), 0o755) //nolint:gosec // an executable script
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
