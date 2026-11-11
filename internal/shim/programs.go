// Package shim is heavy started under the name of a program it shims: the net behind the guard for heavy calls
// that reach a program without heavy, because a variable, a script file or a make target hid them from the guard.
//
// The shims are links shims/<program> -> ../bin/heavy, and the SessionStart hook puts shims/ in front of PATH. A
// call reaches the shim with the arguments the shell has already expanded. The shim replaces itself with the real
// program, the first one on PATH that is no link to heavy, or for a heavy call with `heavy --directory <dir>
// <program> …`, which waits in the queue. Inside an active run the call goes straight through, and docker exec
// and run carry the run into the container.
package shim

// Programs are the programs heavy shims, sorted.
var Programs = []string{"bun", "bunx", "composer", "docker", "gmake", "make", "npm", "npx", "pnpm", "pnpx", "uv", "uvx", "yarn"}

// IsProgram reports whether heavy shims the program of that name.
func IsProgram(name string) bool {
	for _, program := range Programs {
		if program == name {
			return true
		}
	}
	return false
}
