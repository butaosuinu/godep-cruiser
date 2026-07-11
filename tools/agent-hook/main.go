// Command agent-hook provides the repository-local hooks used by coding agents.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"
)

const (
	exitOK    = 0
	exitBlock = 2
)

type hookInput struct {
	CWD       string          `json:"cwd"`
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
}

type shellToken struct {
	text string
	kind tokenKind
}

type tokenKind uint8

const (
	wordToken tokenKind = iota
	controlToken
	redirectToken
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: agent-hook pre-push|format")
		return exitBlock
	}

	switch args[0] {
	case "pre-push":
		return runPrePush(stdin, stdout, stderr)
	case "format":
		return runMakeTarget("fmt", "", stdout, stderr)
	default:
		fmt.Fprintln(stderr, "usage: agent-hook pre-push|format")
		return exitBlock
	}
}

func runPrePush(stdin io.Reader, stdout, stderr io.Writer) int {
	input, command, relevant, err := decodeHookInput(stdin)
	if err != nil {
		fmt.Fprintf(stderr, "agent-hook: invalid pre-push hook input: %v\n", err)
		return exitBlock
	}
	if !relevant {
		return exitOK
	}

	push, err := containsGitPush(command)
	if err != nil {
		fmt.Fprintf(stderr, "agent-hook: cannot parse shell command: %v\n", err)
		return exitBlock
	}
	if !push {
		return exitOK
	}

	return runMakeTarget("check", input.CWD, stdout, stderr)
}

func decodeHookInput(r io.Reader) (hookInput, string, bool, error) {
	var input hookInput
	decoder := json.NewDecoder(r)
	if err := decoder.Decode(&input); err != nil {
		return hookInput{}, "", false, err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return hookInput{}, "", false, err
	}

	if input.ToolName != "" && !isShellTool(input.ToolName) {
		return input, "", false, nil
	}
	if len(input.ToolInput) == 0 || bytes.Equal(bytes.TrimSpace(input.ToolInput), []byte("null")) {
		return hookInput{}, "", false, errors.New("missing tool_input")
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(input.ToolInput, &fields); err != nil {
		return hookInput{}, "", false, fmt.Errorf("tool_input: %w", err)
	}
	raw, ok := fields["command"]
	if !ok {
		raw, ok = fields["cmd"]
	}
	if !ok {
		return hookInput{}, "", false, errors.New("tool_input has no command or cmd field")
	}

	var command string
	if err := json.Unmarshal(raw, &command); err != nil {
		return hookInput{}, "", false, fmt.Errorf("shell command: %w", err)
	}
	return input, command, true, nil
}

func isShellTool(name string) bool {
	switch strings.ToLower(filepath.Base(name)) {
	case "bash", "shell", "exec_command", "shell_command", "terminal":
		return true
	default:
		return false
	}
}

func runMakeTarget(target, hookCWD string, stdout, stderr io.Writer) int {
	root, err := resolveProjectRoot(os.Getenv("AGENT_HOOK_PROJECT_ROOT"), hookCWD)
	if err != nil {
		fmt.Fprintf(stderr, "agent-hook: cannot resolve project root: %v\n", err)
		return exitBlock
	}

	makeExecutable := os.Getenv("MAKE")
	if makeExecutable == "" {
		makeExecutable = "make"
	}
	cmd := exec.Command(makeExecutable, "-C", root, target)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if len(output) > 0 {
			_, _ = stderr.Write(output)
			if output[len(output)-1] != '\n' {
				fmt.Fprintln(stderr)
			}
		}
		fmt.Fprintf(stderr, "agent-hook: make %s failed: %v\n", target, err)
		return exitBlock
	}
	if len(output) > 0 {
		_, _ = stdout.Write(output)
	}
	return exitOK
}

func resolveProjectRoot(explicitRoot, hookCWD string) (string, error) {
	if explicitRoot != "" {
		return validateRoot(explicitRoot)
	}

	start := hookCWD
	if start == "" {
		var err error
		start, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	start, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	if info, statErr := os.Stat(start); statErr != nil {
		return "", statErr
	} else if !info.IsDir() {
		start = filepath.Dir(start)
	}

	for dir := filepath.Clean(start); ; dir = filepath.Dir(dir) {
		if isProjectRoot(dir) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	return "", fmt.Errorf("no parent directory contains both .git and Makefile (starting at %q)", start)
}

func validateRoot(root string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%q is not a directory", absRoot)
	}
	if _, err := os.Stat(filepath.Join(absRoot, "Makefile")); err != nil {
		return "", fmt.Errorf("%q has no Makefile: %w", absRoot, err)
	}
	return filepath.Clean(absRoot), nil
}

func isProjectRoot(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return false
	}
	if info, err := os.Stat(filepath.Join(dir, "Makefile")); err != nil || info.IsDir() {
		return false
	}
	return true
}

