// Command agent-hook provides the repository-local hooks used by coding agents.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const (
	exitOK                 = 0
	exitBlock              = 2
	dynamicWordPlaceholder = "__AGENT_HOOK_DYNAMIC__"
	dynamicGitAlias        = "__AGENT_HOOK_DYNAMIC_GIT_ALIAS__"
	dynamicGitAliasName    = "__AGENT_HOOK_ANY_GIT_ALIAS__"
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

type shellRedirect struct {
	operator string
	target   shellToken
}

type shellInputKind uint8

const (
	shellInputNone shellInputKind = iota
	shellInputScript
	shellInputUnknown
)

type shellInputSource struct {
	kind   shellInputKind
	script string
}

type shellGroup struct {
	start     int
	end       int
	redirects []shellRedirect
}

type shellFunction struct {
	body      []shellToken
	redirects []shellRedirect
}

type shellFunctionDefinition struct {
	name     string
	function *shellFunction
	end      int
}

type shellFunctionState struct {
	definitions map[string]*shellFunction
	active      map[*shellFunction]bool
	depth       int
}

type shellTokenRange struct {
	start       int
	end         int
	beforeGroup int
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
	if !ok && strings.EqualFold(filepath.Base(input.ToolName), "monitor") {
		if _, hasWebSocket := fields["ws"]; hasWebSocket {
			return input, "", false, nil
		}
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
	case "bash", "monitor", "shell", "exec_command", "shell_command", "terminal":
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
	return containsGitPushWithContext(command, nil, nil)
}

func containsGitPushWithContext(command string, inheritedInputs map[int]shellInputSource, inheritedGitAliases map[string]string) (bool, error) {
	hereDocBodies := make(map[string]string)
	command, hereDocScripts, err := stripHereDocBodiesAndCapture(command, hereDocBodies)
	if err != nil {
		return false, err
	}

	nested, err := commandSubstitutions(command)
	if err != nil {
		return false, err
	}
	nested = append(nested, hereDocScripts...)
	for _, script := range nested {
		push, nestedErr := containsGitPushWithContext(script, inheritedInputs, inheritedGitAliases)
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
	functions := &shellFunctionState{
		definitions: make(map[string]*shellFunction),
		active:      make(map[*shellFunction]bool),
	}
	return shellTokensContainGitPush(tokens, hereDocBodies, inheritedInputs, inheritedGitAliases, functions), nil
}

func shellTokensContainGitPush(tokens []shellToken, hereDocBodies map[string]string, inheritedInputs map[int]shellInputSource, inheritedGitAliases map[string]string, functions *shellFunctionState) bool {
	groups := shellGroups(tokens)
	pipeRanges := shellPipelineRanges(tokens, groups)
	definitions := shellFunctionDefinitions(tokens, groups)
	for start := 0; start < len(tokens); {
		if tokens[start].kind == controlToken {
			start++
			continue
		}
		if definition, ok := definitions[start]; ok {
			functions.definitions[definition.name] = definition.function
			start = definition.end
			continue
		}
		end := start
		for end < len(tokens) && tokens[end].kind != controlToken {
			end++
		}
		inputs := shellSegmentInputs(inheritedInputs, groups, pipeRanges, start, end, hereDocBodies)
		if segmentContainsGitPushWithFunctions(tokens[start:end], hereDocBodies, inputs, inheritedGitAliases, functions) {
			return true
		}
		start = end
	}
	return false
}

func shellGroups(tokens []shellToken) []shellGroup {
	type groupStart struct {
		index int
		kind  string
	}
	var stack []groupStart
	var groups []shellGroup
	for i, token := range tokens {
		kind := ""
		opening := false
		switch {
		case token.kind == controlToken && token.text == "(":
			kind, opening = "(", true
		case token.kind == wordToken && token.text == "{":
			kind, opening = "{", true
		case token.kind == wordToken && token.text == "if":
			kind, opening = "if", true
		case token.kind == wordToken && (token.text == "for" || token.text == "select" || token.text == "until" || token.text == "while"):
			kind, opening = "loop", true
		case token.kind == wordToken && token.text == "case":
			kind, opening = "case", true
		case token.kind == controlToken && token.text == ")":
			kind = "("
		case token.kind == wordToken && token.text == "}":
			kind = "{"
		case token.kind == wordToken && token.text == "fi":
			kind = "if"
		case token.kind == wordToken && token.text == "done":
			kind = "loop"
		case token.kind == wordToken && token.text == "esac":
			kind = "case"
		default:
			continue
		}
		if opening {
			stack = append(stack, groupStart{index: i, kind: kind})
			continue
		}
		if len(stack) == 0 || stack[len(stack)-1].kind != kind {
			continue
		}
		start := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		redirectEnd := i + 1
		for redirectEnd < len(tokens) && tokens[redirectEnd].kind != controlToken {
			redirectEnd++
		}
		groups = append(groups, shellGroup{
			start:     start.index,
			end:       i,
			redirects: shellRedirections(tokens[i+1 : redirectEnd]),
		})
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].start == groups[j].start {
			return groups[i].end > groups[j].end
		}
		return groups[i].start < groups[j].start
	})
	return groups
}

func shellFunctionDefinitions(tokens []shellToken, groups []shellGroup) map[int]shellFunctionDefinition {
	definitions := make(map[int]shellFunctionDefinition)
	for _, group := range groups {
		if group.start >= len(tokens) || tokens[group.start].kind != wordToken || tokens[group.start].text != "{" {
			continue
		}
		headerStart, name, ok := shellFunctionHeader(tokens, group.start)
		if !ok {
			continue
		}
		statementStart := 0
		for i := headerStart - 1; i >= 0; i-- {
			if tokens[i].kind == controlToken {
				statementStart = i + 1
				break
			}
		}
		if skipCommandPrefixes(tokens[statementStart:headerStart], 0, true) != headerStart-statementStart {
			continue
		}
		end := group.end + 1
		for end < len(tokens) && tokens[end].kind != controlToken {
			end++
		}
		definitions[statementStart] = shellFunctionDefinition{
			name: name,
			function: &shellFunction{
				body:      tokens[group.start+1 : group.end],
				redirects: group.redirects,
			},
			end: end,
		}
	}
	return definitions
}

