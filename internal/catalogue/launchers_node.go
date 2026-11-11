package catalogue

var (
	pnpmValues   = set("-C", "--dir", "--filter", "-F", "--reporter", "--loglevel", "--workspace-concurrency")
	pnpmCommands = set(
		"install", "i", "add", "remove", "rm", "uninstall", "update", "up", "upgrade", "outdated", "list", "ls",
		"ll", "why", "link", "unlink", "import", "store", "config", "get", "set", "init", "create", "publish",
		"pack", "audit", "prune", "rebuild", "fetch", "patch", "patch-commit", "env", "setup", "dedupe",
		"licenses", "deploy", "root", "bin", "doctor", "approve-builds", "self-update", "help",
	)
	npmValues           = set("--prefix", "-w", "--workspace", "--loglevel")
	packageRunnerValues = set("-p", "--package", "-c", "--call")
	testCommands        = set("test", "t", "tst")
	yarnValues          = set("--cwd")
	yarnCommands        = set(
		"install", "add", "remove", "upgrade", "up", "info", "init", "link", "unlink", "why", "config", "cache",
		"global", "outdated", "pack", "publish", "set", "plugin", "npm", "constraints", "dedupe", "explain",
		"node", "patch", "rebuild", "search", "version", "workspaces", "help",
	)
	bunValues   = set("--cwd", "--filter")
	bunCommands = set(
		"install", "i", "add", "a", "remove", "rm", "update", "outdated", "link", "unlink", "pm", "init", "create",
		"upgrade", "build", "repl", "help", "publish", "patch", "audit", "info",
	)
)

func pnpm(arguments []string) []target {
	rest := SkipOptions(arguments, pnpmValues)
	if len(rest) == 0 {
		return nil
	}
	command, rest := rest[0], rest[1:]
	switch {
	case command == "exec" || command == "dlx":
		return []target{{command: SkipOptions(rest, packageRunnerValues)}}
	case command == "run" || command == "run-script":
		return scriptTarget("pnpm", SkipOptions(rest, pnpmValues))
	case testCommands[command]:
		return []target{{script: &packageScript{launcher: "pnpm", name: "test", arguments: rest}}}
	case pnpmCommands[command]:
		return nil
	}
	return scriptOrCommand("pnpm", command, rest)
}

func npm(arguments []string) []target {
	rest := SkipOptions(arguments, npmValues)
	if len(rest) == 0 {
		return nil
	}
	command, rest := rest[0], rest[1:]
	switch {
	case testCommands[command]:
		return []target{{script: &packageScript{launcher: "npm", name: "test", arguments: rest}}}
	case command == "run" || command == "run-script" || command == "rum" || command == "urn":
		return scriptTarget("npm", SkipOptions(rest, npmValues))
	case command == "exec" || command == "x":
		return []target{{command: SkipOptions(rest, packageRunnerValues)}}
	}
	return nil
}

// packageRunner is npx, pnpx and bunx: they start the command after their options.
func packageRunner(arguments []string) []target {
	return []target{{command: SkipOptions(arguments, packageRunnerValues)}}
}

func yarn(arguments []string) []target {
	rest := SkipOptions(arguments, yarnValues)
	if len(rest) == 0 {
		return nil
	}
	command, rest := rest[0], rest[1:]
	switch {
	case command == "run":
		return scriptTarget("yarn", rest)
	case command == "exec" || command == "dlx":
		return []target{{command: SkipOptions(rest, packageRunnerValues)}}
	case command == "workspace":
		if len(rest) == 0 {
			return nil
		}
		return yarn(rest[1:])
	case yarnCommands[command]:
		return nil
	}
	return scriptOrCommand("yarn", command, rest)
}

func bun(arguments []string) []target {
	rest := SkipOptions(arguments, bunValues)
	if len(rest) == 0 {
		return nil
	}
	command, rest := rest[0], rest[1:]
	switch {
	case command == "run":
		return scriptTarget("bun", SkipOptions(rest, bunValues))
	case command == "x":
		return []target{{command: SkipOptions(rest, packageRunnerValues)}}
	case bunCommands[command]:
		return nil
	}
	return scriptOrCommand("bun", command, rest)
}

// scriptOrCommand is a word a package manager runs as a script of that name, or else as a program.
func scriptOrCommand(launcher, command string, rest []string) []target {
	return []target{
		{script: &packageScript{launcher: launcher, name: command, arguments: rest}},
		{command: append([]string{command}, rest...)},
	}
}
