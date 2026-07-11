package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestStripHereDocBodiesDetection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		command string
		want    bool
		wantErr bool
	}{
		{
			name:    "literal unquoted body is data",
			command: "cat <<EOF\ngit push\nEOF\n",
		},
		{
			name:    "single quoted delimiter suppresses expansion",
			command: "cat <<'EOF'\n$(git push)\n`git push`\nEOF\n",
		},
		{
			name:    "double quoted delimiter suppresses expansion",
			command: "cat <<\"EOF\"\n$(git push)\nEOF\n",
		},
		{
			name:    "escaped delimiter suppresses expansion",
			command: "cat <<\\EOF\n$(git push)\nEOF\n",
		},
		{
			name:    "partially quoted delimiter suppresses expansion",
			command: "cat <<E\"OF\"\n$(git push)\nEOF\n",
		},
		{
			name:    "unquoted body command substitution executes",
			command: "cat <<EOF\n$(git push)\nEOF\n",
			want:    true,
		},
		{
			name:    "continued delimiter remains unquoted",
			command: "cat <<\\\nEOF\n$(git push)\nEOF\n",
			want:    true,
		},
		{
			name:    "unquoted body backtick substitution executes",
			command: "cat <<EOF\n`git push`\nEOF\n",
			want:    true,
		},
		{
			name:    "quotes in unquoted body do not suppress expansion",
			command: "cat <<EOF\n'$(git push)'\nEOF\n",
			want:    true,
		},
		{
			name:    "escaped expansion in unquoted body is literal",
			command: "cat <<EOF\n\\$(git push)\n\\`git push\\`\nEOF\n",
		},
		{
			name:    "line continuation forms command substitution",
			command: "cat <<EOF\n$\\\n(git push)\nEOF\n",
			want:    true,
		},
		{
			name:    "escaped line continuation does not join substitution",
			command: "cat <<EOF\n$\\\\\n(git push)\nEOF\n",
		},
		{
			name:    "tab stripping literal body",
			command: "cat <<-EOF\n\tgit push\n\tEOF\n",
		},
		{
			name:    "tab stripping executable substitution",
			command: "cat <<-EOF\n\t$(git push)\n\tEOF\n",
			want:    true,
		},
		{
			name:    "spaces do not satisfy tab stripping terminator",
			command: "cat <<-EOF\nbody\n EOF\n",
			wantErr: true,
		},
		{
			name:    "multiple heredocs ignore literal and quoted bodies",
			command: "cat <<A <<'B'\ngit push\nA\n$(git push)\nB\n",
		},
		{
			name:    "multiple heredocs preserve unquoted expansion",
			command: "cat <<'A' <<B\n$(git push)\nA\n$(git push)\nB\n",
			want:    true,
		},
		{
			name:    "real command after terminator remains executable",
			command: "cat <<EOF\ngit push\nEOF\ngit push\n",
			want:    true,
		},
		{
			name:    "continued terminator preserves following command",
			command: "cat <<EOF\nliteral\nEO\\\nF\ngit push\n",
			want:    true,
		},
		{
			name:    "quoted heredoc does not join terminator",
			command: "cat <<'EOF'\nEO\\\nF\nEOF\n",
		},
		{
			name:    "ANSI-C quoted delimiter suppresses expansion",
			command: "cat <<$'EOF'\n$(git push)\nEOF\n",
		},
		{
			name:    "malformed text in quoted body is ignored",
			command: "cat <<'EOF'\n\"'$(\nEOF\n",
		},
		{
			name:    "malformed substitution in unquoted body fails closed",
			command: "cat <<EOF\n$(\nEOF\n",
			wantErr: true,
		},
		{
			name:    "missing terminator fails closed",
			command: "cat <<EOF\ngit status\n",
			wantErr: true,
		},
		{
			name:    "here string is not a heredoc",
			command: "cat <<< 'git push'",
		},
		{
			name:    "delimiter substitution-looking text is not executed",
			command: "cat <<\"$(git push)\"\ngit push\n$(git push)\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := strippedCommandContainsGitPush(tt.command)
			if (err != nil) != tt.wantErr {
				t.Fatalf("strippedCommandContainsGitPush() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("strippedCommandContainsGitPush() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStripHereDocBodiesOutput(t *testing.T) {
	t.Parallel()

	command := "cat 3<<-EOF <<'DONE'\n\tfirst\n\tEOF\nsecond\nDONE\ngit status\n"
	code, scripts, err := stripHereDocBodies(command)
	if err != nil {
		t.Fatal(err)
	}
	wantCode := "cat 3<<" + hereDocPlaceholder + "0__ <<" + hereDocPlaceholder + "1__\ngit status\n"
	if code != wantCode {
		t.Errorf("code = %q, want %q", code, wantCode)
	}
	if len(scripts) != 0 {
		t.Errorf("scripts = %q, want none", scripts)
	}
}

func TestHereDocExecutableScripts(t *testing.T) {
	t.Parallel()

	body := "plain git push\n$(echo $(git push))\n`git status`\n\\$(ignored)\n"
	got, err := hereDocExecutableScripts(body)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"echo $(git push)", "git status"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("hereDocExecutableScripts() = %q, want %q", got, want)
	}
}

func strippedCommandContainsGitPush(command string) (bool, error) {
	return containsGitPush(command)
}

func TestStripHereDocBodiesIgnoresCommentedOperator(t *testing.T) {
	t.Parallel()

	command := "echo ok # <<EOF\ngit status\n"
	code, scripts, err := stripHereDocBodies(command)
	if err != nil {
		t.Fatal(err)
	}
	wantCode := "echo ok \ngit status\n"
	if code != wantCode {
		t.Errorf("code = %q, want %q", code, wantCode)
	}
	if len(scripts) != 0 {
		t.Errorf("scripts = %q, want none", scripts)
	}
}

func TestStripHereDocBodiesPreservesDelimiterLikeBodyLine(t *testing.T) {
	t.Parallel()

	command := strings.Join([]string{
		"cat <<EOF",
		" EOF",
		"EOF trailing",
		"EOF",
		"git status",
		"",
	}, "\n")
	code, scripts, err := stripHereDocBodies(command)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(code, "git status") {
		t.Errorf("code = %q, want following command preserved", code)
	}
	if len(scripts) != 0 {
		t.Errorf("scripts = %q, want none", scripts)
	}
}