func shellFunctionHeader(tokens []shellToken, braceStart int) (start int, name string, ok bool) {
	staticWord := func(index int, text string) bool {
		return index >= 0 && index < len(tokens) && tokens[index].kind == wordToken && !tokens[index].dynamic && tokens[index].text == text
	}
	parenthesis := func(index int, text string) bool {
		return index >= 0 && index < len(tokens) && tokens[index].kind == controlToken && tokens[index].text == text
	}
	identifier := func(index int) bool {
		return index >= 0 && index < len(tokens) && tokens[index].kind == wordToken && !tokens[index].dynamic && isShellIdentifier(tokens[index].text)
	}
	functionName := func(index int) bool {
		return index >= 0 && index < len(tokens) && tokens[index].kind == wordToken && !tokens[index].dynamic && tokens[index].text != ""
	}
	previous := func(index int) int {
		index--
		for index >= 0 && tokens[index].kind == controlToken && tokens[index].text == "\n" {
			index--
		}
		return index
	}

	last := previous(braceStart)
	if parenthesis(last, ")") {
		open := previous(last)
		nameIndex := previous(open)
		keyword := previous(nameIndex)
		if parenthesis(open, "(") && functionName(nameIndex) && staticWord(keyword, "function") {
			return keyword, tokens[nameIndex].text, true
		}
		if parenthesis(open, "(") && identifier(nameIndex) {
			return nameIndex, tokens[nameIndex].text, true
		}
		return 0, "", false
	}
	keyword := previous(last)
	if functionName(last) && staticWord(keyword, "function") {
		return keyword, tokens[last].text, true
	}
	return 0, "", false
}

func shellPipelineRanges(tokens []shellToken, groups []shellGroup) []shellTokenRange {
	groupByStart := make(map[int]shellGroup, len(groups))
	for _, group := range groups {
		groupByStart[group.start] = group
	}
	var ranges []shellTokenRange
	for i, token := range tokens {
		if token.kind != controlToken || token.text != "|" && token.text != "|&" {
			continue
		}
		start := i + 1
		for start < len(tokens) && tokens[start].kind == controlToken && tokens[start].text == "\n" {
			start++
		}
		if start >= len(tokens) {
			continue
		}
		if group, ok := groupByStart[start]; ok {
			ranges = append(ranges, shellTokenRange{start: start + 1, end: group.end, beforeGroup: group.start})
			continue
		}
		end := start
		for end < len(tokens) && tokens[end].kind != controlToken {
			end++
		}
		ranges = append(ranges, shellTokenRange{start: start, end: end, beforeGroup: -1})
	}
	return ranges
}

func shellSegmentInputs(inherited map[int]shellInputSource, groups []shellGroup, pipeRanges []shellTokenRange, start, end int, hereDocBodies map[string]string) map[int]shellInputSource {
	inputs := cloneShellInputSources(inherited)
	for _, group := range groups {
		if rangesOverlap(start, end, group.start+1, group.end) {
			for _, pipeRange := range pipeRanges {
				if pipeRange.beforeGroup == group.start && rangesOverlap(start, end, pipeRange.start, pipeRange.end) {
					inputs[0] = shellInputSource{kind: shellInputUnknown}
				}
			}
			inputs = resolveShellInputSources(inputs, group.redirects, hereDocBodies)
		}
	}
	for _, pipeRange := range pipeRanges {
		if pipeRange.beforeGroup < 0 && rangesOverlap(start, end, pipeRange.start, pipeRange.end) {
			inputs[0] = shellInputSource{kind: shellInputUnknown}
			break
		}
	}
	return inputs
}

func rangesOverlap(leftStart, leftEnd, rightStart, rightEnd int) bool {
	return leftStart < rightEnd && rightStart < leftEnd
}

func cloneShellInputSources(sources map[int]shellInputSource) map[int]shellInputSource {
	cloned := make(map[int]shellInputSource, len(sources))
	maps.Copy(cloned, sources)
	return cloned
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
	wordParentheses := []bool{true}
	quote := byte(0)
	inWord := false
	for i := start; i < len(command); i++ {
		ch := command[i]
		if quote == 0 && ch == '$' && i+1 < len(command) && command[i+1] == '\'' {
			_, end, ansiErr := parseANSIQuoted(command, i+2)
			if ansiErr != nil {
				return 0, ansiErr
			}
			i = end
			inWord = true
			continue
		}
		if quote == '\'' {
			if ch == '\'' {
				quote = 0
			}
			continue
		}
		if ch == '\\' {
			if i+1 < len(command) && command[i+1] != '\n' {
				inWord = true
			}
			i++
			continue
		}
		if ch == '\'' && quote == 0 {
			quote = '\''
			inWord = true
			continue
		}
		if ch == '"' {
			switch quote {
			case '"':
				quote = 0
			case 0:
				quote = '"'
				inWord = true
			}
			continue
		}
		if quote != 0 {
			continue
		}
		if ch == '#' && !inWord {
			newline := strings.IndexByte(command[i:], '\n')
			if newline < 0 {
				return 0, errors.New("unterminated command substitution")
			}
			i += newline
			inWord = false
			continue
		}
		if ch == '`' {
			end, err := closingBacktick(command, i+1)
			if err != nil {
				return 0, err
			}
			i = end
			inWord = true
			continue
		}
		switch ch {
		case '(':
			depth++
			wordParentheses = append(wordParentheses, i > 0 && (command[i-1] == '$' || command[i-1] == '<' || command[i-1] == '>'))
			inWord = false
		case ')':
			depth--
			closedWord := wordParentheses[len(wordParentheses)-1]
			wordParentheses = wordParentheses[:len(wordParentheses)-1]
			if depth == 0 {
				return i, nil
			}
			inWord = closedWord
		case ' ', '\t', '\r', '\n', ';', '&', '|', '<', '>':
			inWord = false
		default:
			inWord = true
		}
	}
	return 0, errors.New("unterminated command substitution")
}

