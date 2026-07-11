package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestContainsGitPush(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		command string
		want    bool
		wantErr bool
	}{
		{name: "direct push", command: "git push", want: true},
		{name: "absolute git", command: "/usr/bin/git push origin main", want: true},
		{name: "environment assignment", command: "CI=1 git push", want: true},
		{name: "env wrapper", command: "env -i TOKEN=value git push", want: true},
		{name: "command wrapper", command: "command -- git push", want: true},
		{name: "nested wrappers", command: "command env TOKEN=value /opt/bin/git push", want: true},
		{name: "git directory option", command: "git -C ../repo push", want: true},
		{name: "git global options", command: "git --no-pager -c push.default=current push", want: true},
		{name: "git inline push alias", command: "git -c alias.p='push --dry-run' p", want: true},
		{name: "git shell push alias", command: "git -c alias.p='!git push --dry-run' p", want: true},
		{name: "git chained push alias", command: "git -c alias.p=publish -c alias.publish=push p", want: true},
		{name: "git option prefixed chained push alias", command: "git -c alias.p='-p q' -c alias.q=push p", want: true},
		{name: "git alias defines chained push alias", command: "git -c alias.p='-c alias.q=push q' p", want: true},
		{name: "git alias uses invocation as subcommand", command: "git -c alias.p=-p p push", want: true},
		{name: "git case insensitive push alias", command: "git -c AlIaS.P=push P", want: true},
		{name: "git config env push alias", command: "git --config-env=alias.p=ALIAS_VALUE p", want: true},
		{name: "git process environment push alias", command: "GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=alias.p GIT_CONFIG_VALUE_0=push git p --dry-run", want: true},
		{name: "git env wrapper push alias", command: "env GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=alias.p GIT_CONFIG_VALUE_0=push git p", want: true},
		{name: "git shell alias invocation push", command: "git -c alias.p='!git' p push --dry-run", want: true},
		{name: "git shell alias inherited inline alias push", command: "git -c alias.p='!git' -c alias.q=push p q", want: true},
		{name: "git shell alias inherited environment alias push", command: "GIT_CONFIG_COUNT=2 GIT_CONFIG_KEY_0=alias.p GIT_CONFIG_VALUE_0='!git' GIT_CONFIG_KEY_1=alias.q GIT_CONFIG_VALUE_1=push git p q", want: true},
		{name: "git dynamic environment alias overrides prior alias", command: "KEY=alias.p; GIT_CONFIG_COUNT=2 GIT_CONFIG_KEY_0=alias.p GIT_CONFIG_VALUE_0='!echo' GIT_CONFIG_KEY_1=$KEY GIT_CONFIG_VALUE_1=push git p", want: true},
		{name: "git dynamic push alias", command: `git -c alias.p="$ALIAS_VALUE" p`, want: true},
		{name: "attached git option", command: "git --git-dir=/tmp/repo/.git push", want: true},
		{name: "and compound command", command: "make test && git push", want: true},
		{name: "semicolon compound command", command: "git status; git push", want: true},
		{name: "newline compound command", command: "git status\ngit push", want: true},
		{name: "leading file descriptor redirect", command: "2>/dev/null git push", want: true},
		{name: "git option area file descriptor redirect", command: "git 2>/dev/null push", want: true},
		{name: "trailing file descriptor redirect", command: "git push 2>/dev/null", want: true},
		{name: "command substitution redirect operand", command: "git >$(printf /dev/null) push --dry-run", want: true},
		{name: "process substitution redirect operand", command: "git > >(cat) push --dry-run", want: true},
		{name: "backtick redirect operand", command: "git >`printf /dev/null` push --dry-run", want: true},
		{name: "parameter redirect operand", command: `git >"$OUTPUT" push --dry-run`, want: true},
		{name: "brace group", command: "{ git status; git push; }", want: true},
		{name: "conditional", command: "if git status; then git push; fi", want: true},
		{name: "command substitution", command: "echo $(git push)", want: true},
		{name: "command substitution comment parenthesis", command: "echo \"$(echo ok # )\ngit push)\"", want: true},
		{name: "backtick substitution", command: "echo `git push`", want: true},
		{name: "quoted command substitution", command: `echo "$(git push)"`, want: true},
		{name: "quoted backtick substitution", command: "echo \"`git push`\"", want: true},
		{name: "bash command string", command: "bash -c 'git push'", want: true},
		{name: "bash command string option separator", command: "bash -c -- 'git push'", want: true},
		{name: "bash command string later option", command: "bash -c -e 'git push'", want: true},
		{name: "bash command string reset noexec option", command: "bash -c -n +n 'git push'", want: true},
		{name: "bash command string lone minus option", command: "bash -c - 'git push'", want: true},
		{name: "bash command string lone plus option", command: "bash -c + 'git push'", want: true},
		{name: "zsh login command string", command: `/bin/zsh -lc "GIT_SSH_COMMAND='ssh -F /dev/null' git push"`, want: true},
		{name: "environment wrapped shell", command: "env TOKEN=value bash -c 'git push'", want: true},
		{name: "shell here string script", command: "bash <<< 'git push'", want: true},
		{name: "shell explicit stdin heredoc script", command: "bash -s <<'EOF'\ngit push\nEOF\n", want: true},
		{name: "shell implicit stdin heredoc script", command: "bash <<EOF\ngit push\nEOF\n", want: true},
		{name: "shell stdin heredoc deferred substitution", command: "bash <<EOF\n\\$(git push)\nEOF\n", want: true},
		{name: "shell stdin heredoc nested deferred substitution", command: "bash -s <<EOF\necho \"\\\\$(git push)\"\nEOF\n", want: true},
		{name: "shell stdin heredoc nested deferred backtick", command: "bash -s <<EOF\necho \"\\\\`git push`\"\nEOF\n", want: true},
		{name: "shell stdin alternate zero descriptor", command: "bash -s 00<<< 'git push'", want: true},
		{name: "shell stdin duplicated descriptor", command: "bash -s 3<<EOF 0<&3\ngit push\nEOF\n", want: true},
		{name: "shell dash stdin script", command: "bash - <<EOF\ngit push\nEOF\n", want: true},
		{name: "shell dev stdin script", command: "bash /dev/stdin <<< 'git push'", want: true},
		{name: "shell alternate descriptor script", command: "bash /dev/fd/3 3<<EOF\ngit push\nEOF\n", want: true},
		{name: "shell arithmetic descriptor script", command: "bash /dev/fd/$((1+2)) 3<<EOF\ngit push\nEOF\n", want: true},
		{name: "shell process substitution script", command: "bash <(printf 'git push\\n')", want: true},
		{name: "shell arithmetic duplicated descriptor", command: "bash -s 3<<EOF 0<&$((1 + 2))\ngit push\nEOF\n", want: true},
		{name: "pipeline fed shell script", command: "printf 'git push\\n' | bash -s", want: true},
		{name: "pipeline fed wrapped shell script", command: "printf 'git status\\n' | env bash -s", want: true},
		{name: "multiline pipeline fed shell script", command: "printf x |\nbash -s", want: true},
		{name: "pipeline fed brace group shell script", command: "printf 'git push\\n' | { :; bash -s; }", want: true},
		{name: "pipeline fed subshell script", command: "printf 'git push\\n' | ( :; bash -s )", want: true},
		{name: "pipeline fed conditional shell script", command: "printf 'git push\\n' | if true; then bash -s; fi", want: true},
		{name: "pipeline fed loop shell script", command: "printf 'git push\\n' | while true; do bash -s; break; done", want: true},
		{name: "inner pipeline overrides group heredoc", command: "{ printf 'git push\\n' | bash -s; } <<'EOF'\ngit status\nEOF\n", want: true},
		{name: "brace group heredoc shell script", command: "{ :; bash -s; } <<'EOF'\ngit push\nEOF\n", want: true},
		{name: "subshell heredoc shell script", command: "( :; bash -s ) <<'EOF'\ngit push\nEOF\n", want: true},
		{name: "nested command string stdin script", command: "bash -c 'bash -s' <<'EOF'\ngit push\nEOF\n", want: true},
		{name: "nested command string pipeline script", command: "printf 'git push\\n' | bash -c 'bash -s'", want: true},
		{name: "nested command string descriptor script", command: "bash -c 'bash /dev/fd/3' 3<<'EOF'\ngit push\nEOF\n", want: true},
		{name: "split nested command string stdin script", command: "env -S 'bash -c \"bash -s\"' <<'EOF'\ngit push\nEOF\n", want: true},
		{name: "split command string option separator", command: "env -S 'bash -c -- \"git push\"'", want: true},
		{name: "split command string later option", command: "env -S 'bash -c -e \"git push\"'", want: true},
		{name: "shell rcfile descriptor script", command: "bash --noprofile --rcfile /dev/fd/3 -i 3<<'EOF' </dev/null\ngit push\nEOF\n", want: true},
		{name: "shell attached rcfile descriptor script", command: "bash --rcfile=/dev/fd/3 -i 3<<'EOF' </dev/null\ngit push\nEOF\n", want: true},
		{name: "wrapped shell stdin heredoc script", command: "env bash -s <<EOF\ngit push\nEOF\n", want: true},
		{name: "split wrapped shell stdin heredoc script", command: "env -S 'bash -s' <<EOF\ngit push\nEOF\n", want: true},
		{name: "evaluated command", command: "eval 'git push'", want: true},
		{name: "evaluated command option separator", command: "eval -- 'git push'", want: true},
		{name: "builtin evaluated command option separator", command: "builtin eval -- 'git push'", want: true},
		{name: "exec wrapped push", command: "exec git push", want: true},
		{name: "exec clear environment push", command: "exec -c git push", want: true},
		{name: "exec custom argv zero push", command: "exec -a git git push", want: true},
		{name: "exec clustered options push", command: "exec -cla name git push", want: true},
		{name: "exec attached argv zero push", command: "exec -agit git push", want: true},
		{name: "exec option separator push", command: "exec -- git push", want: true},
		{name: "builtin exec push", command: "builtin exec -c git push", want: true},
		{name: "command builtin exec push", command: "command builtin exec git push", want: true},
		{name: "nested builtin exec push", command: "builtin builtin exec git push", want: true},
		{name: "exit trap push", command: "trap 'git push' EXIT", want: true},
		{name: "numeric exit trap push", command: "trap 'git push' 0", want: true},
		{name: "trap with separator push", command: "trap -- 'git push' EXIT", want: true},
		{name: "trap hyphen action after separator push", command: "trap -- '-x; git push' EXIT", want: true},
		{name: "trap dynamic option push", command: "OPT=-; trap -$OPT 'git push' EXIT", want: true},
		{name: "command wrapped trap push", command: "command trap 'git push' EXIT", want: true},
		{name: "time wrapped trap push", command: "time trap 'git push' EXIT", want: true},
		{name: "builtin trap push", command: "builtin trap 'git push' EXIT", want: true},
		{name: "nested builtin trap push", command: "builtin builtin trap 'git push' EXIT", want: true},
		{name: "builtin separator trap push", command: "builtin -- trap 'git push' EXIT", want: true},
		{name: "command builtin trap push", command: "command builtin trap 'git push' EXIT", want: true},
		{name: "time builtin trap push", command: "time builtin trap 'git push' EXIT", want: true},
		{name: "external command trap push", command: "env command trap 'git push' EXIT", want: true},
		{name: "exec command trap push", command: "exec command trap 'git push' EXIT", want: true},
		{name: "deferred substitution trap push", command: "trap '$(git push)' EXIT", want: true},
		{name: "time wrapper", command: "time git push", want: true},
		{name: "time portability option", command: "time -p git push", want: true},
		{name: "external time option", command: "/usr/bin/time -l git push", want: true},
		{name: "external time output option", command: "/usr/bin/time -o report git push", want: true},
		{name: "external time clustered output option", command: "/usr/bin/time -lo /dev/null git push", want: true},
		{name: "environment wrapped external time", command: "env time -o report git push", want: true},
		{name: "command wrapped external time", command: "command time -o report git push", want: true},
		{name: "assignment prefixed external time", command: "FORMAT=short time -lo /dev/null git push", want: true},
		{name: "redirect prefixed external time", command: "2>/dev/null time -lo /dev/null git push", want: true},
		{name: "bash named option before command", command: "bash -o pipefail -c 'git push'", want: true},
		{name: "bash combined options before command", command: "bash -euo pipefail -c 'git push'", want: true},
		{name: "bash noexec option reset before command", command: "bash -n +n -c 'git push'", want: true},
		{name: "bash split combined options before command", command: "bash -eu -o pipefail -c 'git push'", want: true},
		{name: "bash combined option values before command", command: "bash -Oc extglob 'git push'", want: true},
		{name: "bash redirect before command string", command: "bash -c 2>/dev/null 'git push'", want: true},
		{name: "bash redirect among named options", command: "bash -o 2>/dev/null pipefail -c 'git push'", want: true},
		{name: "bash command substitution redirect", command: "bash -c >$(printf /dev/null) 'git push'", want: true},
		{name: "bash process substitution redirect", command: "bash -c > >(cat) 'git push'", want: true},
		{name: "bash backtick redirect", command: "bash -c >`printf /dev/null` 'git push'", want: true},
		{name: "env split string", command: "env -S 'git push origin main'", want: true},
		{name: "env clustered split string", command: "env -iS 'git push origin main'", want: true},
		{name: "env split string after redirect", command: "env -S 2>/dev/null 'git push'", want: true},
		{name: "env split executable and remaining argument", command: "env -S 'git' push", want: true},
		{name: "env attached split string", command: "env --split-string='git push'", want: true},
		{name: "env split string separator escape", command: `env -S 'git\_push'`, want: true},
		{name: "env split string options", command: "env -S '-i git push'", want: true},
		{name: "env split string path option", command: "env -S '-P /usr/bin git push'", want: true},
		{name: "env clustered unset option", command: "env -iu NAME git push", want: true},
		{name: "env split string stop escape", command: `env -S 'git push\c ignored'`, want: true},
		{name: "env split string expansion fails closed", command: `env -S 'git ${SUBCOMMAND}'`, want: true},
		{name: "ANSI-C quoted subcommand", command: "git $'push' --dry-run", want: true},
		{name: "ANSI-C hex escaped subcommand", command: `git $'pu\x73h' --dry-run`, want: true},
		{name: "ANSI-C concatenated subcommand", command: "git p$'ush' --dry-run", want: true},
		{name: "ANSI-C NUL truncated subcommand", command: `git $'push\0ignored' --dry-run`, want: true},
		{name: "ANSI-C hex NUL truncated subcommand", command: `git $'push\x00ignored' --dry-run`, want: true},
		{name: "ANSI-C Unicode NUL truncated subcommand", command: `git $'push\u0000ignored' --dry-run`, want: true},
		{name: "ANSI-C control NUL truncated subcommand", command: `git $'push\c@ignored' --dry-run`, want: true},
		{name: "ANSI-C NUL truncation with concatenation", command: `git $'pu\0ignored'sh --dry-run`, want: true},
		{name: "ANSI-C NUL truncated executable", command: `$'git\0ignored' push --dry-run`, want: true},
		{name: "nested wrapper composition", command: `time env -S 'bash -euo pipefail -c "git push"'`, want: true},
		{name: "command substitution in arithmetic expansion", command: "echo $(( $(git push) << 1 ))", want: true},
		{name: "dynamic git subcommand fails closed", command: "git $(printf push) --dry-run", want: true},
		{name: "backtick git subcommand fails closed", command: "git `printf push` --dry-run", want: true},
		{name: "dynamic git executable fails closed", command: "$(printf git) push --dry-run", want: true},
		{name: "dynamic parameter subcommand fails closed", command: `git "$SUBCOMMAND" --dry-run`, want: true},
		{name: "locale translated subcommand fails closed", command: `git $"push" --dry-run`, want: true},
		{name: "process substitution command is inspected", command: "echo >(git push)", want: true},
		{name: "brace expanded git subcommand fails closed", command: "git {pu,}sh --dry-run", want: true},
		{name: "brace expanded git choice fails closed", command: "git {push,status} --dry-run", want: true},
		{name: "brace expanded executable fails closed", command: "{git,} push --dry-run", want: true},
		{name: "globbed git subcommand fails closed", command: "git p* --dry-run", want: true},
		{name: "globbed git executable fails closed", command: "g?t push --dry-run", want: true},
		{name: "split git chdir option value fails closed", command: `git -C $VALUE status --dry-run`, want: true},
		{name: "split git config option value fails closed", command: `git -c $VALUE status --dry-run`, want: true},
		{name: "split env option value fails closed", command: `env -u $VALUE push --dry-run`, want: true},
		{name: "split time option value fails closed", command: `/usr/bin/time -o $VALUE push --dry-run`, want: true},
		{name: "split attached git option fails closed", command: `git --git-dir=$GIT_DIR status`, want: true},
		{name: "split attached env option fails closed", command: `env --chdir=$DIR git status`, want: true},
		{name: "split attached shell option fails closed", command: `bash --rcfile=$FILE -c "echo ok"`, want: true},
		{name: "non push git command", command: "git status", want: false},
		{name: "push is argument to another command", command: "echo git push", want: false},
		{name: "quoted command text", command: `echo "git push; still text"`, want: false},
		{name: "quoted separator", command: `printf '%s' 'text; git push'`, want: false},
		{name: "shell command with quoted push text", command: `bash -c 'echo "git push"'`, want: false},
		{name: "shell options with quoted push text", command: `bash -euo pipefail -c 'echo "git push"'`, want: false},
		{name: "command string ignores redirected stdin script", command: "bash -c 'echo ok' <<< 'git push'", want: false},
		{name: "script file ignores redirected stdin script", command: "bash script.sh <<< 'git push'", want: false},
		{name: "non stdin heredoc is not shell script", command: "bash -s 3<<'EOF'\ngit push\nEOF\n", want: false},
		{name: "benign shell stdin heredoc", command: "bash -s <<EOF\ngit status\nEOF\n", want: false},
		{name: "later file redirect replaces heredoc stdin", command: "bash -s <<EOF </dev/null\ngit push\nEOF\n", want: false},
		{name: "later file redirect replaces duplicated heredoc stdin", command: "bash -s 3<<EOF 0<&3 </dev/null\ngit push\nEOF\n", want: false},
		{name: "separator makes dash a script path", command: "bash -- - <<EOF\ngit push\nEOF\n", want: false},
		{name: "command string ignores pipeline stdin", command: "printf x | bash -c 'cat >/dev/null'", want: false},
		{name: "script file ignores pipeline stdin", command: "printf x | bash script.sh", want: false},
		{name: "file redirect replaces pipeline stdin", command: "printf x | bash -s </dev/null", want: false},
		{name: "group file redirect replaces pipeline stdin", command: "printf x | { bash -s; } </dev/null", want: false},
		{name: "benign here string replaces pipeline stdin", command: "printf x | bash -s <<< 'git status'", want: false},
		{name: "shell noexec stdin script", command: "bash -n <<< 'git push'", want: false},
		{name: "shell command string noexec script", command: "bash -c -n 'git push'", want: false},
		{name: "shell named noexec stdin script", command: "bash -o noexec <<< 'git push'", want: false},
		{name: "shell help ignores stdin script", command: "bash --help <<< 'git push'", want: false},
		{name: "shell version ignores stdin script", command: "bash --version <<< 'git push'", want: false},
		{name: "shell noninteractive rcfile is ignored", command: "bash --rcfile /dev/fd/3 3<<'EOF' </dev/null\ngit push\nEOF\n", want: false},
		{name: "shell noexec rcfile is ignored", command: "bash --rcfile /dev/fd/3 -i -n 3<<'EOF' </dev/null\ngit push\nEOF\n", want: false},
		{name: "literal command substitution", command: `echo '$(git push)'`, want: false},
		{name: "benign command substitution comment parenthesis", command: "echo \"$(echo ok # )\necho done)\"", want: false},
		{name: "nested command substitution keeps word comment state", command: "echo $(echo foo$(printf bar)#suffix)", want: false},
		{name: "benign dynamic argument", command: "echo $(printf push)", want: false},
		{name: "benign parameter argument", command: `echo "$HOME"`, want: false},
		{name: "benign brace expansion", command: "echo {push,status}", want: false},
		{name: "benign glob argument", command: "echo p*", want: false},
		{name: "quoted glob subcommand is literal", command: `git "p*"`, want: false},
		{name: "quoted brace expansion is literal", command: `git "{push,status}"`, want: false},
		{name: "dynamic attached git dir option", command: `git --git-dir="$GIT_DIR" status`, want: false},
		{name: "dynamic attached git chdir option", command: `git -C"$REPO" status`, want: false},
		{name: "dynamic attached git config option", command: `git -cfoo.bar="$VALUE" status`, want: false},
		{name: "dynamic separate git chdir option", command: `git -C "$REPO" status`, want: false},
		{name: "dynamic separate git config option", command: `git -c "$VALUE" status`, want: false},
		{name: "dynamic attached env chdir option", command: `env --chdir="$DIR" git status`, want: false},
		{name: "dynamic separate env unset option", command: `env -u "$NAME" git status`, want: false},
		{name: "dynamic separate time output option", command: `/usr/bin/time -o "$OUTPUT" git status`, want: false},
		{name: "dynamic attached shell rcfile option", command: `bash --rcfile="$FILE" -c "echo ok"`, want: false},
		{name: "ANSI-C quoted argument to another command", command: "echo $'git push'", want: false},
		{name: "ANSI-C unknown escape remains literal", command: `git $'pu\sh'`, want: false},
		{name: "ANSI-C NUL truncates before push text", command: `git $'status\0push'`, want: false},
		{name: "env split string does not interpret shell separators", command: "env -S 'git status; git push'", want: false},
		{name: "empty env split string preserves quoted argument", command: "env --split-string= 'git push'", want: false},
		{name: "quoted env split separator is literal space", command: `env -S '"git\_push"'`, want: false},
		{name: "time wraps non-push", command: "time git status", want: false},
		{name: "exec wraps non-push", command: "exec git status", want: false},
		{name: "exec assignment is executable name", command: "exec FOO=bar git push", want: false},
		{name: "command assignment is executable name", command: "command FOO=bar git push", want: false},
		{name: "command lookup option cluster", command: "command -pv git push", want: false},
		{name: "command separator preserves option executable", command: "command -- -p git push", want: false},
		{name: "exec invalid option is non-push", command: "exec -x git push", want: false},
		{name: "exec long option is non-push", command: "exec --help git push", want: false},
		{name: "exec argv zero hides push argument", command: "exec -a name echo git push", want: false},
		{name: "env cannot invoke exec builtin", command: "env exec git push", want: false},
		{name: "external time cannot invoke exec builtin", command: "/usr/bin/time exec git push", want: false},
		{name: "path exec is not builtin", command: "/usr/bin/exec git push", want: false},
		{name: "exec cannot invoke exec builtin", command: "exec exec git push", want: false},
		{name: "builtin rejects external git", command: "builtin git push", want: false},
		{name: "env cannot invoke builtin exec", command: "env builtin exec git push", want: false},
		{name: "trap reset is non-push", command: "trap - EXIT", want: false},
		{name: "trap listing is non-push", command: "trap -p", want: false},
		{name: "trap hyphen option is non-push", command: "trap '-x; git push' EXIT", want: false},
		{name: "trap reset after separator is non-push", command: "trap -- - EXIT", want: false},
		{name: "trap empty action after separator is non-push", command: "trap -- '' EXIT", want: false},
		{name: "env cannot invoke trap builtin", command: "env trap 'git push' EXIT", want: false},
		{name: "env cannot invoke builtin trap", command: "env builtin trap 'git push' EXIT", want: false},
		{name: "path trap is not builtin", command: "/usr/bin/trap 'git push' EXIT", want: false},
		{name: "trap without signal is non-push", command: "trap 'git push'", want: false},
		{name: "commented trap push is non-push", command: "trap 'echo ok # $(git push)' EXIT", want: false},
		{name: "time wraps push text argument", command: "time printf '%s' 'git push'", want: false},
		{name: "reserved time does not accept separator", command: "time -- git push", want: false},
		{name: "reserved time does not accept output option", command: "time -o report git push", want: false},
		{name: "arithmetic shift is not a heredoc", command: "echo $((1 << 2))", want: false},
		{name: "heredoc arithmetic shift is not command substitution", command: "cat <<EOF\n$((1 << 2))\nEOF\n", want: false},
		{name: "quoted arithmetic shift is not a heredoc", command: `printf '%d\n' "$((1<<2))"`, want: false},
		{name: "arithmetic command shift is not a heredoc", command: "((value = 1 << 2))", want: false},
		{name: "nested command substitution quote is not a heredoc", command: `echo "$(printf "%s" "<<EOF")"`, want: false},
		{name: "ANSI-C escaped quote shields heredoc text", command: `printf '%s\n' $'x\'<<EOF'`, want: false},
		{name: "parameter expansion shields heredoc text", command: `echo ${value#<<}`, want: false},
		{name: "command lookup", command: "command -v git push", want: false},
		{name: "different git subcommand", command: "git config alias.push status", want: false},
		{name: "benign git alias", command: "git -c alias.s=status s", want: false},
		{name: "benign git process environment alias", command: "GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=alias.s GIT_CONFIG_VALUE_0=status git s", want: false},
		{name: "git shell alias invocation is argument", command: "git -c alias.p='!echo' p git push", want: false},
		{name: "git shell alias does not execute inherited alias argument", command: "git -c alias.p='!echo' -c alias.q=push p q", want: false},
		{name: "git shell alias cycle terminates", command: "git -c alias.p='!git p' p", want: false},
		{name: "git shell alias cleared child environment", command: "git -c alias.p='!env -i git' -c alias.q=push p q", want: false},
		{name: "git static environment alias overrides dynamic alias", command: "KEY=alias.p; GIT_CONFIG_COUNT=2 GIT_CONFIG_KEY_0=$KEY GIT_CONFIG_VALUE_0=push GIT_CONFIG_KEY_1=alias.p GIT_CONFIG_VALUE_1=status git p", want: false},
		{name: "unused push alias", command: "git -c alias.p=push status", want: false},
		{name: "unused config env push alias", command: "git --config-env=alias.p=ALIAS_VALUE status", want: false},
		{name: "unused dynamic push alias", command: `git -c alias.p="$ALIAS_VALUE" status`, want: false},
		{name: "git alias semicolon is not shell syntax", command: "git -c 'alias.s=status; push' s", want: false},
		{name: "git alias shell text is not shell syntax", command: "git -c 'alias.s=status; git push' s", want: false},
		{name: "git alias substitution is not shell syntax", command: "git -c 'alias.s=status $(git push)' s", want: false},
		{name: "benign git option prefixed alias", command: "git -c alias.p='-p q' -c alias.q=status p", want: false},
		{name: "git global separator is invalid", command: "git -c alias.p=push -- p", want: false},
		{name: "git attached config option is invalid", command: "git -calias.p=push p", want: false},
		{name: "similarly named executable", command: "mygit push", want: false},
		{name: "relative executable", command: "./git push", want: false},
		{name: "commented push", command: "git status # git push", want: false},
		{name: "commented command substitution", command: "echo ok # $(git push)", want: false},
		{name: "commented backtick substitution", command: "echo ok # `git push`", want: false},
		{name: "malformed substitution in comment", command: "echo ok # $(", want: false},
		{name: "word embedded comment marker", command: "echo x#$(git push)", want: true},
		{name: "quoted comment marker", command: `echo "# $(git push)"`, want: true},
		{name: "push after commented substitution", command: "echo ok # $(git push)\ngit push", want: true},
		{name: "unterminated quote", command: `echo "git push`, wantErr: true},
		{name: "trailing escape", command: `echo git\`, wantErr: true},
		{name: "unterminated ANSI-C quote", command: "git $'push", wantErr: true},
		{name: "unterminated arithmetic expression", command: "echo $((1 << 2)", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := containsGitPush(tt.command)
			if (err != nil) != tt.wantErr {
				t.Fatalf("containsGitPush(%q) error = %v, wantErr %v", tt.command, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("containsGitPush(%q) = %v, want %v", tt.command, got, tt.want)
			}
		})
	}
}

func TestDecodeHookInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		input        string
		wantCommand  string
		wantRelevant bool
		wantErr      bool
	}{
		{
			name:         "Claude Bash command",
			input:        `{"cwd":"/repo","tool_name":"Bash","tool_input":{"command":"git push"}}`,
			wantCommand:  "git push",
			wantRelevant: true,
		},
		{
			name:         "Codex exec command",
			input:        `{"tool_name":"exec_command","tool_input":{"cmd":"git status"}}`,
			wantCommand:  "git status",
			wantRelevant: true,
		},
		{
			name:         "Claude Monitor command",
			input:        `{"tool_name":"Monitor","tool_input":{"command":"git push"}}`,
			wantCommand:  "git push",
			wantRelevant: true,
		},
		{
			name:  "Claude Monitor websocket",
			input: `{"tool_name":"Monitor","tool_input":{"ws":{"url":"wss://127.0.0.1:1234"}}}`,
		},
		{
			name:  "unrelated tool is ignored",
			input: `{"tool_name":"Write","tool_input":{"file_path":"README.md"}}`,
		},
		{name: "malformed JSON", input: `{`, wantErr: true},
		{name: "multiple JSON values", input: `{} {}`, wantErr: true},
		{name: "missing tool input", input: `{"tool_name":"Bash"}`, wantErr: true},
		{name: "missing shell command", input: `{"tool_name":"Bash","tool_input":{}}`, wantErr: true},
		{name: "non-string shell command", input: `{"tool_name":"Bash","tool_input":{"command":42}}`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, command, relevant, err := decodeHookInput(strings.NewReader(tt.input))
			if (err != nil) != tt.wantErr {
				t.Fatalf("decodeHookInput() error = %v, wantErr %v", err, tt.wantErr)
			}
			if command != tt.wantCommand {
				t.Errorf("decodeHookInput() command = %q, want %q", command, tt.wantCommand)
			}
			if relevant != tt.wantRelevant {
				t.Errorf("decodeHookInput() relevant = %v, want %v", relevant, tt.wantRelevant)
			}
		})
	}
}

func TestClaudePrePushHookIncludesMonitor(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "..", ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Hooks struct {
			PreToolUse []struct {
				Matcher string `json:"matcher"`
			} `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	got := ""
	if len(settings.Hooks.PreToolUse) != 0 {
		got = settings.Hooks.PreToolUse[0].Matcher
	}
	if got != "Bash|Monitor" {
		t.Fatalf("Claude PreToolUse matcher = %q, want %q", got, "Bash|Monitor")
	}
}

func TestResolveProjectRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, ".git"), "gitdir: elsewhere\n", 0o600)
	writeTestFile(t, filepath.Join(root, "Makefile"), "test:\n", 0o600)
	nested := filepath.Join(root, "one", "two")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}

	got, err := resolveProjectRoot("", nested)
	if err != nil {
		t.Fatalf("resolveProjectRoot() error = %v", err)
	}
	if got != root {
		t.Errorf("resolveProjectRoot() = %q, want %q", got, root)
	}
}

