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
	"strconv"
	"strings"
	"unicode"
)

const (
	exitOK                 = 0
	exitBlock              = 2
	dynamicWordPlaceholder = "__AGENT_HOOK_DYNAMIC__"
)

type hookInput struct {
	CWD       string          `json:"cwd"`
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
}

type shellToken struct {
	text     string
	kind     tokenKind
	dynamic  bool
	maySplit bool
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
	command, hereDocScripts, err := stripHereDocBodies(command)
	if err != nil {
		return false, err
	}

	nested, err := commandSubstitutions(command)
	if err != nil {
		return false, err
	}
	nested = append(nested, hereDocScripts...)
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
		if quote == 0 && ch == '$' && i+1 < len(command) && command[i+1] == '\'' {
			_, end, ansiErr := parseANSIQuoted(command, i+2)
			if ansiErr != nil {
				return nil, ansiErr
			}
			i = end
			continue
		}
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
		if quote == 0 && (ch == '<' || ch == '>') && i+1 < len(command) && command[i+1] == '(' {
			end, err := closingCommandParenthesis(command, i+2)
			if err != nil {
				return nil, err
			}
			scripts = append(scripts, command[i+2:end])
			i = end
			continue
		}
		if ch == '$' && i+2 < len(command) && command[i+1] == '(' && command[i+2] == '(' {
			i += 2
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
		if quote == 0 && ch == '$' && i+1 < len(command) && command[i+1] == '\'' {
			_, end, ansiErr := parseANSIQuoted(command, i+2)
			if ansiErr != nil {
				return 0, ansiErr
			}
			i = end
			continue
		}
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
	canUseReservedTime := reservedTimeAllowed(tokens)
	tokens = shellCommandArguments(tokens)
	i := skipCommandPrefixes(tokens, 0)
	if i >= len(tokens) || tokens[i].kind != wordToken {
		return false
	}

	for {
		if tokens[i].dynamic {
			return true
		}
		executable := filepath.Base(tokens[i].text)
		switch executable {
		case "command":
			canUseReservedTime = false
			i++
			for i < len(tokens) && tokens[i].kind == wordToken && strings.HasPrefix(tokens[i].text, "-") {
				if tokens[i].dynamic {
					return true
				}
				if tokens[i].text == "-v" || tokens[i].text == "-V" {
					return false
				}
				i++
			}
		case "env":
			canUseReservedTime = false
			i++
			for i < len(tokens) && tokens[i].kind == wordToken {
				arg := tokens[i].text
				if isAssignment(arg) {
					i++
					continue
				}
				if tokens[i].dynamic && envOptionHasAttachedValue(arg) {
					if tokens[i].maySplit {
						return true
					}
					i++
					continue
				}
				if tokens[i].dynamic {
					return true
				}
				if splitValue, split, consumeNext := shortEnvOption(arg); split {
					return splitStringContainsGitPush(tokens[i+1:], splitValue, consumeNext)
				} else if consumeNext {
					var unsafe bool
					i, unsafe = optionValueEnd(tokens, i+1)
					if unsafe {
						return true
					}
					continue
				}
				if arg == "--split-string" {
					return splitStringContainsGitPush(tokens[i+1:], "", true)
				}
				if value, ok := strings.CutPrefix(arg, "--split-string="); ok {
					return splitStringContainsGitPush(tokens[i+1:], value, false)
				}
				if arg == "--unset" || arg == "--chdir" || arg == "--argv0" {
					var unsafe bool
					i, unsafe = optionValueEnd(tokens, i+1)
					if unsafe {
						return true
					}
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
		case "time":
			reserved := canUseReservedTime && tokens[i].text == "time"
			i = skipTimeOptions(tokens, i+1, reserved)
			canUseReservedTime = reserved
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

func optionValueEnd(tokens []shellToken, start int) (end int, unsafe bool) {
	i, ok := nextWordToken(tokens, start)
	if !ok {
		return len(tokens), false
	}
	return i + 1, tokens[i].maySplit
}

func reservedTimeAllowed(tokens []shellToken) bool {
	for _, token := range tokens {
		if token.kind == redirectToken {
			return false
		}
		if token.kind != wordToken {
			continue
		}
		if isAssignment(token.text) {
			return false
		}
		switch token.text {
		case "!", "{", "}", "if", "then", "elif", "else", "do", "while", "until":
			continue
		default:
			return true
		}
	}
	return true
}

func shortEnvOption(arg string) (splitValue string, split bool, consumeNext bool) {
	if len(arg) < 2 || arg[0] != '-' || strings.HasPrefix(arg, "--") {
		return "", false, false
	}
	options := arg[1:]
	for i, option := range options {
		switch option {
		case 'S':
			if i+1 < len(options) {
				return options[i+1:], true, false
			}
			return "", true, true
		case 'u', 'C', 'P':
			return "", false, i+1 == len(options)
		}
	}
	return "", false, false
}

func envOptionHasAttachedValue(arg string) bool {
	for _, prefix := range []string{"--unset=", "--chdir=", "--argv0="} {
		if strings.HasPrefix(arg, prefix) {
			return true
		}
	}
	return len(arg) > 2 && (strings.HasPrefix(arg, "-u") || strings.HasPrefix(arg, "-C") || strings.HasPrefix(arg, "-P"))
}

func splitStringContainsGitPush(tokens []shellToken, prefix string, consumeValue bool) bool {
	value := prefix
	remaining := tokens
	if consumeValue {
		for len(remaining) > 0 && remaining[0].kind != wordToken {
			remaining = remaining[1:]
		}
		if len(remaining) == 0 {
			return false
		}
		if remaining[0].dynamic {
			return true
		}
		value = remaining[0].text
		remaining = remaining[1:]
	}

	words, err := splitEnvString(value)
	if err != nil {
		return true
	}
	combined := make([]shellToken, 0, 1+len(words)+len(remaining))
	combined = append(combined, shellToken{text: "env", kind: wordToken})
	for _, word := range words {
		combined = append(combined, shellToken{text: word, kind: wordToken})
	}
	combined = append(combined, remaining...)
	if len(combined) == 0 {
		return false
	}
	return segmentContainsGitPush(combined)
}

func splitEnvString(value string) ([]string, error) {
	var words []string
	var current strings.Builder
	inWord := false
	quote := byte(0)
	flush := func() {
		if inWord {
			words = append(words, current.String())
			current.Reset()
			inWord = false
		}
	}

	for i := 0; i < len(value); i++ {
		ch := value[i]
		if quote != 0 {
			if ch == quote {
				quote = 0
				continue
			}
			if quote == '"' && ch == '\\' {
				if i+1 >= len(value) {
					return nil, errors.New("trailing escape in env split string")
				}
				i++
				if value[i] == 'c' {
					flush()
					return words, nil
				}
				if value[i] == '_' {
					current.WriteByte(' ')
				} else {
					current.WriteByte(value[i])
				}
				continue
			}
			if quote == '"' && ch == '$' && i+1 < len(value) && value[i+1] == '{' {
				return nil, errors.New("environment expansion in env split string")
			}
			current.WriteByte(ch)
			continue
		}

		switch ch {
		case '\'', '"':
			quote = ch
			inWord = true
		case '\\':
			if i+1 >= len(value) {
				return nil, errors.New("trailing escape in env split string")
			}
			i++
			if value[i] == 'c' {
				flush()
				return words, nil
			}
			if value[i] == '_' {
				flush()
			} else {
				current.WriteByte(value[i])
				inWord = true
			}
		case '$':
			if i+1 < len(value) && value[i+1] == '{' {
				return nil, errors.New("environment expansion in env split string")
			}
			current.WriteByte(ch)
			inWord = true
		case ' ', '\t', '\r', '\n':
			flush()
		default:
			current.WriteByte(ch)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, errors.New("unterminated quote in env split string")
	}
	flush()
	return words, nil
}

func skipTimeOptions(tokens []shellToken, start int, reserved bool) int {
	i := start
	if reserved {
		for i < len(tokens) && tokens[i].kind == wordToken && tokens[i].text == "-p" {
			i++
		}
		return i
	}
	for i < len(tokens) && tokens[i].kind == wordToken {
		if tokens[i].maySplit {
			return i
		}
		arg := tokens[i].text
		switch {
		case arg == "--":
			return i + 1
		case arg == "-o" || arg == "--output" || arg == "-f" || arg == "--format":
			end, unsafe := optionValueEnd(tokens, i+1)
			if unsafe {
				return end - 1
			}
			i = end
		case strings.HasPrefix(arg, "--output=") || strings.HasPrefix(arg, "--format="):
			i++
		case strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && shortTimeOptionConsumesValue(arg):
			end, unsafe := optionValueEnd(tokens, i+1)
			if unsafe {
				return end - 1
			}
			i = end
		case strings.HasPrefix(arg, "-"):
			i++
		default:
			return i
		}
	}
	return i
}

func shortTimeOptionConsumesValue(arg string) bool {
	for i, option := range arg[1:] {
		if option != 'o' && option != 'f' {
			continue
		}
		return i == len(arg[1:])-1
	}
	return false
}

func shellCommandArguments(tokens []shellToken) []shellToken {
	arguments := make([]shellToken, 0, len(tokens))
	for i := 0; i < len(tokens); i++ {
		if tokens[i].kind == redirectToken {
			if i+1 < len(tokens) && tokens[i+1].kind == wordToken {
				i++
			}
			continue
		}
		arguments = append(arguments, tokens[i])
	}
	return arguments
}

func shellCommandContainsGitPush(tokens []shellToken) bool {
	for i := 0; i < len(tokens); {
		if tokens[i].kind != wordToken {
			i++
			continue
		}
		arg := tokens[i].text
		if tokens[i].dynamic {
			if shellOptionHasAttachedValue(arg) && !tokens[i].maySplit {
				i++
				continue
			}
			return true
		}
		if arg == "--" {
			return false
		}
		if strings.HasPrefix(arg, "--") {
			i++
			if arg == "--rcfile" || arg == "--init-file" {
				var ok bool
				i, ok = nextWordToken(tokens, i)
				if !ok {
					return false
				}
				if tokens[i].maySplit {
					return true
				}
				i++
			}
			continue
		}
		if len(arg) < 2 || (arg[0] != '-' && arg[0] != '+') {
			return false
		}

		options := arg[1:]
		i++
		for _, option := range options {
			if option != 'o' && option != 'O' {
				continue
			}
			var ok bool
			i, ok = nextWordToken(tokens, i)
			if !ok {
				return false
			}
			if tokens[i].maySplit {
				return true
			}
			i++
		}
		if !strings.ContainsRune(options, 'c') {
			continue
		}
		var ok bool
		i, ok = nextWordToken(tokens, i)
		if !ok {
			return false
		}
		if tokens[i].dynamic {
			return true
		}
		push, err := containsGitPush(tokens[i].text)
		return err != nil || push
	}
	return false
}

func shellOptionHasAttachedValue(arg string) bool {
	return strings.HasPrefix(arg, "--rcfile=") || strings.HasPrefix(arg, "--init-file=")
}

func nextWordToken(tokens []shellToken, start int) (int, bool) {
	for i := start; i < len(tokens); i++ {
		if tokens[i].kind == wordToken {
			return i, true
		}
	}
	return 0, false
}

func evaluatedCommandContainsGitPush(tokens []shellToken) bool {
	var words []string
	for _, token := range tokens {
		if token.kind == wordToken {
			if token.dynamic {
				return true
			}
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

func shellParameterExpansionEnd(command string, start int) (end int, ok bool, err error) {
	if start+1 >= len(command) || command[start] != '$' {
		return 0, false, nil
	}
	next := command[start+1]
	if next == '{' {
		end, err := closingParameterExpansion(command, start+2)
		return end, true, err
	}
	if isShellNameStart(next) {
		end := start + 1
		for end+1 < len(command) && isShellNameContinue(command[end+1]) {
			end++
		}
		return end, true, nil
	}
	if (next >= '0' && next <= '9') || strings.ContainsRune("@*#?$!-_", rune(next)) {
		return start + 1, true, nil
	}
	return 0, false, nil
}

func parameterExpansionMaySplit(expansion string, quote byte) bool {
	if quote == 0 {
		return true
	}
	return expansion == "$@" || strings.Contains(expansion, "[@]")
}

func shellBraceExpansionEnd(command string, start int) (end int, ok bool) {
	depth := 0
	quote := byte(0)
	hasSeparator := false
	for i := start; i < len(command); i++ {
		ch := command[i]
		if quote != 0 {
			if ch == quote {
				quote = 0
			} else if quote == '"' && ch == '\\' {
				i++
			}
			continue
		}
		if ch == '\\' {
			i++
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			continue
		}
		switch ch {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, hasSeparator
			}
		case ',':
			if depth > 0 {
				hasSeparator = true
			}
		case '.':
			if depth > 0 && i+1 < len(command) && command[i+1] == '.' {
				hasSeparator = true
			}
		}
	}
	return 0, false
}

func closingParameterExpansion(command string, start int) (int, error) {
	depth := 1
	quote := byte(0)
	for i := start; i < len(command); i++ {
		ch := command[i]
		if quote == 0 && ch == '$' && i+1 < len(command) && command[i+1] == '\'' {
			_, end, ansiErr := parseANSIQuoted(command, i+2)
			if ansiErr != nil {
				return 0, ansiErr
			}
			i = end
			continue
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
				continue
			}
			if quote == '"' && ch == '\\' {
				i++
			}
			continue
		}
		if ch == '\\' {
			i++
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			continue
		}
		if ch == '$' && i+1 < len(command) && command[i+1] == '{' {
			depth++
			i++
			continue
		}
		if ch == '}' {
			depth--
			if depth == 0 {
				return i, nil
			}
		}
	}
	return 0, errors.New("unterminated parameter expansion")
}

func isShellNameStart(ch byte) bool {
	return ch == '_' || ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z'
}

func isShellNameContinue(ch byte) bool {
	return isShellNameStart(ch) || ch >= '0' && ch <= '9'
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
		if tokens[i].dynamic && (!gitOptionHasAttachedValue(arg) || tokens[i].maySplit) {
			return true
		}
		if arg == "--" {
			i++
			return i < len(tokens) && tokens[i].kind == wordToken && (tokens[i].dynamic || tokens[i].text == "push")
		}
		if strings.HasPrefix(arg, "-") {
			if optionsWithSeparateValue[arg] {
				if i+1 < len(tokens) && tokens[i+1].kind == wordToken && tokens[i+1].maySplit {
					return true
				}
				i++
			}
			continue
		}
		return arg == "push"
	}
	return false
}

func gitOptionHasAttachedValue(arg string) bool {
	for _, prefix := range []string{"--git-dir=", "--work-tree=", "--namespace=", "--config-env=", "--exec-path="} {
		if strings.HasPrefix(arg, prefix) {
			return true
		}
	}
	return len(arg) > 2 && (strings.HasPrefix(arg, "-C") || strings.HasPrefix(arg, "-c"))
}

func lexShell(command string) ([]shellToken, error) {
	var tokens []shellToken
	var current strings.Builder
	inWord := false
	quote := byte(0)
	currentDynamic := false
	currentMaySplit := false

	flushWord := func() {
		if !inWord {
			return
		}
		tokens = append(tokens, shellToken{
			text:     current.String(),
			kind:     wordToken,
			dynamic:  currentDynamic,
			maySplit: currentMaySplit,
		})
		current.Reset()
		inWord = false
		currentDynamic = false
		currentMaySplit = false
	}

	for i := 0; i < len(command); i++ {
		ch := command[i]
		if quote == 0 && ch == '$' && i+1 < len(command) && command[i+1] == '\'' {
			value, end, err := parseANSIQuoted(command, i+2)
			if err != nil {
				return nil, err
			}
			current.WriteString(value)
			inWord = true
			i = end
			continue
		}
		if quote != '\'' && strings.HasPrefix(command[i:], "$((") {
			end, err := closingCommandParenthesis(command, i+2)
			if err != nil {
				return nil, err
			}
			current.WriteString(dynamicWordPlaceholder)
			inWord = true
			i = end
			continue
		}
		if quote != '\'' && strings.HasPrefix(command[i:], "$(") {
			end, err := closingCommandParenthesis(command, i+2)
			if err != nil {
				return nil, err
			}
			current.WriteString(dynamicWordPlaceholder)
			currentDynamic = true
			currentMaySplit = currentMaySplit || quote == 0
			inWord = true
			i = end
			continue
		}
		if quote == 0 && (ch == '<' || ch == '>') && i+1 < len(command) && command[i+1] == '(' {
			end, err := closingCommandParenthesis(command, i+2)
			if err != nil {
				return nil, err
			}
			current.WriteString(dynamicWordPlaceholder)
			inWord = true
			i = end
			continue
		}
		if quote != '\'' && ch == '`' {
			end, err := closingBacktick(command, i+1)
			if err != nil {
				return nil, err
			}
			current.WriteString(dynamicWordPlaceholder)
			currentDynamic = true
			currentMaySplit = currentMaySplit || quote == 0
			inWord = true
			i = end
			continue
		}
		if quote == 0 && ch == '$' && i+1 < len(command) && command[i+1] == '"' {
			current.WriteString(dynamicWordPlaceholder)
			currentDynamic = true
			inWord = true
			continue
		}
		if quote != '\'' && ch == '$' {
			end, ok, err := shellParameterExpansionEnd(command, i)
			if err != nil {
				return nil, err
			}
			if ok {
				current.WriteString(dynamicWordPlaceholder)
				currentDynamic = true
				currentMaySplit = currentMaySplit || parameterExpansionMaySplit(command[i:end+1], quote)
				inWord = true
				i = end
				continue
			}
		}
		if quote == 0 && ch == '{' {
			end, ok := shellBraceExpansionEnd(command, i)
			if ok {
				current.WriteString(dynamicWordPlaceholder)
				currentDynamic = true
				currentMaySplit = true
				inWord = true
				i = end
				continue
			}
		}
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
			if ch == '*' || ch == '?' || ch == '[' {
				currentDynamic = true
				currentMaySplit = true
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

func parseANSIQuoted(command string, start int) (string, int, error) {
	var value strings.Builder
	truncated := false
	writeByte := func(ch byte) {
		if truncated {
			return
		}
		if ch == 0 {
			truncated = true
			return
		}
		value.WriteByte(ch)
	}
	writeRune := func(char rune) {
		if truncated {
			return
		}
		if char == 0 {
			truncated = true
			return
		}
		value.WriteRune(char)
	}
	for i := start; i < len(command); i++ {
		ch := command[i]
		if ch == '\'' {
			return value.String(), i, nil
		}
		if ch != '\\' {
			writeByte(ch)
			continue
		}
		if i+1 >= len(command) {
			return "", 0, errors.New("trailing escape in ANSI-C quoted string")
		}
		i++
		escaped := command[i]
		switch escaped {
		case 'a':
			writeByte('\a')
		case 'b':
			writeByte('\b')
		case 'c':
			if i+1 >= len(command) {
				return "", 0, errors.New("incomplete control escape in ANSI-C quoted string")
			}
			i++
			control := command[i] & 0x1f
			if command[i] == '?' {
				control = 0x7f
			}
			writeByte(control)
		case 'e', 'E':
			writeByte(0x1b)
		case 'f':
			writeByte('\f')
		case 'n':
			writeByte('\n')
		case 'r':
			writeByte('\r')
		case 't':
			writeByte('\t')
		case 'v':
			writeByte('\v')
		case '\\', '\'', '"':
			writeByte(escaped)
		case '\n':
			// A backslash-newline pair is removed by the shell.
		case 'x':
			decoded, consumed, err := parseEscapedNumber(command[i+1:], 16, 2)
			if err != nil {
				return "", 0, err
			}
			writeByte(byte(decoded))
			i += consumed
		case 'u':
			decoded, consumed, err := parseEscapedNumber(command[i+1:], 16, 4)
			if err != nil {
				return "", 0, err
			}
			writeRune(rune(decoded))
			i += consumed
		case 'U':
			decoded, consumed, err := parseEscapedNumber(command[i+1:], 16, 8)
			if err != nil {
				return "", 0, err
			}
			writeRune(rune(decoded))
			i += consumed
		default:
			if escaped >= '0' && escaped <= '7' {
				decoded, consumed, err := parseEscapedNumber(command[i:], 8, 3)
				if err != nil {
					return "", 0, err
				}
				writeByte(byte(decoded))
				i += consumed - 1
			} else {
				writeByte('\\')
				writeByte(escaped)
			}
		}
	}
	return "", 0, errors.New("unterminated ANSI-C quoted string")
}

func parseEscapedNumber(input string, base, limit int) (uint64, int, error) {
	length := 0
	for length < len(input) && length < limit {
		ch := input[length]
		valid := ch >= '0' && ch <= '7'
		if base == 16 {
			valid = valid || ch >= '8' && ch <= '9' || ch >= 'a' && ch <= 'f' || ch >= 'A' && ch <= 'F'
		}
		if !valid {
			break
		}
		length++
	}
	if length == 0 {
		return 0, 0, errors.New("invalid numeric escape in ANSI-C quoted string")
	}
	value, err := strconv.ParseUint(input[:length], base, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("parse ANSI-C escape: %w", err)
	}
	return value, length, nil
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
