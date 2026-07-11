package main

import (
	"fmt"
	"strings"
)

const hereDocPlaceholder = "__AGENT_HOOK_HEREDOC_"

type hereDocSpec struct {
	placeholder string
	delimiter   string
	quoted      bool
	stripTabs   bool
}

// stripHereDocBodies removes here-document bodies and shell comments from
// shell source so they are not mistaken for commands. Command substitutions
// from expandable bodies are returned separately because the invoking shell
// executes those before it starts the redirected command.
func stripHereDocBodies(command string) (code string, executableScripts []string, err error) {
	return stripHereDocBodiesAndCapture(command, nil)
}

func stripHereDocBodiesAndCapture(command string, capturedBodies map[string]string) (code string, executableScripts []string, err error) {
	var normalized strings.Builder
	var pending []hereDocSpec
	quote := byte(0)
	inWord := false
	nextHereDocID := 0

	for i := 0; i < len(command); {
		ch := command[i]

		if quote != '\'' && strings.HasPrefix(command[i:], "$((") {
			end, closeErr := closingCommandParenthesis(command, i+2)
			if closeErr != nil {
				return "", nil, fmt.Errorf("arithmetic expansion: %w", closeErr)
			}
			normalized.WriteString(command[i : end+1])
			i = end + 1
			inWord = true
			continue
		}
		if quote == 0 && !inWord && strings.HasPrefix(command[i:], "((") {
			end, closeErr := closingCommandParenthesis(command, i+1)
			if closeErr != nil {
				return "", nil, fmt.Errorf("arithmetic command: %w", closeErr)
			}
			normalized.WriteString(command[i : end+1])
			i = end + 1
			inWord = false
			continue
		}
		if quote != '\'' && strings.HasPrefix(command[i:], "$(") {
			end, closeErr := closingCommandParenthesis(command, i+2)
			if closeErr != nil {
				return "", nil, closeErr
			}
			normalized.WriteString(command[i : end+1])
			i = end + 1
			inWord = true
			continue
		}
		if quote == 0 && (ch == '<' || ch == '>') && i+1 < len(command) && command[i+1] == '(' {
			end, closeErr := closingCommandParenthesis(command, i+2)
			if closeErr != nil {
				return "", nil, closeErr
			}
			normalized.WriteString(command[i : end+1])
			i = end + 1
			inWord = true
			continue
		}
		if quote != '\'' && ch == '`' {
			end, closeErr := closingBacktick(command, i+1)
			if closeErr != nil {
				return "", nil, closeErr
			}
			normalized.WriteString(command[i : end+1])
			i = end + 1
			inWord = true
			continue
		}
		if quote != '\'' && strings.HasPrefix(command[i:], "${") {
			end, closeErr := closingParameterExpansion(command, i+2)
			if closeErr != nil {
				return "", nil, closeErr
			}
			normalized.WriteString(command[i : end+1])
			i = end + 1
			inWord = true
			continue
		}
		if quote == 0 && ch == '$' && i+1 < len(command) && command[i+1] == '\'' {
			_, end, ansiErr := parseANSIQuoted(command, i+2)
			if ansiErr != nil {
				return "", nil, ansiErr
			}
			normalized.WriteString(command[i : end+1])
			i = end + 1
			inWord = true
			continue
		}

		if quote != 0 {
			normalized.WriteByte(ch)
			if ch == quote {
				quote = 0
				i++
				continue
			}
			if quote == '"' && ch == '\\' && i+1 < len(command) {
				normalized.WriteByte(command[i+1])
				i += 2
				continue
			}
			i++
			continue
		}

		if ch == '\\' {
			normalized.WriteByte(ch)
			if i+1 >= len(command) {
				return "", nil, fmt.Errorf("trailing escape")
			}
			normalized.WriteByte(command[i+1])
			if command[i+1] != '\n' {
				inWord = true
			}
			i += 2
			continue
		}

		switch ch {
		case '\'', '"':
			quote = ch
			inWord = true
			normalized.WriteByte(ch)
			i++
			continue
		case '#':
			if !inWord {
				end := strings.IndexByte(command[i:], '\n')
				if end < 0 {
					i = len(command)
					continue
				}
				end += i
				i = end
				continue
			}
		}

		if ch == '<' {
			if strings.HasPrefix(command[i:], "<<<") {
				normalized.WriteString("<<<")
				i += len("<<<")
				inWord = false
				continue
			}
			opLength, stripTabs := hereDocOperator(command[i:])
			if opLength != 0 {
				normalized.WriteString("<<")
				i += opLength
				for i < len(command) && (command[i] == ' ' || command[i] == '\t') {
					normalized.WriteByte(command[i])
					i++
				}
				if i >= len(command) || command[i] == '\n' || command[i] == '\r' {
					return "", nil, fmt.Errorf("here-document operator has no delimiter")
				}

				delimiter, quotedDelimiter, end, delimiterErr := readHereDocDelimiter(command, i)
				if delimiterErr != nil {
					return "", nil, delimiterErr
				}
				placeholder := fmt.Sprintf("%s%d__", hereDocPlaceholder, nextHereDocID)
				nextHereDocID++
				normalized.WriteString(placeholder)
				pending = append(pending, hereDocSpec{
					placeholder: placeholder,
					delimiter:   delimiter,
					quoted:      quotedDelimiter,
					stripTabs:   stripTabs,
				})
				i = end
				inWord = true
				continue
			}
		}

		if ch == '\n' {
			normalized.WriteByte(ch)
			i++
			inWord = false
			if len(pending) != 0 {
				var scripts []string
				var consumeErr error
				i, scripts, consumeErr = consumeHereDocs(command, i, pending, capturedBodies)
				if consumeErr != nil {
					return "", nil, consumeErr
				}
				executableScripts = append(executableScripts, scripts...)
				pending = pending[:0]
			}
			continue
		}

		normalized.WriteByte(ch)
		if ch == ' ' || ch == '\t' || ch == '\r' || strings.ContainsRune(";&|()<>", rune(ch)) {
			inWord = false
		} else {
			inWord = true
		}
		i++
	}

	if len(pending) != 0 {
		return "", nil, fmt.Errorf("here-document body does not start before end of command")
	}
	return normalized.String(), executableScripts, nil
}