func TestRunPrePush(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake make uses a POSIX shell script")
	}

	t.Run("push invokes check exactly once", func(t *testing.T) {
		root, record := configureFakeMake(t)
		input := marshalHookInput(t, root, "git push origin main")
		var stdout, stderr bytes.Buffer

		got := run([]string{"pre-push"}, strings.NewReader(input), &stdout, &stderr)

		if got != exitOK {
			t.Fatalf("run(pre-push) = %d, want %d; stderr = %q", got, exitOK, stderr.String())
		}
		assertMakeInvocation(t, record, root, "check")
	})

	t.Run("non push does not invoke make", func(t *testing.T) {
		root, record := configureFakeMake(t)
		input := marshalHookInput(t, root, "git status")
		var stdout, stderr bytes.Buffer

		got := run([]string{"pre-push"}, strings.NewReader(input), &stdout, &stderr)

		if got != exitOK {
			t.Fatalf("run(pre-push) = %d, want %d; stderr = %q", got, exitOK, stderr.String())
		}
		if _, err := os.Stat(record); !os.IsNotExist(err) {
			t.Fatalf("make record exists for non-push command: err = %v", err)
		}
	})

	t.Run("gate failure blocks push", func(t *testing.T) {
		root, record := configureFakeMake(t)
		t.Setenv("AGENT_HOOK_EXIT", "7")
		input := marshalHookInput(t, root, "git push")
		var stdout, stderr bytes.Buffer

		got := run([]string{"pre-push"}, strings.NewReader(input), &stdout, &stderr)

		if got != exitBlock {
			t.Fatalf("run(pre-push) = %d, want %d", got, exitBlock)
		}
		if !strings.Contains(stderr.String(), "make check failed") {
			t.Errorf("stderr = %q, want make check failure", stderr.String())
		}
		if !strings.Contains(stderr.String(), "fake make failure") {
			t.Errorf("stderr = %q, want captured make output", stderr.String())
		}
		assertMakeInvocation(t, record, root, "check")
	})

	t.Run("malformed input blocks push hook", func(t *testing.T) {
		var stdout, stderr bytes.Buffer

		got := run([]string{"pre-push"}, strings.NewReader("{"), &stdout, &stderr)

		if got != exitBlock {
			t.Fatalf("run(pre-push) = %d, want %d", got, exitBlock)
		}
		if !strings.Contains(stderr.String(), "invalid pre-push hook input") {
			t.Errorf("stderr = %q, want invalid input diagnostic", stderr.String())
		}
	})
}