func segmentContainsGitPush(tokens []shellToken, hereDocBodies map[string]string, inheritedInputs map[int]shellInputSource, inheritedGitAliases map[string]string) bool {
	functions := &shellFunctionState{
		definitions: make(map[string]*shellFunction),
		active:      make(map[*shellFunction]bool),
	}
	return segmentContainsGitPushWithFunctions(tokens, hereDocBodies, inheritedInputs, inheritedGitAliases, functions)
}

func segmentContainsGitPushWithFunctions(tokens []shellToken, hereDocBodies map[string]string, inheritedInputs map[int]shellInputSource, inheritedGitAliases map[string]string, functions *shellFunctionState) bool {
	canUseReservedTime := reservedTimeAllowed(tokens)
	canUseShellBuiltin := true
	canUseShellFunction := true
	gitEnvironment := make(map[string]shellToken)
	shellEnvironment := make(map[string]shellToken)
	redirects := shellRedirections(tokens)
	inputSources := resolveShellInputSources(inheritedInputs, redirects, hereDocBodies)
	tokens = shellCommandArguments(tokens)
	i := skipCommandPrefixes(tokens, 0, true)
	recordGitEnvironmentAssignments(gitEnvironment, tokens[:i])
	recordShellEnvironmentAssignments(shellEnvironment, tokens[:i])
	if i >= len(tokens) || tokens[i].kind != wordToken {
		return false
	}

	for {
		if tokens[i].dynamic {
			return true
		}
		if canUseShellFunction && tokens[i].text != "time" && tokens[i].text != "coproc" {
			if function, ok := functions.definitions[tokens[i].text]; ok {
				aliases := maps.Clone(inheritedGitAliases)
				if aliases == nil {
					aliases = make(map[string]string)
				}
				maps.Copy(aliases, gitAliasesFromEnvironment(gitEnvironment))
				return shellFunctionContainsGitPush(function, hereDocBodies, inputSources, aliases, functions)
			}
		}
		executable := filepath.Base(tokens[i].text)
		allowAssignmentPrefixes := false
		switch executable {
		case "command":
			canUseReservedTime = false
			canUseShellFunction = false
			var lookup, unsafe, valid bool
			i, lookup, unsafe, valid = commandCommandIndex(tokens, i+1)
			if unsafe {
				return true
			}
			if lookup || !valid {
				return false
			}
			// macOS provides an external command utility that can dispatch shell
			// builtins. Treat it like the builtin here so wrapped trap and exec
			// invocations still fail closed on every supported platform.
			canUseShellBuiltin = true
		case "env":
			canUseReservedTime = false
			canUseShellBuiltin = false
			canUseShellFunction = false
			i++
			for i < len(tokens) && tokens[i].kind == wordToken {
				arg := tokens[i].text
				if !tokens[i].dynamic && envClearsEnvironment(arg) {
					clear(gitEnvironment)
					clear(shellEnvironment)
					inheritedGitAliases = nil
				}
				if !tokens[i].dynamic {
					if name, consumeNext, unset := envUnsetOption(arg); unset {
						if consumeNext {
							var ok bool
							i, ok = nextWordToken(tokens, i+1)
							if !ok {
								return false
							}
							if tokens[i].maySplit || tokens[i].dynamic && (len(gitEnvironment) > 0 || len(shellEnvironment) > 0) {
								return true
							}
							if tokens[i].dynamic {
								i++
								continue
							}
							name = tokens[i].text
						}
						delete(gitEnvironment, name)
						delete(shellEnvironment, name)
						i++
						continue
					}
				}
				if isAssignment(arg) {
					recordGitEnvironmentAssignment(gitEnvironment, tokens[i])
					recordShellEnvironmentAssignment(shellEnvironment, tokens[i])
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
					return splitStringContainsGitPush(tokens[i+1:], splitValue, consumeNext, redirects, hereDocBodies, inheritedInputs, inheritedGitAliases, gitEnvironment, shellEnvironment)
				} else if consumeNext {
					var unsafe bool
					i, unsafe = optionValueEnd(tokens, i+1)
					if unsafe {
						return true
					}
					continue
				}
				if arg == "--split-string" {
					return splitStringContainsGitPush(tokens[i+1:], "", true, redirects, hereDocBodies, inheritedInputs, inheritedGitAliases, gitEnvironment, shellEnvironment)
				}
				if value, ok := strings.CutPrefix(arg, "--split-string="); ok {
					return splitStringContainsGitPush(tokens[i+1:], value, false, redirects, hereDocBodies, inheritedInputs, inheritedGitAliases, gitEnvironment, shellEnvironment)
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
		case "coproc":
			if !canUseReservedTime || tokens[i].text != "coproc" {
				return false
			}
			i++
			if i+1 < len(tokens) && tokens[i].kind == wordToken && tokens[i+1].kind == wordToken && tokens[i+1].text == "{" && isShellIdentifier(tokens[i].text) {
				i++
			}
			allowAssignmentPrefixes = true
		case "exec":
			if !canUseShellBuiltin || tokens[i].text != "exec" {
				return false
			}
			canUseReservedTime = false
			canUseShellBuiltin = false
			canUseShellFunction = false
			var unsafe, valid bool
			i, unsafe, valid = execCommandIndex(tokens, i+1)
			if unsafe {
				return true
			}
			if !valid {
				return false
			}
		case "sh", "bash", "dash", "ksh", "zsh":
			_, posixEnvironment := shellEnvironment["POSIXLY_CORRECT"]
			startupPush := executable == "bash" && !posixEnvironment && shellEnvironmentContainsGitPush(shellEnvironment, inputSources, inheritedGitAliases)
			return shellCommandContainsGitPush(tokens[i+1:], inputSources, inheritedGitAliases, startupPush)
		case "eval":
			if !canUseShellBuiltin || tokens[i].text != "eval" {
				return false
			}
			return evaluatedCommandContainsGitPush(tokens[i+1:], inputSources, inheritedGitAliases)
		case ".", "source":
			if !canUseShellBuiltin || tokens[i].text != executable {
				return false
			}
			return sourcedScriptContainsGitPush(tokens[i+1:], inputSources, inheritedGitAliases)
		case "builtin":
			if !canUseShellBuiltin || tokens[i].text != "builtin" {
				return false
			}
			canUseReservedTime = false
			canUseShellFunction = false
			i++
			if i < len(tokens) && tokens[i].kind == wordToken && tokens[i].text == "--" && !tokens[i].dynamic {
				i++
			}
			if i >= len(tokens) || tokens[i].kind != wordToken {
				return false
			}
			if tokens[i].dynamic {
				return true
			}
			switch tokens[i].text {
			case ".", "builtin", "command", "eval", "exec", "source", "trap":
				// Continue through the wrapper loop for builtins whose arguments
				// can execute a command string or replace the current process.
			default:
				return false
			}
		case "trap":
			if !canUseShellBuiltin || tokens[i].text != "trap" {
				return false
			}
			return trapContainsGitPush(tokens[i+1:], inputSources, inheritedGitAliases)
		case "time":
			reserved := canUseReservedTime && tokens[i].text == "time"
			i = skipTimeOptions(tokens, i+1, reserved)
			canUseReservedTime = reserved
			canUseShellBuiltin = reserved
			canUseShellFunction = reserved
			allowAssignmentPrefixes = reserved
		default:
			if !isGitExecutable(tokens[i].text) {
				return false
			}
			aliases := maps.Clone(inheritedGitAliases)
			if aliases == nil {
				aliases = make(map[string]string)
			}
			maps.Copy(aliases, gitAliasesFromEnvironment(gitEnvironment))
			return gitArgsContainPush(tokens[i+1:], aliases, inputSources)
		}

		start := i
		i = skipCommandPrefixes(tokens, i, allowAssignmentPrefixes)
		if allowAssignmentPrefixes {
			recordGitEnvironmentAssignments(gitEnvironment, tokens[start:i])
			recordShellEnvironmentAssignments(shellEnvironment, tokens[start:i])
		}
		if i >= len(tokens) || tokens[i].kind != wordToken {
			return false
		}
	}
}

func shellFunctionContainsGitPush(function *shellFunction, hereDocBodies map[string]string, inputSources map[int]shellInputSource, gitAliases map[string]string, functions *shellFunctionState) bool {
	if functions.active[function] {
		return false
	}
	if functions.depth >= 64 {
		return true
	}
	functions.active[function] = true
	functions.depth++
	defer func() {
		functions.depth--
		delete(functions.active, function)
	}()

	inputs := resolveShellInputSources(inputSources, function.redirects, hereDocBodies)
	return shellTokensContainGitPush(function.body, hereDocBodies, inputs, gitAliases, functions)
}

func sourcedScriptContainsGitPush(tokens []shellToken, inputSources map[int]shellInputSource, gitAliases map[string]string) bool {
	i, ok := nextWordToken(tokens, 0)
	if !ok {
		return false
	}
	path := tokens[i]
	if path.dynamic || strings.Contains(path.text, dynamicWordPlaceholder) {
		return true
	}
	if path.text == "/dev/null" {
		return false
	}
	fd, ok := shellInputFileDescriptor(path.text)
	if !ok {
		return true
	}
	return shellInputSourceContainsGitPush(fd, inputSources, gitAliases)
}

func shellInputSourceContainsGitPush(fd int, inputSources map[int]shellInputSource, gitAliases map[string]string) bool {
	source := inputSources[fd]
	if source.kind == shellInputUnknown {
		return true
	}
	if source.kind != shellInputScript {
		return false
	}
	childInputs := cloneShellInputSources(inputSources)
	delete(childInputs, fd)
	push, err := containsGitPushWithContext(source.script, childInputs, gitAliases)
	return err != nil || push
}

func trapContainsGitPush(tokens []shellToken, inputSources map[int]shellInputSource, gitAliases map[string]string) bool {
	i, ok := nextWordToken(tokens, 0)
	if !ok {
		return false
	}
	optionsEnded := !tokens[i].dynamic && tokens[i].text == "--"
	if optionsEnded {
		i, ok = nextWordToken(tokens, i+1)
		if !ok {
			return false
		}
	}
	if _, hasSignal := nextWordToken(tokens, i+1); !hasSignal {
		return false
	}
	action := tokens[i]
	if action.dynamic {
		return true
	}
	if action.text == "" || action.text == "-" {
		return false
	}
	if !optionsEnded && strings.HasPrefix(action.text, "-") {
		return false
	}
	push, err := containsGitPushWithContext(action.text, inputSources, gitAliases)
	return err != nil || push
}

func commandCommandIndex(tokens []shellToken, start int) (index int, lookup, unsafe, valid bool) {
	for i := start; i < len(tokens) && tokens[i].kind == wordToken; i++ {
		arg := tokens[i]
		if arg.dynamic {
			return i, false, true, true
		}
		if arg.text == "--" {
			return i + 1, false, false, true
		}
		if arg.text == "-" || !strings.HasPrefix(arg.text, "-") {
			return i, false, false, true
		}
		if strings.HasPrefix(arg.text, "--") {
			return i, false, false, false
		}
		for _, option := range arg.text[1:] {
			switch option {
			case 'p':
			case 'v', 'V':
				lookup = true
			default:
				return i, false, false, false
			}
		}
		if lookup {
			return i + 1, true, false, true
		}
	}
	return len(tokens), false, false, true
}

func execCommandIndex(tokens []shellToken, start int) (index int, unsafe, valid bool) {
	i := start
	for i < len(tokens) && tokens[i].kind == wordToken {
		arg := tokens[i]
		if arg.dynamic {
			return i, true, true
		}
		if arg.text == "--" {
			return i + 1, false, true
		}
		if arg.text == "-" || !strings.HasPrefix(arg.text, "-") {
			return i, false, true
		}
		if strings.HasPrefix(arg.text, "--") {
			return i, false, false
		}

		options := arg.text[1:]
		for optionIndex, option := range options {
			switch option {
			case 'c', 'l':
			case 'a':
				if optionIndex+1 < len(options) {
					i++
				} else {
					var valueUnsafe bool
					i, valueUnsafe = optionValueEnd(tokens, i+1)
					if valueUnsafe {
						return i, true, true
					}
				}
			default:
				return i, false, false
			}
			if option == 'a' {
				break
			}
		}
		if strings.ContainsRune(options, 'a') {
			continue
		}
		i++
	}
	return i, false, true
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

func envClearsEnvironment(arg string) bool {
	if arg == "--ignore-environment" {
		return true
	}
	if len(arg) < 2 || arg[0] != '-' || strings.HasPrefix(arg, "--") {
		return false
	}
	for _, option := range arg[1:] {
		if option == 'i' {
			return true
		}
		if option == 'S' || option == 'u' || option == 'C' || option == 'P' {
			return false
		}
	}
	return false
}

func envUnsetOption(arg string) (name string, consumeNext, ok bool) {
	if arg == "--unset" {
		return "", true, true
	}
	if name, attached := strings.CutPrefix(arg, "--unset="); attached {
		return name, false, true
	}
	if len(arg) < 2 || arg[0] != '-' || strings.HasPrefix(arg, "--") {
		return "", false, false
	}
	options := arg[1:]
	for i, option := range options {
		if option == 'u' {
			if i+1 == len(options) {
				return "", true, true
			}
			return options[i+1:], false, true
		}
		if option == 'S' || option == 'C' || option == 'P' {
			return "", false, false
		}
	}
	return "", false, false
}

func splitStringContainsGitPush(tokens []shellToken, prefix string, consumeValue bool, redirects []shellRedirect, hereDocBodies map[string]string, inheritedInputs map[int]shellInputSource, inheritedGitAliases map[string]string, gitEnvironment, shellEnvironment map[string]shellToken) bool {
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
	combined := make([]shellToken, 0, 1+len(gitEnvironment)+len(shellEnvironment)+len(words)+len(remaining))
	combined = append(combined, shellToken{text: "env", kind: wordToken})
	combined = append(combined, environmentAssignmentTokens(gitEnvironment)...)
	combined = append(combined, environmentAssignmentTokens(shellEnvironment)...)
	for _, word := range words {
		combined = append(combined, shellToken{text: word, kind: wordToken})
	}
	combined = append(combined, remaining...)
	for _, redirect := range redirects {
		combined = append(combined,
			shellToken{text: redirect.operator, kind: redirectToken},
			redirect.target,
		)
	}
	if len(combined) == 0 {
		return false
	}
	return segmentContainsGitPush(combined, hereDocBodies, inheritedInputs, inheritedGitAliases)
}

func environmentAssignmentTokens(environment map[string]shellToken) []shellToken {
	names := make([]string, 0, len(environment))
	for name := range environment {
		names = append(names, name)
	}
	sort.Strings(names)
	tokens := make([]shellToken, 0, len(names))
	for _, name := range names {
		token := environment[name]
		token.text = name + "=" + token.text
		tokens = append(tokens, token)
	}
	return tokens
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

func shellRedirections(tokens []shellToken) []shellRedirect {
	var redirects []shellRedirect
	for i := 0; i < len(tokens); i++ {
		if tokens[i].kind != redirectToken || i+1 >= len(tokens) || tokens[i+1].kind != wordToken {
			continue
		}
		redirects = append(redirects, shellRedirect{operator: tokens[i].text, target: tokens[i+1]})
		i++
	}
	return redirects
}

func shellCommandContainsGitPush(tokens []shellToken, inputSources map[int]shellInputSource, gitAliases map[string]string, environmentStartupPush bool) bool {
	stdinMode := false
	commandStringMode := false
	noExec := false
	interactive := false
	privileged := false
	posixMode := false
	startupContainsPush := false
	environmentStartupActive := func() bool {
		return environmentStartupPush && !interactive && !privileged && !posixMode
	}
	inputContainsPush := func(source shellInputSource) bool {
		return !noExec && (environmentStartupActive() || interactive && startupContainsPush || shellInputContainsGitPush(source))
	}
	startupPush := func() bool {
		return !noExec && (environmentStartupActive() || interactive && startupContainsPush)
	}
	commandStringContainsPush := func(command shellToken) bool {
		if noExec {
			return false
		}
		if environmentStartupActive() || interactive && startupContainsPush || command.dynamic {
			return true
		}
		push, err := containsGitPushWithContext(command.text, inputSources, gitAliases)
		return err != nil || push
	}
	for i := 0; i < len(tokens); {
		if tokens[i].kind != wordToken {
			i++
			continue
		}
		arg := tokens[i].text
		if stdinMode && !commandStringMode && (tokens[i].dynamic || len(arg) < 2 || (arg[0] != '-' && arg[0] != '+')) {
			return inputContainsPush(inputSources[0])
		}
		if tokens[i].dynamic {
			if commandStringMode {
				return commandStringContainsPush(tokens[i])
			}
			if shellOptionHasAttachedValue(arg) && !tokens[i].maySplit {
				i++
				continue
			}
			return true
		}
		if arg == "--" {
			i++
			if commandStringMode {
				nextIndex, ok := nextWordToken(tokens, i)
				if !ok {
					return false
				}
				i = nextIndex
				return commandStringContainsPush(tokens[i])
			}
			if stdinMode || i >= len(tokens) {
				return inputContainsPush(inputSources[0])
			}
			if tokens[i].dynamic {
				return true
			}
			if strings.Contains(tokens[i].text, dynamicWordPlaceholder) {
				return !noExec
			}
			if fd, ok := shellInputFileDescriptor(tokens[i].text); ok {
				return inputContainsPush(inputSources[fd])
			}
			return startupPush()
		}
		if strings.HasPrefix(arg, "--") {
			if arg == "--help" || arg == "--version" {
				return false
			}
			if arg == "--noexec" {
				noExec = true
			}
			if arg == "--interactive" {
				interactive = true
			}
			if arg == "--privileged" {
				privileged = true
			}
			if arg == "--posix" {
				posixMode = true
			}
			for _, prefix := range []string{"--rcfile=", "--init-file="} {
				if value, attached := strings.CutPrefix(arg, prefix); attached {
					if strings.Contains(value, dynamicWordPlaceholder) {
						startupContainsPush = true
					} else if fd, inputFile := shellInputFileDescriptor(value); inputFile {
						startupContainsPush = startupContainsPush || shellInputContainsGitPush(inputSources[fd])
					}
				}
			}
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
				if strings.Contains(tokens[i].text, dynamicWordPlaceholder) {
					startupContainsPush = true
				}
				if fd, inputFile := shellInputFileDescriptor(tokens[i].text); inputFile {
					startupContainsPush = startupContainsPush || shellInputContainsGitPush(inputSources[fd])
				}
				i++
			}
			continue
		}
		if arg == "-" {
			if commandStringMode {
				i++
				continue
			}
			return inputContainsPush(inputSources[0])
		}
		if arg == "+" && commandStringMode {
			i++
			continue
		}
		if len(arg) < 2 || (arg[0] != '-' && arg[0] != '+') {
			if commandStringMode {
				return commandStringContainsPush(tokens[i])
			}
			if strings.Contains(arg, dynamicWordPlaceholder) {
				return !noExec
			}
			if fd, ok := shellInputFileDescriptor(arg); ok {
				return inputContainsPush(inputSources[fd])
			}
			return startupPush()
		}

		options := arg[1:]
		if strings.ContainsRune(options, 'n') {
			noExec = arg[0] == '-'
		}
		if strings.ContainsRune(options, 'i') {
			interactive = arg[0] == '-'
		}
		if strings.ContainsRune(options, 'p') {
			privileged = arg[0] == '-'
		}
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
			if option == 'o' && tokens[i].text == "noexec" {
				noExec = arg[0] == '-'
			}
			if option == 'o' && tokens[i].text == "posix" {
				posixMode = arg[0] == '-'
			}
			if option == 'o' && tokens[i].text == "privileged" {
				privileged = arg[0] == '-'
			}
			i++
		}
		if !strings.ContainsRune(options, 'c') {
			if arg[0] == '-' && strings.ContainsRune(options, 's') {
				stdinMode = true
			}
			continue
		}
		if arg[0] != '-' {
			continue
		}
		commandStringMode = true
	}
	if commandStringMode {
		return false
	}
	return inputContainsPush(inputSources[0])
}

func resolveShellInputSources(inherited map[int]shellInputSource, redirects []shellRedirect, hereDocBodies map[string]string) map[int]shellInputSource {
	sources := cloneShellInputSources(inherited)
	for _, redirect := range redirects {
		fd, operator, input := shellInputRedirect(redirect.operator)
		if !input {
			continue
		}
		switch operator {
		case "<<<":
			if redirect.target.dynamic {
				sources[fd] = shellInputSource{kind: shellInputUnknown}
			} else {
				sources[fd] = shellInputSource{kind: shellInputScript, script: redirect.target.text}
			}
		case "<<":
			if script, ok := hereDocBodies[redirect.target.text]; ok {
				sources[fd] = shellInputSource{kind: shellInputScript, script: script}
			} else {
				sources[fd] = shellInputSource{kind: shellInputUnknown}
			}
		case "<", "<>":
			if redirect.target.dynamic || strings.Contains(redirect.target.text, dynamicWordPlaceholder) {
				sources[fd] = shellInputSource{kind: shellInputUnknown}
				continue
			}
			if sourceFD, ok := shellInputFileDescriptor(redirect.target.text); ok {
				sources[fd] = sources[sourceFD]
			} else {
				delete(sources, fd)
			}
		case "<&":
			if redirect.target.dynamic || redirect.target.maySplit || strings.Contains(redirect.target.text, dynamicWordPlaceholder) {
				sources[fd] = shellInputSource{kind: shellInputUnknown}
				continue
			}
			sourceFD, move, ok := duplicatedFileDescriptor(redirect.target.text)
			if !ok {
				delete(sources, fd)
				continue
			}
			sources[fd] = sources[sourceFD]
			if move {
				delete(sources, sourceFD)
			}
		}
	}
	return sources
}

func shellInputContainsGitPush(source shellInputSource) bool {
	if source.kind == shellInputUnknown {
		return true
	}
	if source.kind != shellInputScript {
		return false
	}
	push, err := containsGitPush(source.script)
	return err != nil || push
}

func shellInputRedirect(operator string) (fd int, redirectOperator string, input bool) {
	i := 0
	for i < len(operator) && operator[i] >= '0' && operator[i] <= '9' {
		i++
	}
	fd = 0
	if i > 0 {
		parsedFD, err := strconv.Atoi(operator[:i])
		if err != nil {
			return 0, "", false
		}
		fd = parsedFD
	}
	operator = operator[i:]
	switch operator {
	case "<", "<<", "<<<", "<>", "<&":
		return fd, operator, true
	default:
		return 0, "", false
	}
}

func shellInputFileDescriptor(path string) (int, bool) {
	if path == "/dev/stdin" {
		return 0, true
	}
	for _, prefix := range []string{"/dev/fd/", "/proc/self/fd/"} {
		value, ok := strings.CutPrefix(path, prefix)
		if !ok || value == "" {
			continue
		}
		fd, err := strconv.Atoi(value)
		return fd, err == nil && fd >= 0
	}
	return 0, false
}

func duplicatedFileDescriptor(value string) (fd int, move, ok bool) {
	if value == "-" {
		return 0, false, false
	}
	if strings.HasSuffix(value, "-") {
		move = true
		value = strings.TrimSuffix(value, "-")
	}
	if value == "" {
		return 0, false, false
	}
	fd, err := strconv.Atoi(value)
	return fd, move, err == nil && fd >= 0
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

func evaluatedCommandContainsGitPush(tokens []shellToken, inputSources map[int]shellInputSource, gitAliases map[string]string) bool {
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
	if words[0] == "--" {
		words = words[1:]
	}
	if len(words) == 0 {
		return false
	}
	push, err := containsGitPushWithContext(strings.Join(words, " "), inputSources, gitAliases)
	return err != nil || push
}

func skipCommandPrefixes(tokens []shellToken, start int, allowAssignments bool) int {
	i := start
	for i < len(tokens) {
		if tokens[i].kind == redirectToken {
			i++
			if i < len(tokens) && tokens[i].kind == wordToken {
				i++
			}
			continue
		}
		if allowAssignments && tokens[i].kind == wordToken && isAssignment(tokens[i].text) {
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
	return ok && isShellIdentifier(name)
}

func isShellIdentifier(name string) bool {
	if name == "" {
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

func recordGitEnvironmentAssignments(environment map[string]shellToken, tokens []shellToken) {
	for _, token := range tokens {
		recordGitEnvironmentAssignment(environment, token)
	}
}

func recordGitEnvironmentAssignment(environment map[string]shellToken, token shellToken) {
	name, value, ok := strings.Cut(token.text, "=")
	if !ok || name != "GIT_CONFIG_COUNT" && !strings.HasPrefix(name, "GIT_CONFIG_KEY_") && !strings.HasPrefix(name, "GIT_CONFIG_VALUE_") {
		return
	}
	token.text = value
	environment[name] = token
}

func recordShellEnvironmentAssignments(environment map[string]shellToken, tokens []shellToken) {
	for _, token := range tokens {
		recordShellEnvironmentAssignment(environment, token)
	}
}

func recordShellEnvironmentAssignment(environment map[string]shellToken, token shellToken) {
	name, value, ok := strings.Cut(token.text, "=")
	if !ok || name != "BASH_ENV" && name != "POSIXLY_CORRECT" {
		return
	}
	token.text = value
	environment[name] = token
}

func shellEnvironmentContainsGitPush(environment map[string]shellToken, inputSources map[int]shellInputSource, gitAliases map[string]string) bool {
	bashEnvironment, ok := environment["BASH_ENV"]
	if !ok || bashEnvironment.text == "" {
		return false
	}
	if bashEnvironment.dynamic || strings.Contains(bashEnvironment.text, dynamicWordPlaceholder) {
		return true
	}
	if bashEnvironment.text == "/dev/null" {
		return false
	}
	fd, ok := shellInputFileDescriptor(bashEnvironment.text)
	if !ok {
		return true
	}
	return shellInputSourceContainsGitPush(fd, inputSources, gitAliases)
}

func gitAliasesFromEnvironment(environment map[string]shellToken) map[string]string {
	aliases := make(map[string]string)
	countToken, ok := environment["GIT_CONFIG_COUNT"]
	if !ok {
		return aliases
	}
	if countToken.dynamic {
		aliases[dynamicGitAliasName] = dynamicGitAlias
		return aliases
	}
	count, err := strconv.Atoi(countToken.text)
	if err != nil || count < 0 || count > 10_000 {
		return aliases
	}
	for i := range count {
		key, keyOK := environment[fmt.Sprintf("GIT_CONFIG_KEY_%d", i)]
		value, valueOK := environment[fmt.Sprintf("GIT_CONFIG_VALUE_%d", i)]
		if !keyOK || !valueOK {
			continue
		}
		if key.dynamic {
			clear(aliases)
			aliases[dynamicGitAliasName] = dynamicGitAlias
			continue
		}
		name, _, alias := gitAliasDefinition(key.text + "=")
		if !alias {
			continue
		}
		if value.dynamic {
			aliases[name] = dynamicGitAlias
		} else {
			aliases[name] = value.text
		}
	}
	return aliases
}

func gitArgsContainPush(tokens []shellToken, aliases map[string]string, inputSources map[int]shellInputSource) bool {
	return gitArgsContainPushWithState(tokens, aliases, make(map[string]bool), inputSources)
}

func gitArgsContainPushWithState(tokens []shellToken, aliases map[string]string, seen map[string]bool, inputSources map[int]shellInputSource) bool {
	optionsWithSeparateValue := map[string]bool{
		"-C": true, "--git-dir": true, "--work-tree": true,
		"--namespace": true, "--exec-path": true,
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
		if arg == "-c" {
			if i+1 >= len(tokens) || tokens[i+1].kind != wordToken {
				return false
			}
			config := tokens[i+1]
			if config.maySplit {
				return true
			}
			if name, value, ok := gitAliasDefinition(config.text); ok {
				if config.dynamic {
					aliases[name] = dynamicGitAlias
				} else {
					aliases[name] = value
				}
			}
			i++
			continue
		}
		if arg == "--config-env" {
			if i+1 >= len(tokens) || tokens[i+1].kind != wordToken {
				return false
			}
			config := tokens[i+1]
			if config.maySplit {
				return true
			}
			if config.dynamic {
				aliases[dynamicGitAliasName] = dynamicGitAlias
			} else if name, alias := gitConfigEnvAlias(config.text); alias {
				aliases[name] = dynamicGitAlias
			}
			i++
			continue
		}
		if config, ok := strings.CutPrefix(arg, "--config-env="); ok {
			if tokens[i].maySplit {
				return true
			}
			if tokens[i].dynamic {
				aliases[dynamicGitAliasName] = dynamicGitAlias
			} else if name, alias := gitConfigEnvAlias(config); alias {
				aliases[name] = dynamicGitAlias
			}
			continue
		}
		if strings.HasPrefix(arg, "-c") && len(arg) > 2 {
			// Unlike -C, Git does not accept an attached value for -c.
			return false
		}
		if tokens[i].dynamic && (!gitOptionHasAttachedValue(arg) || tokens[i].maySplit) {
			return true
		}
		if arg == "--" {
			// Git has no global option separator before the subcommand.
			return false
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
		if arg == "push" {
			return true
		}
		return gitAliasContainsPush(arg, tokens[i+1:], aliases, seen, inputSources)
	}
	return false
}

func gitAliasDefinition(config string) (name, value string, ok bool) {
	key, value, ok := strings.Cut(config, "=")
	if !ok || len(key) <= len("alias.") || !strings.EqualFold(key[:len("alias.")], "alias.") {
		return "", "", false
	}
	return strings.ToLower(key[len("alias."):]), value, true
}

func gitConfigEnvAlias(config string) (name string, ok bool) {
	key, _, ok := strings.Cut(config, "=")
	if !ok || len(key) <= len("alias.") || !strings.EqualFold(key[:len("alias.")], "alias.") {
		return "", false
	}
	return strings.ToLower(key[len("alias."):]), true
}

func gitAliasContainsPush(name string, invocationArgs []shellToken, aliases map[string]string, seen map[string]bool, inputSources map[int]shellInputSource) bool {
	name = strings.ToLower(name)
	if seen[name] {
		return false
	}
	value, ok := aliases[name]
	if !ok {
		if isKnownGitCommand(name) {
			return false
		}
		return aliases[dynamicGitAliasName] == dynamicGitAlias
	}
	seen[name] = true
	if value == dynamicGitAlias {
		return true
	}
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "!") {
		command := gitShellAliasCommand(strings.TrimSpace(value[1:]), invocationArgs)
		childAliases := maps.Clone(aliases)
		delete(childAliases, name)
		push, err := containsGitPushWithContext(command, inputSources, childAliases)
		return err != nil || push
	}
	words, err := splitAliasWords(value)
	if err != nil || len(words) == 0 {
		return err != nil
	}
	expanded := make([]shellToken, 0, len(words)+len(invocationArgs))
	for _, word := range words {
		expanded = append(expanded, shellToken{text: word, kind: wordToken})
	}
	expanded = append(expanded, invocationArgs...)
	return gitArgsContainPushWithState(expanded, aliases, seen, inputSources)
}

func isKnownGitCommand(name string) bool {
	switch name {
	case "add", "am", "archive", "bisect", "branch", "bundle", "checkout", "cherry", "cherry-pick", "clean", "clone", "commit", "config", "describe", "diff", "difftool", "fetch", "format-patch", "fsck", "gc", "grep", "init", "log", "maintenance", "merge", "mergetool", "mv", "notes", "pull", "range-diff", "rebase", "reflog", "remote", "repack", "replace", "reset", "restore", "revert", "rm", "shortlog", "show", "show-branch", "sparse-checkout", "stash", "status", "submodule", "switch", "tag", "worktree":
		return true
	default:
		return false
	}
}

func gitShellAliasCommand(body string, invocationArgs []shellToken) string {
	var command strings.Builder
	command.WriteString(body)
	for _, arg := range invocationArgs {
		if arg.kind != wordToken {
			continue
		}
		command.WriteByte(' ')
		if arg.dynamic {
			if arg.maySplit {
				command.WriteString("$AGENT_HOOK_DYNAMIC")
			} else {
				command.WriteString("\"$AGENT_HOOK_DYNAMIC\"")
			}
			continue
		}
		command.WriteByte('\'')
		command.WriteString(strings.ReplaceAll(arg.text, "'", "'\\''"))
		command.WriteByte('\'')
	}
	return command.String()
}

func splitAliasWords(value string) ([]string, error) {
	var words []string
	var current strings.Builder
	inWord := false
	quote := byte(0)
	flush := func() {
		if !inWord {
			return
		}
		words = append(words, current.String())
		current.Reset()
		inWord = false
	}

	for i := 0; i < len(value); i++ {
		ch := value[i]
		if quote != 0 {
			if ch == quote {
				quote = 0
				continue
			}
			if ch == '\\' && quote == '"' {
				if i+1 >= len(value) {
					return nil, errors.New("trailing escape in git alias")
				}
				i++
				current.WriteByte(value[i])
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
			if i+1 >= len(value) {
				return nil, errors.New("trailing escape in git alias")
			}
			i++
			current.WriteByte(value[i])
			inWord = true
		case ' ', '\t', '\r', '\n':
			flush()
		default:
			current.WriteByte(ch)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, errors.New("unterminated quote in git alias")
	}
	flush()
	return words, nil
}

func gitOptionHasAttachedValue(arg string) bool {
	for _, prefix := range []string{"--git-dir=", "--work-tree=", "--namespace=", "--config-env=", "--exec-path="} {
		if strings.HasPrefix(arg, prefix) {
			return true
		}
	}
	return len(arg) > 2 && strings.HasPrefix(arg, "-C")
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
			if consumed == 0 {
				writeByte('\\')
				writeByte(escaped)
				continue
			}
			writeByte(byte(decoded))
			i += consumed
		case 'u':
			decoded, consumed, err := parseEscapedNumber(command[i+1:], 16, 4)
			if err != nil {
				return "", 0, err
			}
			if consumed == 0 {
				writeByte('\\')
				writeByte(escaped)
				continue
			}
			writeRune(rune(decoded))
			i += consumed
		case 'U':
			decoded, consumed, err := parseEscapedNumber(command[i+1:], 16, 8)
			if err != nil {
				return "", 0, err
			}
			if consumed == 0 {
				writeByte('\\')
				writeByte(escaped)
				continue
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
		return 0, 0, nil
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