func hereDocOperator(source string) (length int, stripTabs bool) {
	if strings.HasPrefix(source, "<<<") {
		return 0, false
	}
	if strings.HasPrefix(source, "<<-") {
		return len("<<-"), true
	}
	if strings.HasPrefix(source, "<<") {
		return len("<<"), false
	}
	return 0, false
}

func readHereDocDelimiter(command string, start int) (delimiter string, quoted bool, end int, err error) {
	var value strings.Builder
	quote := byte(0)
	sawWord := false

	for i := start; i < len(command); {
		ch := command[i]
		if quote != 0 {
			sawWord = true
			if ch == quote {
				quote = 0
				i++
				continue
			}
			if quote == '"' && ch == '\\' && i+1 < len(command) {
				next := command[i+1]
				if strings.ContainsRune("$`\"\\", rune(next)) {
					value.WriteByte(next)
				} else if next != '\n' {
					value.WriteByte(ch)
					value.WriteByte(next)
				}
				i += 2
				continue
			}
			if ch == '\n' || ch == '\r' {
				return "", false, 0, fmt.Errorf("newline in quoted here-document delimiter")
			}
			value.WriteByte(ch)
			i++
			continue
		}
		if ch == '$' && i+1 < len(command) && command[i+1] == '\'' {
			decoded, ansiEnd, ansiErr := parseANSIQuoted(command, i+2)
			if ansiErr != nil {
				return "", false, 0, ansiErr
			}
			if strings.ContainsAny(decoded, "\r\n") {
				return "", false, 0, fmt.Errorf("newline in ANSI-C quoted here-document delimiter")
			}
			value.WriteString(decoded)
			quoted = true
			sawWord = true
			i = ansiEnd + 1
			continue
		}

		switch ch {
		case '\'', '"':
			quote = ch
			quoted = true
			sawWord = true
			i++
			continue
		case '\\':
			if i+1 >= len(command) {
				return "", false, 0, fmt.Errorf("trailing escape in here-document delimiter")
			}
			if command[i+1] == '\n' {
				i += 2
				continue
			}
			quoted = true
			sawWord = true
			value.WriteByte(command[i+1])
			i += 2
			continue
		case '$':
			if i+1 < len(command) && (command[i+1] == '(' || command[i+1] == '"') {
				return "", false, 0, fmt.Errorf("unsupported expansion syntax in here-document delimiter")
			}
		case '`':
			return "", false, 0, fmt.Errorf("unsupported command substitution syntax in here-document delimiter")
		}

		if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' || strings.ContainsRune(";&|()<>", rune(ch)) {
			if !sawWord {
				return "", false, 0, fmt.Errorf("here-document operator has no delimiter")
			}
			return value.String(), quoted, i, nil
		}
		value.WriteByte(ch)
		sawWord = true
		i++
	}

	if quote != 0 {
		return "", false, 0, fmt.Errorf("unterminated quote in here-document delimiter")
	}
	if !sawWord {
		return "", false, 0, fmt.Errorf("here-document operator has no delimiter")
	}
	return value.String(), quoted, len(command), nil
}