func TestRunFormat(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake make uses a POSIX shell script")
	}

	t.Run("success invokes formatter", func(t *testing.T) {
		root, record := configureFakeMake(t)
		var stdout, stderr bytes.Buffer
		got := run([]string{"format"}, strings.NewReader("ignored"), &stdout, &stderr)
		if got != exitOK {
			t.Fatalf("run(format) = %d, want %d; stderr = %q", got, exitOK, stderr.String())
		}
		assertMakeInvocation(t, record, root, "fmt")
	})

	t.Run("failure reports and blocks", func(t *testing.T) {
		root, record := configureFakeMake(t)
		t.Setenv("AGENT_HOOK_EXIT", "9")
		var stdout, stderr bytes.Buffer
		got := run([]string{"format"}, strings.NewReader("ignored"), &stdout, &stderr)
		if got != exitBlock {
			t.Fatalf("run(format) = %d, want %d", got, exitBlock)
		}
		if !strings.Contains(stderr.String(), "fake make failure") || !strings.Contains(stderr.String(), "make fmt failed") {
			t.Errorf("stderr = %q, want formatter output and failure summary", stderr.String())
		}
		assertMakeInvocation(t, record, root, "fmt")
	})
}

func TestRunRejectsInvalidMode(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	got := run([]string{"unknown"}, strings.NewReader(""), &stdout, &stderr)
	if got != exitBlock {
		t.Fatalf("run(unknown) = %d, want %d", got, exitBlock)
	}
	if !strings.Contains(stderr.String(), "usage:") {
		t.Errorf("stderr = %q, want usage", stderr.String())
	}
}

