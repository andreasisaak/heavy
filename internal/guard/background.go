package guard

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/andreasisaak/heavy/internal/shell"
	"mvdan.cc/sh/v3/syntax"
)

// Background call forms that hooks keeping background output visible rewrite, port of hooks/background_forms.py.
//
// Such a hook (for example a personal PreToolUse guard that turns `cmd > file` into `cmd 2>&1 | tee file`) builds
// its rewrite from the original command. When the heavy guard rewrites the same call, Claude Code runs whichever
// rewrite finishes last, and `heavy` can go missing. The guard therefore denies a background heavy call in the
// three forms these hooks rewrite: stdout into a file, a tee target in a directory that does not exist yet, and a
// trailing `; echo "name $?"` without `exit`.

// conflictingForm is why another hook would rewrite this background call, "" when such hooks leave it alone.
func conflictingForm(current call, cwd string) string {
	if writesStdoutToFile(current) {
		return "stdout goes to a file"
	}
	if target, found := teeTargetWithoutDirectory(current.script, cwd); found {
		return "the directory of the tee target " + target + " does not exist"
	}
	if endsWithStatusEcho(current) {
		return "a trailing echo of $? does not end with exit"
	}
	return ""
}

// writesStdoutToFile reports a redirect of stdout into a file other than /dev/null, outside substitutions.
func writesStdoutToFile(current call) bool {
	found := false
	walkOutsideSubstitutions(current.file, func(node syntax.Node) bool {
		redirect, isRedirect := node.(*syntax.Redirect)
		if !isRedirect || redirect.Word == nil || (redirect.N != nil && redirect.N.Value != "1") {
			return !found
		}
		target := sourceOf(current.source, redirect.Word)
		switch {
		case target == "/dev/null":
		case redirect.Op == syntax.RdrOut, redirect.Op == syntax.AppOut, redirect.Op == syntax.RdrClob,
			redirect.Op == syntax.RdrAll, redirect.Op == syntax.AppAll:
			found = true
		case redirect.Op == syntax.DplOut:
			found = !digits(target)
		}
		return !found
	})
	return found
}

// teeTargetWithoutDirectory is the first tee file whose directory neither exists nor comes from an earlier mkdir;
// a path the shell still expands ($, ~user) stays unchecked.
func teeTargetWithoutDirectory(script shell.Script, cwd string) (string, bool) {
	var created []string
	for _, command := range script.Commands {
		switch filepath.Base(command.Argv[0]) {
		case "mkdir":
			for _, argument := range command.Argv[1:] {
				if !strings.HasPrefix(argument, "-") {
					created = append(created, absolute(argument, cwd))
				}
			}
		case "tee":
			for _, target := range command.Argv[1:] {
				if strings.HasPrefix(target, "-") || strings.Contains(target, "$") || (strings.HasPrefix(target, "~") && !homePath(target)) {
					continue
				}
				directory := filepath.Dir(absolute(target, cwd))
				if !isDirectory(directory) && !madeBy(created, directory) {
					return target, true
				}
			}
		}
	}
	return "", false
}

// endsWithStatusEcho reports a call that ends in `; echo … $? …`, whose status a hook would pass on with exit.
func endsWithStatusEcho(current call) bool {
	statements := current.file.Stmts
	if len(statements) < 2 {
		return false
	}
	previous, last := statements[len(statements)-2], statements[len(statements)-1]
	if !previous.Semicolon.IsValid() || previous.Background || current.source[previous.Semicolon.Offset()] != ';' {
		return false
	}
	if last.Semicolon.IsValid() || last.Background || last.Negated || len(last.Redirs) > 0 {
		return false
	}
	echo, isCall := last.Cmd.(*syntax.CallExpr)
	if !isCall || len(echo.Assigns) > 0 || len(echo.Args) == 0 || sourceOf(current.source, echo.Args[0]) != "echo" {
		return false
	}
	for _, word := range echo.Args[1:] {
		text := sourceOf(current.source, word)
		if strings.Contains(text, "$?") && !strings.Contains(text, "'") {
			return true
		}
	}
	return false
}

func digits(word string) bool {
	for _, character := range word {
		if character < '0' || character > '9' {
			return false
		}
	}
	return word != ""
}

func homePath(path string) bool {
	return path == "~" || strings.HasPrefix(path, "~/")
}

// absolute is the path from cwd, with ~ and ~/ for the home directory, as os.path.expanduser and normpath did.
func absolute(path, cwd string) string {
	if homePath(path) {
		path = os.Getenv("HOME") + path[1:]
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(cwd, path)
}

func isDirectory(path string) bool {
	information, err := os.Stat(path)
	return err == nil && information.IsDir()
}

func madeBy(created []string, directory string) bool {
	for _, made := range created {
		if directory == made || strings.HasPrefix(directory, made+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