func containsGitPush(command string) (bool, error) {
	nested, err := commandSubstitutions(command)
	if err != nil {
		return false, err
	}
	for _, script := range nested {
		push, nestedErr := containsGitPush(script)
		if nestedErr != nil {
			return false, nestedErr
		}
		if push {
			return true, nil
		}
	}

	tokens, err := lexShell(command)
	if err != nil {
		return false, err
	}

	start := 0
	for i, token := range tokens {
		if token.kind != controlToken {
			continue
		}
		if segmentContainsGitPush(tokens[start:i]) {
			return true, nil
		}
		start = i + 1
	}
	return segmentContainsGitPush(tokens[start:]), nil
}

func commandSubstitutions(command string) ([]string, error) {
	var scripts []string
	quote := byte(0)

	for i := 0; i < len(command); i++ {
		ch := command[i]
		if quote == '\'' {
			if ch == '\'' {
				quote = 0
			}
			continue
		}
		if ch == '\\' {
			if i+1 >= len(command) {
				return nil, errors.New("trailing escape")
			}
			i++
			continue
		}
		if ch == '\'' && quote == 0 {
			quote = '\''
			continue
		}
		if ch == '"' {
			switch quote {
			case '"':
				quote = 0
			case 0:
				quote = '"'
			}
			continue
		}
		if ch == '`' {
			end, err := closingBacktick(command, i+1)
			if err != nil {
				return nil, err
			}
			scripts = append(scripts, command[i+1:end])
			i = end
			continue
		}
		if ch == '$' && i+1 < len(command) && command[i+1] == '(' {
			end, err := closingCommandParenthesis(command, i+2)
			if err != nil {
				return nil, err
			}
			scripts = append(scripts, command[i+2:end])
			i = end
		}
	}

	if quote != 0 {
		return nil, errors.New("unterminated quoted string")
	}
	return scripts, nil
}

func closingBacktick(command string, start int) (int, error) {
	for i := start; i < len(command); i++ {
		if command[i] == '\\' {
			i++
			continue
		}
		if command[i] == '`' {
			return i, nil
		}
	}
	return 0, errors.New("unterminated backtick command substitution")
}

func closingCommandParenthesis(command string, start int) (int, error) {
	depth := 1
	quote := byte(0)
	for i := start; i < len(command); i++ {
		ch := command[i]
		if quote == '\'' {
			if ch == '\'' {
				quote = 0
			}
			continue
		}
		if ch == '\\' {
			i++
			continue
		}
		if ch == '\'' && quote == 0 {
			quote = '\''
			continue
		}
		if ch == '"' {
			switch quote {
			case '"':
				quote = 0
			case 0:
				quote = '"'
			}
			continue
		}
		if quote != 0 {
			continue
		}
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i, nil
			}
		}
	}
	return 0, errors.New("unterminated command substitution")
}

func segmentContainsGitPush(tokens []shellToken) bool {
	i := skipCommandPrefixes(tokens, 0)
	if i >= len(tokens) || tokens[i].kind != wordToken {
		return false
	}

	for {
		executable := filepath.Base(tokens[i].text)
		switch executable {
		case "command":
			i++
			for i < len(tokens) && tokens[i].kind == wordToken && strings.HasPrefix(tokens[i].text, "-") {
				if tokens[i].text == "-v" || tokens[i].text == "-V" {
					return false
				}
				i++
			}
		case "env":
			i++
			for i < len(tokens) && tokens[i].kind == wordToken {
				arg := tokens[i].text
				if isAssignment(arg) {
					i++
					continue
				}
				if arg == "-u" || arg == "--unset" || arg == "-C" || arg == "--chdir" || arg == "--argv0" {
					i += 2
					continue
				}
				if strings.HasPrefix(arg, "-") {
					i++
					continue
				}
				break
			}
		case "sh", "bash", "dash", "ksh", "zsh":
			return shellCommandContainsGitPush(tokens[i+1:])
		case "eval":
			return evaluatedCommandContainsGitPush(tokens[i+1:])
		default:
			if !isGitExecutable(tokens[i].text) {
				return false
			}
			return gitArgsContainPush(tokens[i+1:])
		}

		i = skipCommandPrefixes(tokens, i)
		if i >= len(tokens) || tokens[i].kind != wordToken {
			return false
		}
	}
}

func shellCommandContainsGitPush(tokens []shellToken) bool {
	for i := 0; i < len(tokens); i++ {
		if tokens[i].kind != wordToken {
			continue
		}
		arg := tokens[i].text
		hasCommand := arg == "-c" || (strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.Contains(arg[1:], "c"))
		if hasCommand {
			for i++; i < len(tokens); i++ {
				if tokens[i].kind != wordToken {
					continue
				}
				push, err := containsGitPush(tokens[i].text)
				return err != nil || push
			}
			return false
		}
		if !strings.HasPrefix(arg, "-") {
			return false
		}
		if arg == "-O" || arg == "+O" || arg == "--rcfile" || arg == "--init-file" {
			i++
		}
	}
	return false
}

