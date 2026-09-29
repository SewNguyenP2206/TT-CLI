package shell

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const marker = "# tt heartbeat hook"

func Install(shellName string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil { return "", err }
	var rc, hook string
	switch strings.ToLower(filepath.Base(shellName)) {
	case "bash":
		rc = filepath.Join(home, ".bashrc")
		hook = `# tt heartbeat hook
__tt_beat() {
  local now
  now=$(date +%s 2>/dev/null) || return 0
	if [ -z "${TT_LAST_BEAT:-}" ] || [ "$((now - TT_LAST_BEAT))" -ge 30 ]; then
		TT_LAST_BEAT=$now
		tt beat --source shell >/dev/null 2>&1 &
  fi
}
PROMPT_COMMAND="${PROMPT_COMMAND:+$PROMPT_COMMAND; }__tt_beat"
`
	case "zsh":
		rc = filepath.Join(home, ".zshrc")
		hook = `# tt heartbeat hook
function __tt_beat() {
  local now=$(date +%s 2>/dev/null) || return 0
	if [[ -z "${TT_LAST_BEAT:-}" || $((now - TT_LAST_BEAT)) -ge 30 ]]; then
		TT_LAST_BEAT=$now
		tt beat --source shell >/dev/null 2>&1 &!
  fi
}
precmd_functions+=(__tt_beat)
`
	case "fish":
		rc = filepath.Join(home, ".config", "fish", "config.fish")
		hook = `# tt heartbeat hook
function __tt_beat --on-event fish_prompt
    set -l now (date +%s 2>/dev/null)
	if test -z "$TT_LAST_BEAT"; or test (math $now - $TT_LAST_BEAT) -ge 30
		set -g TT_LAST_BEAT $now
		tt beat --source shell >/dev/null 2>&1 &
    end
end
`
	default:
		return "", fmt.Errorf("shell belum didukung: %s (gunakan bash, zsh, atau fish)", shellName)
	}
	if err := os.MkdirAll(filepath.Dir(rc), 0700); err != nil { return "", err }
	current, err := os.ReadFile(rc)
	if err == nil && strings.Contains(string(current), marker) { return rc, nil }
	file, err := os.OpenFile(rc, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil { return "", err }
	defer file.Close()
	if _, err := fmt.Fprintln(file, "\n"+hook); err != nil { return "", err }
	return rc, nil
}