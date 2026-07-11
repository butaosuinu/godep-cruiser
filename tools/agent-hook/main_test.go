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
		{name: "non push git command", command: "git status", want: false},
		{name: "push is argument to another command", command: "echo git push", want: false},
		{name: "quoted command text", command: `echo "git push; still text"`, want: false},
		{name: "quoted separator", command: `printf '%s' 'text; git push'`, want: false},
		{name: "shell command with quoted push text", command: `bash -c 'echo "git push"'`, want: false},
		{name: "literal command substitution", command: `echo '$(git push)'`, want: false},
		{name: "command lookup", command: "command -v git push", want: false},
		{name: "different git subcommand", command: "git config alias.push status", want: false},
		{name: "similarly named executable", command: "mygit push", want: false},
		{name: "relative executable", command: "./git push", want: false},
		{name: "commented push", command: "git status # git push", want: false},
		{name: "unterminated quote", command: `echo "git push`, wantErr: true},
		{name: "trailing escape", command: `echo git\`, wantErr: true},
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