func evaluatedCommandContainsGitPush(tokens []shellToken) bool {
	var words []string
	for _, token := range tokens {
		if token.kind == wordToken {
			words = append(words, token.text)
		}
	}
	if len(words) == 0 {
		return false
	}
	push, err := containsGitPush(strings.Join(words, " "))
	return err != nil || push
}

func skipCommandPrefixes(tokens []shellToken, start int) int {
	i := start
	for i < len(tokens) {
		if tokens[i].kind == redirectToken {
			i++
			if i < len(tokens) && tokens[i].kind == wordToken {
				i++
			}
			continue
		}
		if tokens[i].kind == wordToken && isAssignment(tokens[i].text) {
			i++
			continue
		}
		if tokens[i].kind == wordToken {
			switch tokens[i].text {
			case "!", "{", "}", "if", "then", "elif", "else", "do", "while", "until":
				i++
				continue
			}
		}
		break
	}
	return i
}

func isAssignment(word string) bool {
	name, _, ok := strings.Cut(word, "=")
	if !ok || name == "" {
		return false
	}
	for i, r := range name {
		if (i == 0 && r != '_') && !unicode.IsLetter(r) {
			return false
		}
		if i > 0 && r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func isGitExecutable(word string) bool {
	return word == "git" || (filepath.IsAbs(word) && filepath.Base(word) == "git")
}

func gitArgsContainPush(tokens []shellToken) bool {
	optionsWithSeparateValue := map[string]bool{
		"-C": true, "-c": true, "--git-dir": true, "--work-tree": true,
		"--namespace": true, "--config-env": true, "--exec-path": true,
	}

	for i := 0; i < len(tokens); i++ {
		if tokens[i].kind == redirectToken {
			i++
			continue
		}
		if tokens[i].kind != wordToken {
			continue
		}
		arg := tokens[i].text
		if arg == "--" {
			i++
			return i < len(tokens) && tokens[i].kind == wordToken && tokens[i].text == "push"
		}
		if strings.HasPrefix(arg, "-") {
			if optionsWithSeparateValue[arg] {
				i++
			}
			continue
		}
		return arg == "push"
	}
	return false
}

func lexShell(command string) ([]shellToken, error) {
	var tokens []shellToken
	var current strings.Builder
	inWord := false
	quote := byte(0)

	flushWord := func() {
		if !inWord {
			return
		}
		tokens = append(tokens, shellToken{text: current.String(), kind: wordToken})
		current.Reset()
		inWord = false
	}

	for i := 0; i < len(command); i++ {
		ch := command[i]
		if quote != 0 {
			if ch == quote {
				quote = 0
				continue
			}
			if quote == '"' && ch == '\\' {
				if i+1 >= len(command) {
					return nil, errors.New("trailing escape in double-quoted string")
				}
				i++
				if command[i] != '\n' {
					current.WriteByte(command[i])
				}
				continue
			}
			current.WriteByte(ch)
			continue
		}

		switch ch {
		case '\'', '"':
			quote = ch
			inWord = true
		case '\\':
			if i+1 >= len(command) {
				return nil, errors.New("trailing escape")
			}
			i++
			if command[i] != '\n' {
				current.WriteByte(command[i])
				inWord = true
			}
		case ' ', '\t', '\r':
			flushWord()
		case '\n':
			flushWord()
			tokens = append(tokens, shellToken{text: "\n", kind: controlToken})
		case '#':
			if inWord {
				current.WriteByte(ch)
				continue
			}
			for i+1 < len(command) && command[i+1] != '\n' {
				i++
			}
		default:
			if strings.ContainsRune(";&|()<>`", rune(ch)) {
				fileDescriptor := ""
				if (ch == '<' || ch == '>') && inWord && containsOnlyDigits(current.String()) {
					fileDescriptor = current.String()
					current.Reset()
					inWord = false
				} else {
					flushWord()
				}
				op, consumed := shellOperator(command[i:])
				i += consumed - 1
				kind := redirectToken
				if isControlOperator(op) {
					kind = controlToken
				}
				tokens = append(tokens, shellToken{text: fileDescriptor + op, kind: kind})
				continue
			}
			current.WriteByte(ch)
			inWord = true
		}
	}

	if quote != 0 {
		return nil, errors.New("unterminated quoted string")
	}
	flushWord()
	return tokens, nil
}

func containsOnlyDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func shellOperator(rest string) (string, int) {
	for _, operator := range []string{";;&", "<<<", "&>>", "&&", "||", "|&", ";;", ";&", ">>", "<<", "<>", ">&", "<&", ">|", "&>"} {
		if strings.HasPrefix(rest, operator) {
			return operator, len(operator)
		}
	}
	return rest[:1], 1
}

func isControlOperator(operator string) bool {
	switch operator {
	case ";", ";;", ";&", ";;&", "&", "&&", "|", "|&", "||", "(", ")", "`":
		return true
	default:
		return false
	}
}
