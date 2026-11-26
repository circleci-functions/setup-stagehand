package chrome

import (
	"os"
	"path/filepath"
	"runtime"
)

// Find returns the Chrome Stagehand would launch on its own: CHROME_PATH if
// it names one, then the places Stagehand looks. It returns ErrNotFound if
// there is none.
func Find() (string, error) {
	if p := os.Getenv("CHROME_PATH"); p != "" && IsExecutable(p) {
		return p, nil
	}
	for _, c := range candidates(runtime.GOOS, os.Getenv) {
		if IsExecutable(c) {
			return c, nil
		}
	}
	return "", ErrNotFound
}

// candidates are where Stagehand looks for Chrome when CHROME_PATH is not
// set, in its order.
func candidates(goos string, getenv func(string) string) []string {
	switch goos {
	case "darwin":
		return []string{
			"/Applications/Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary",
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Google Chrome Beta.app/Contents/MacOS/Google Chrome Beta",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
	case "windows":
		var out []string
		for _, root := range []string{getenv("LOCALAPPDATA"), getenv("PROGRAMFILES"), getenv("PROGRAMFILES(X86)")} {
			if root == "" {
				continue
			}
			out = append(out,
				filepath.Join(root, "Google", "Chrome SxS", "Application", "chrome.exe"),
				filepath.Join(root, "Google", "Chrome", "Application", "chrome.exe"))
		}
		return out
	case "linux":
		var out []string
		for _, name := range []string{"google-chrome-stable", "google-chrome", "chromium-browser", "chromium"} {
			for _, dir := range filepath.SplitList(getenv("PATH")) {
				if dir != "" {
					out = append(out, filepath.Join(dir, name))
				}
			}
		}
		return out
	}
	return nil
}
