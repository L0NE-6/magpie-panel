package sessions

// Carrying a session on in another agent (#845): only where that agent reads
// the session's own file as it is, so nothing is converted, and the file is
// left as it was.
//
// oh-my-pi grew from Pi and reads Pi's sessions: `omp --fork <file>` loads a
// Pi session (its version-3 entries, messages, tool calls and results,
// thinking; the model from its assistant messages, as omp names models
// provider/id) and writes a new omp session from it, the Pi file untouched.
// The other way doesn't work: Pi opens a file only when its first line is
// the session header, and omp's first line is its title. Claude Code's and
// Codex's sessions are converted for each other (carry_convert.go).

import (
	"os"
	"runtime"
	"strings"

	"github.com/yetone/magpie/internal/proc"
)

// Carry is another agent a session can be carried on in, and the shell
// line that does it in the session's folder. A Convert one has no line
// yet: the session is converted into a new one of that agent's when the
// user asks (CarryOver, carry_convert.go), and the line opens that.
type Carry struct {
	Agent   string `json:"agent"`
	Command string `json:"command,omitempty"`
	Convert bool   `json:"convert,omitempty"`
}

// ompThere says whether oh-my-pi is installed: its command, or its folder.
// A var so tests can say.
var ompThere = func() bool {
	if _, err := proc.Look("omp"); err == nil {
		return true
	}
	_, err := os.Stat(OmpDir())
	return err == nil
}

// carries are the other agents a session can be carried on in.
func carries(s Session) []Carry {
	if s.ReadOnly || s.WSL != "" || s.Path == "" || s.Cwd == "" {
		return nil
	}
	switch s.Agent {
	case "pi":
		if !ompThere() {
			return nil
		}
		return []Carry{{Agent: "omp", Command: inFolder(s.Cwd, "omp --fork "+argQuote(s.Path))}}
	}
	if to := convertsTo[s.Agent]; to != "" && agentThere(to) {
		return []Carry{{Agent: to, Convert: true}}
	}
	return nil
}

// inFolder is a shell line running run in the folder cwd.
func inFolder(cwd, run string) string {
	if runtime.GOOS == "windows" {
		return "Set-Location -LiteralPath " + argQuote(cwd) + "; " + run
	}
	return "cd " + argQuote(cwd) + " && " + run
}

// powerShellQuote is s as one single-quoted PowerShell argument: wrapped in
// '...', with every quote character in it doubled. ASCII ' is not the only
// one PowerShell's parser takes as a single quote: U+2018, U+2019, U+201A and
// U+201B are too (#8048), so a cwd or argument holding one - a cloned
// third-party repository's name, say - would end the quoted string there and
// have what follows read as code. Each of them is escaped by doubling that
// same character, as ' is.
func powerShellQuote(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('\'')
	for _, r := range s {
		switch r {
		case '\'', '\u2018', '\u2019', '\u201a', '\u201b':
			b.WriteRune(r) // the pair
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('\'')
	return b.String()
}

// argQuote is s as one argument of this system's shell.
func argQuote(s string) string {
	if runtime.GOOS == "windows" {
		return powerShellQuote(s)
	}
	return shellQuote(s)
}
