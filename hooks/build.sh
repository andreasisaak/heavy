#!/bin/sh
# Puts bin/heavy and, on a Mac, bin/heavy-linux in place when they are missing or, at a session or monitor start,
# when the sources changed (design G2), then runs the hook of the mode with bin/heavy. Every hook and the monitor
# start through this file. bin/heavy-linux is the helper for the Docker VM of a Mac; on Linux docker needs no VM, so
# only bin/heavy is there. It first looks for the binaries GitHub built for exactly these sources, the release
# build-<stamp>, and takes them when their checksums match, so a machine needs no Go; else it builds with the local
# Go. The stamp is the SHA-256 over every Go file, go.mod and go.sum, kept in bin/.stamp. A lock file with the PID
# of its holder serializes sessions that start at once; a lock whose build died is taken over.
# The binaries are written under a temporary name of this process and moved into place, so no hook runs a
# half-written one, even when two builds overlap after a takeover. The monitor, which runs the whole session, first
# waits for a build that holds the lock, so it starts the new binary and not the one being replaced (design G9).
# A failed build runs the binary of the last build; without one, a PreToolUse hook ends with exit 2, which blocks
# the tool call (design G15).

# checksums says whether a program for the stamp is there, and what is missing when not.
checksums() {
	if command -v shasum >/dev/null 2>&1 || command -v sha256sum >/dev/null 2>&1; then
		return 0
	fi
	echo "heavy: found neither shasum nor sha256sum on PATH; install one and start the session again" >&2
	return 1
}

# stamp prints the SHA-256 over the Go sources of the plugin, with shasum on a Mac and sha256sum of coreutils
# elsewhere. Both print the same lines, so the stamp is the same with either.
stamp() {
	if command -v shasum >/dev/null 2>&1; then
		sum="shasum -a 256"
	else
		sum=sha256sum
	fi
	(cd "$1" && { find . -path ./.git -prune -o -name '*.go' -type f -print; echo ./go.mod; echo ./go.sum; } |
		LC_ALL=C sort | xargs $sum | $sum | cut -d ' ' -f 1)
}

# sha256 prints the SHA-256 of one file.
sha256() {
	if command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | cut -d ' ' -f 1
	else
		sha256sum "$1" | cut -d ' ' -f 1
	fi
}

# architecture prints the name Go has for the processor of this machine.
architecture() {
	case $(uname -m) in
	arm64 | aarch64) echo arm64 ;;
	x86_64 | amd64) echo amd64 ;;
	*) uname -m ;;
	esac
}

# take downloads one file of the release into bin/ under a temporary name and checks it against the checksums.
take() {
	curl -fsSL --connect-timeout 5 --max-time 60 -o "$1/bin/$3.new.$$" "$2/$3" 2>/dev/null || return 1
	expected=$(sed -n "s/^\([0-9a-f]*\)  $3\$/\1/p" "$1/bin/checksums.new.$$")
	if [ -z "$expected" ] || [ "$expected" != "$(sha256 "$1/bin/$3.new.$$")" ]; then
		echo "heavy: the download of $3 does not match its checksum, so heavy does not use it" >&2
		return 1
	fi
	chmod +x "$1/bin/$3.new.$$"
}