func configureFakeMake(t *testing.T) (string, string) {
	t.Helper()

	root := filepath.Join(t.TempDir(), "root with spaces")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "Makefile"), "check:\nfmt:\n", 0o600)
	record := filepath.Join(t.TempDir(), "make-args")
	fakeMake := filepath.Join(t.TempDir(), "make")
	writeTestFile(t, fakeMake, "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$AGENT_HOOK_RECORD\"\nif [ -n \"${AGENT_HOOK_EXIT:-}\" ]; then\n\techo 'fake make failure'\n\texit \"$AGENT_HOOK_EXIT\"\nfi\n", 0o700)
	t.Setenv("AGENT_HOOK_PROJECT_ROOT", root)
	t.Setenv("AGENT_HOOK_RECORD", record)
	t.Setenv("AGENT_HOOK_EXIT", "")
	t.Setenv("MAKE", fakeMake)
	return root, record
}

func marshalHookInput(t *testing.T, cwd, command string) string {
	t.Helper()

	input := map[string]any{
		"cwd":       cwd,
		"tool_name": "Bash",
		"tool_input": map[string]string{
			"command": command,
		},
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertMakeInvocation(t *testing.T, record, root, target string) {
	t.Helper()

	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("-C\n%s\n%s\n", root, target)
	if string(data) != want {
		t.Errorf("make arguments = %q, want %q", data, want)
	}
}

func writeTestFile(t *testing.T, path, contents string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
}
