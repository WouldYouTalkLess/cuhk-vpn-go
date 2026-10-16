// corefetch exercises the same download/verification/extraction code as the GUI.
// It never connects a VPN or runs a package installer.
package main

import (
	"context"
	"cuhk-vpn-go/internal/core"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

func main() {
	goos := flag.String("os", runtime.GOOS, "target operating system")
	arch := flag.String("arch", runtime.GOARCH, "target architecture")
	base := flag.String("dir", "", "application directory (default: this executable's directory)")
	flag.Parse()
	var e error
	if *base == "" {
		*base, e = core.ApplicationDir()
	} else {
		*base, e = filepath.Abs(*base)
	}
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	ctx, c := context.WithTimeout(context.Background(), 20*time.Minute)
	defer c()
	dir, e := (core.Manager{Base: *base, OS: *goos, Arch: *arch}).Ensure(ctx, func(s string) { fmt.Fprintln(os.Stderr, s) })
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	json.NewEncoder(os.Stdout).Encode(map[string]string{"os": *goos, "arch": *arch, "directory": dir})
}