# download takes the binaries of the release build-<stamp> for this machine: on a Mac the build for macOS and, as
# bin/heavy-linux, the one for Linux; on Linux the build for Linux alone. Without curl, without a release for these
# sources or with a file that does not match its checksum it takes nothing and returns 1.
download() {
	command -v curl >/dev/null 2>&1 || return 1
	base=${HEAVY_RELEASES:-https://github.com/andreasisaak/heavy/releases/download}/build-$2
	machine=$(architecture)
	host=heavy-linux-$machine
	helper=
	if vm; then
		host=heavy-darwin-$machine
		helper=heavy-linux-$machine
	fi
	taken=1
	if curl -fsSL --connect-timeout 5 --max-time 20 -o "$1/bin/checksums.new.$$" "$base/checksums.txt" 2>/dev/null &&
		take "$1" "$base" "$host" && { [ -z "$helper" ] || take "$1" "$base" "$helper"; }; then
		mv -f "$1/bin/$host.new.$$" "$1/bin/heavy" && { [ -z "$helper" ] || mv -f "$1/bin/$helper.new.$$" "$1/bin/heavy-linux"; } &&
			echo "$2" >"$1/bin/.stamp" && taken=0
	fi
	/bin/rm -f "$1/bin/checksums.new.$$" "$1/bin/$host.new.$$" "$1/bin/heavy-linux-$machine.new.$$"
	[ "$taken" -eq 0 ] || return 1
	if [ -n "$helper" ]; then
		echo "heavy: downloaded bin/heavy and bin/heavy-linux for ${host#heavy-} from the release build-$2" >&2
	else
		echo "heavy: downloaded bin/heavy for ${host#heavy-} from the release build-$2" >&2
	fi
}

# vm says whether docker runs in a VM on this system, which then needs bin/heavy-linux as its helper: everywhere
# but on Linux.
vm() {
	[ "$(uname -s)" != Linux ]
}

# built says whether every binary this system needs is there; it asks for the system only without bin/heavy-linux,
# so a hook on a Mac starts no program for it.
built() {
	[ -x "$1/bin/heavy" ] || return 1
	[ -x "$1/bin/heavy-linux" ] || ! vm
}

# needed says whether the binaries must be built: one is missing, or at a session start or a monitor start the
# stamp differs; the monitor may start before the SessionStart hook takes the lock.
needed() {
	built "$1" || return 0
	case $2 in
	session-start | monitor) ;;
	*) return 1 ;;
	esac
	[ "$(stamp "$1")" != "$(cat "$1/bin/.stamp" 2>/dev/null)" ]
}

# The build lock is the file <lock>/pid with the PID of its holder. The shell creates it itself, with noclobber,
# which opens the file exclusively: no two sessions get it. The folder around it is no lock: on the Ubuntu runner
# of the CI two sessions that started at once both got a zero status from mkdir of the same folder (trace of
# 2026-11-11), so nothing here depends on mkdir failing.

# stale says whether the build lock has no living holder: its holder died, or its file named none for more than
# three calls since the caller set empty=0. kill -0 does not reach the build of another user on the same machine;
# its entry in /proc shows that it lives.
stale() {
	holder=$(cat "$1/pid" 2>/dev/null)
	if [ -n "$holder" ]; then
		if kill -0 "$holder" 2>/dev/null || [ -d "/proc/$holder" ]; then
			return 1
		fi
		return 0
	fi
	empty=$((empty + 1))
	[ "$empty" -gt 3 ]
}

# claim creates the lock file with the PID of this shell and fails when the file exists.
claim() {
	mkdir -p "$1" 2>/dev/null || return 1
	(
		set -C
		echo $$ >"$1/pid"
	) 2>/dev/null
}

# lock waits for the build lock and takes it; a lock without a living holder is removed, with a note that names
# the holder.
lock() {
	empty=0
	until claim "$1"; do
		if stale "$1"; then
			echo "heavy: the build lock names process ${holder:-none}, which does not run; taking the lock over" >&2
			/bin/rm -f "$1/pid" && continue
		fi
		sleep 1
	done
}

# unlock gives the build lock back: it removes the lock file, and the folder when no other session has taken the
# lock in it meanwhile.
unlock() {
	/bin/rm -f "$1/pid"
	rmdir "$1" 2>/dev/null
	return 0
}

# settle waits while a build holds the lock, so the monitor starts the binary that build writes and not the one
# it replaces; a lock without a living holder does not hold it up.
settle() {
	empty=0
	while [ -e "$1/pid" ] && ! stale "$1"; do
		sleep 1
	done
}

# build puts the binaries in place under the lock, unless a build that held the lock has done it meanwhile: the
# download of the release for these sources, else a build with the local Go.
build() {
	root=$1
	checksums || return 1
	mkdir -p "$root/bin" || return 1
	lock "$root/bin/.build.lock"
	status=0
	if needed "$root" "$2"; then
		fresh=$(stamp "$root")
		if download "$root" "$fresh"; then
			:
		elif ! command -v go >/dev/null 2>&1; then
			echo "heavy: found no build of these sources to download and no go on PATH; wait a few minutes for the build on GitHub, or install Go 1.27 (https://go.dev/dl/, on a Mac: brew install go), and start the session again" >&2
			status=1
		elif vm; then
			echo "heavy: building bin/heavy and bin/heavy-linux with $(command -v go)" >&2
			(cd "$root" &&
				go build -trimpath -o "bin/heavy.new.$$" ./cmd/heavy &&
				GOOS=linux CGO_ENABLED=0 go build -trimpath -o "bin/heavy-linux.new.$$" ./cmd/heavy &&
				mv -f "bin/heavy.new.$$" bin/heavy && mv -f "bin/heavy-linux.new.$$" bin/heavy-linux &&
				echo "$fresh" >bin/.stamp) || status=2
		else
			echo "heavy: building bin/heavy with $(command -v go)" >&2
			(cd "$root" &&
				go build -trimpath -o "bin/heavy.new.$$" ./cmd/heavy &&
				mv -f "bin/heavy.new.$$" bin/heavy &&
				echo "$fresh" >bin/.stamp) || status=2
		fi
		if [ "$status" -eq 2 ]; then
			/bin/rm -f "$root/bin/heavy.new.$$" "$root/bin/heavy-linux.new.$$"
			echo "heavy: building heavy failed; the go output above says why" >&2
			status=1
		fi
	fi
	unlock "$root/bin/.build.lock"
	return "$status"
}

