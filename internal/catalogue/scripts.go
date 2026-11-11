package catalogue

import "regexp"

// packageScript is a script a launcher runs by name, with the arguments after the name.
type packageScript struct {
	launcher  string
	name      string
	arguments []string
}

var (
	heavyScript = regexp.MustCompile(`^(test|tests|build|typecheck|type-check|check|ci|e2e|coverage|lint|analyse|analyze|phpstan|psalm)([:_-].*)?$`)
	// neverEndingScript names runs that never end: `test:watch`, `test:ui`, `e2e:open`, `test-serve`.
	neverEndingScript = regexp.MustCompile(`(^|[:_-])(watch|serve|server|ui|open|start|preview)([:_-]|$)`)
)

// scriptRunner names a package script with a heavy name, such as `pnpm typecheck` or `npm run test:e2e`.
func scriptRunner(script packageScript) (string, bool) {
	if !heavyScript.MatchString(script.name) || neverEndingScript.MatchString(script.name) {
		return "", false
	}
	if !runs(script.launcher, script.arguments) || Targeted(script.arguments, nil) {
		return "", false
	}
	return script.launcher + " " + script.name, true
}
