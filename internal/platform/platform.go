package platform

import (
	"fmt"
	"os/exec"
	"strings"
)

func ShellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func QuoteCommand(args []string) string {
	q := make([]string, len(args))
	for i, s := range args {
		q[i] = ShellQuote(s)
	}
	return strings.Join(q, " ")
}
func RunSignalMode(args []string) bool { return signalMode(args) }
func CommandError(name string, err error, output []byte) error {
	return fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(output)))
}
func StartHidden(cmd *exec.Cmd) { hide(cmd) }