# start is the session start. Claude Code takes the environment of an interactive session as soon as it starts its
# monitors, not when the hooks end (probe 2026-11-08: a rebuild of 3 s left the session without the shims), so the
# shims and the session id go into CLAUDE_ENV_FILE before anything slow: with a binary its hook runs first and changed
# sources build after it; without one build.sh writes the two lines itself, builds, and the hook writes them again,
# which changes nothing. The hook registers the session under HEAVY_CLAUDE_PID, the parent of this shell, Claude Code:
# its own parent is this shell when it runs as a child.
start() {
	input=$(cat)
	if [ -x "$1/bin/heavy" ]; then
		printf '%s' "$input" | HEAVY_CLAUDE_PID=$PPID "$1/bin/heavy" --hook session-start
		hooked=$?
		if needed "$1" session-start && ! build "$1" session-start; then
			echo "heavy: running the bin/heavy of the last build meanwhile" >&2
		fi
		return "$hooked"
	fi
	environment "$1" "$input"
	build "$1" session-start || return 1
	printf '%s' "$input" | HEAVY_CLAUDE_PID=$PPID exec "$1/bin/heavy" --hook session-start
}

# environment writes the lines of the SessionStart hook to CLAUDE_ENV_FILE: the shims first on PATH and HEAVY_SESSION
# from the session_id of the hook input. Without either it writes nothing; the hook says why after the build.
environment() {
	[ -n "${CLAUDE_ENV_FILE-}" ] || return 0
	session=$(printf '%s' "$2" | sed -n 's/.*"session_id"[[:space:]]*:[[:space:]]*"\([A-Za-z0-9_-]*\)".*/\1/p' | head -n 1)
	[ -n "$session" ] || return 0
	shims=$(quote "$1/shims")
	printf 'case "$PATH" in %s:*) ;; *) export PATH=%s:"$PATH" ;; esac\nexport HEAVY_SESSION=%s\n' "$shims" "$shims" "$session" >>"$CLAUDE_ENV_FILE"
}

# quote quotes a word for sh like shell.Quote of the hook: unchanged when it holds only safe characters.
quote() {
	case $1 in
	*[!A-Za-z0-9@%+=:,./_-]*) printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")" ;;
	*) printf '%s' "$1" ;;
	esac
}

main() {
	mode=${1-}
	case $mode in
	session-start | guard | stop | monitor | stamp) ;;
	*)
		echo "heavy: build.sh takes the mode session-start, guard, stop, monitor or stamp, got $mode" >&2
		return 64
		;;
	esac
	root=$(cd "$(dirname "$0")/.." && pwd -P) || return 1
	if [ "$mode" = stamp ]; then
		# The stamp of the sources, as the release of their build is named: build-<stamp>.
		checksums && stamp "$root"
		return
	fi
	if [ "$mode" = session-start ]; then
		start "$root"
		return
	fi
	if [ "$mode" = monitor ]; then
		settle "$root/bin/.build.lock"
	fi
	if needed "$root" "$mode" && ! build "$root" "$mode"; then
		if [ ! -x "$root/bin/heavy" ]; then
			case $mode in
			guard | stop) return 2 ;;
			esac
			return 1
		fi
		echo "heavy: running the bin/heavy of the last build meanwhile" >&2
	fi
	if [ "$mode" = monitor ]; then
		exec "$root/bin/heavy" --monitor
	fi
	exec "$root/bin/heavy" --hook "$mode"
}

# One line, parsed in full before main starts: editing this file during a run cannot change the run.
main "$@"; exit