func consumeHereDocs(command string, start int, specs []hereDocSpec, capturedBodies map[string]string) (end int, scripts []string, err error) {
	position := start
	for _, spec := range specs {
		var body strings.Builder
		found := false
		for position <= len(command) {
			line, nextPosition, hasNewline := readHereDocLogicalLine(command, position, !spec.quoted, spec.stripTabs)
			comparison := strings.TrimSuffix(line, "\r")
			if comparison == spec.delimiter {
				position = nextPosition
				found = true
				break
			}

			body.WriteString(line)
			if hasNewline {
				body.WriteByte('\n')
			}
			position = nextPosition
			if !hasNewline {
				break
			}
		}

		if !found {
			return 0, nil, fmt.Errorf("unterminated here-document %q", spec.delimiter)
		}
		if capturedBodies != nil {
			capturedBody := body.String()
			if !spec.quoted {
				capturedBody = unescapeHereDocBody(capturedBody)
			}
			capturedBodies[spec.placeholder] = capturedBody
		}
		if !spec.quoted {
			bodyScripts, bodyErr := hereDocExecutableScripts(body.String())
			if bodyErr != nil {
				return 0, nil, bodyErr
			}
			scripts = append(scripts, bodyScripts...)
		}
	}
	return position, scripts, nil
}

func unescapeHereDocBody(body string) string {
	var unescaped strings.Builder
	for i := 0; i < len(body); i++ {
		if body[i] == '\\' && i+1 < len(body) && strings.ContainsRune("\\$`", rune(body[i+1])) {
			i++
		}
		unescaped.WriteByte(body[i])
	}
	return unescaped.String()
}

func readHereDocLogicalLine(command string, start int, expandable, stripTabs bool) (line string, next int, hasNewline bool) {
	var logical strings.Builder
	position := start
	for {
		lineEnd := strings.IndexByte(command[position:], '\n')
		hasNewline = lineEnd >= 0
		if hasNewline {
			lineEnd += position
		} else {
			lineEnd = len(command)
		}

		physical := command[position:lineEnd]
		if stripTabs {
			physical = strings.TrimLeft(physical, "\t")
		}
		if expandable && hasNewline {
			content := strings.TrimSuffix(physical, "\r")
			if hasOddTrailingBackslashes(content) {
				logical.WriteString(content[:len(content)-1])
				position = lineEnd + 1
				continue
			}
		}

		logical.WriteString(physical)
		next = lineEnd
		if hasNewline {
			next++
		}
		return logical.String(), next, hasNewline
	}
}

func hasOddTrailingBackslashes(value string) bool {
	count := 0
	for i := len(value) - 1; i >= 0 && value[i] == '\\'; i-- {
		count++
	}
	return count%2 != 0
}

func hereDocExecutableScripts(body string) ([]string, error) {
	var scripts []string
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '\\':
			if i+1 >= len(body) {
				continue
			}
			next := body[i+1]
			if next == '\\' || next == '$' || next == '`' || next == '\n' {
				i++
			}
		case '`':
			end, err := closingBacktick(body, i+1)
			if err != nil {
				return nil, err
			}
			scripts = append(scripts, body[i+1:end])
			i = end
		case '$':
			if i+2 < len(body) && body[i+1] == '(' && body[i+2] == '(' {
				i += 2
				continue
			}
			if i+1 >= len(body) || body[i+1] != '(' {
				continue
			}
			end, err := closingCommandParenthesis(body, i+2)
			if err != nil {
				return nil, err
			}
			scripts = append(scripts, body[i+2:end])
			i = end
		}
	}
	return scripts, nil
}
