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
		{name: "backtick substitution", command: "echo `git push`", want: true},
		{name: "quoted command substitution", command: `echo "$(git push)"`, want: true},
		{name: "quoted backtick substitution", command: "echo \"`git push`\"", want: true},
		{name: "bash command string", command: "bash -c 'git push'", want: true},
		{name: "zsh login command string", command: `/bin/zsh -lc "GIT_SSH_COMMAND='ssh -F /dev/null' git push"`, want: true},
		{name: "environment wrapped shell", command: "env TOKEN=value bash -c 'git push'", want: true},
		{name: "evaluated command", command: "eval 'git push'", want: true},
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
		{name: "literal command substitution", command: `echo '$(git push)'`, want: false},
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
		{name: "time wraps push text argument", command: "time printf '%s' 'git push'", want: false},
		{name: "reserved time does not accept separator", command: "time -- git push", want: false},
		{name: "reserved time does not accept output option", command: "time -o report git push", want: false},
		{name: "arithmetic shift is not a heredoc", command: "echo $((1 << 2))", want: false},
		{name: "quoted arithmetic shift is not a heredoc", command: `printf '%d\n' "$((1<<2))"`, want: false},
		{name: "arithmetic command shift is not a heredoc", command: "((value = 1 << 2))", want: false},
		{name: "nested command substitution quote is not a heredoc", command: `echo "$(printf "%s" "<<EOF")"`, want: false},
		{name: "ANSI-C escaped quote shields heredoc text", command: `printf '%s\n' $'x\'<<EOF'`, want: false},
		{name: "parameter expansion shields heredoc text", command: `echo ${value#<<}`, want: false},
		{name: "command lookup", command: "command -v git push", want: false},
		{name: "different git subcommand", command: "git config alias.push status", want: false},
		{name: "similarly named executable", command: "mygit push", want: false},
		{name: "relative executable", command: "./git push", want: false},
		{name: "commented push", command: "git status # git push", want: false},
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
