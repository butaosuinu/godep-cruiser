#!/bin/sh

set -u

fail() {
	printf '%s\n' "[agent-hook] $*" >&2
	exit 2
}

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P) || fail "failed to resolve the hook directory"
root=$(CDPATH= cd -- "$script_dir/.." && pwd -P) || fail "failed to resolve the project root"

case ${1-} in
	pre-push | format)
		mode=$1
		;;
	*)
		fail "expected mode pre-push or format"
		;;
esac

export AGENT_HOOK_PROJECT_ROOT="$root"
export GOCACHE="$root/.cache/go-build"
export GOMODCACHE="$root/.cache/go-mod"

tool_dir=$root/.cache/tools
mkdir -p "$tool_dir" || fail "failed to create the hook tool cache"

temporary=$tool_dir/agent-hook.$$
trap 'rm -f "$temporary"' 0 1 2 15
go -C "$root" build -o "$temporary" ./tools/agent-hook || fail "failed to build the hook helper"

"$temporary" "$mode"
status=$?
rm -f "$temporary"
trap - 0 1 2 15

case $status in
	0 | 2)
		exit "$status"
		;;
	*)
		fail "helper failed with unexpected exit status $status"
		;;
esac
