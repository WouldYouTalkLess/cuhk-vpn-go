// Package appdir locates writable data beside the desktop application.
package appdir

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func Directory() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	p, err = filepath.EvalSymlinks(p)
	if err != nil {
		return "", err
	}
	d := filepath.Dir(p)
	// Keep mutable data outside a signed macOS application bundle.
	if runtime.GOOS == "darwin" && filepath.Base(d) == "MacOS" && filepath.Base(filepath.Dir(d)) == "Contents" && strings.HasSuffix(filepath.Dir(filepath.Dir(d)), ".app") {
		d = filepath.Dir(filepath.Dir(filepath.Dir(d)))
	}
	return d, nil
}
