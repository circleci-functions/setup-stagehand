package chrome

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// unzip extracts a zip into dir, keeping file modes and symbolic links: the
// macOS build is an app bundle whose frameworks are linked, and the Linux one
// runs only if its executables keep their mode.
func unzip(file, dir string) error {
	r, err := zip.OpenReader(file)
	if err != nil {
		return err
	}
	defer r.Close()
	root, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	for _, f := range r.File {
		if err := extract(f, root); err != nil {
			return fmt.Errorf("%s: %w", f.Name, err)
		}
	}
	return nil
}

func extract(f *zip.File, root string) error {
	path := filepath.Join(root, filepath.FromSlash(f.Name)) //nolint:gosec // checked against root below
	if !within(root, path) {
		return fmt.Errorf("outside the archive's root")
	}
	mode := f.Mode()
	switch {
	case mode.IsDir():
		return os.MkdirAll(path, 0o755) //nolint:gosec // a browser's files are not secret
	case mode&os.ModeSymlink != 0:
		target, err := readAll(f)
		if err != nil {
			return err
		}
		resolved := filepath.Join(filepath.Dir(path), filepath.FromSlash(string(target)))
		if filepath.IsAbs(string(target)) || !within(root, resolved) {
			return fmt.Errorf("link to %q points outside the archive", target)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // as above
			return err
		}
		return os.Symlink(string(target), path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // as above
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	perm := mode.Perm() | 0o600
	out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm) //nolint:gosec // checked to be under root
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil { //nolint:gosec // a published browser build, not untrusted input
		_ = out.Close()
		return err
	}
	return out.Close()
}

func readAll(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, 4096))
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
