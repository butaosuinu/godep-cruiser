package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

const anonymousSource = "<config>"

// Error describes an invalid configuration value and its source position.
// Offset, Line, and Column are one-based.
type Error struct {
	Source string
	Path   string
	Offset int
	Line   int
	Column int
	Err    error
}

// Error formats the source position, JSON path, and validation failure.
func (err *Error) Error() string {
	if err.Path == "" {
		return fmt.Sprintf("%s:%d:%d: %v", err.Source, err.Line, err.Column, err.Err)
	}

	return fmt.Sprintf("%s:%d:%d: %s: %v", err.Source, err.Line, err.Column, err.Path, err.Err)
}

// Unwrap returns the underlying syntax or validation error.
func (err *Error) Unwrap() error {
	return err.Err
}

// Load reads and validates one JSON configuration document from reader.
func Load(reader io.Reader) (*Config, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read configuration: %w", err)
	}

	return parse(anonymousSource, data)
}

// LoadFile reads and validates the JSON configuration at filename.
func LoadFile(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("read configuration %q: %w", filename, err)
	}

	return parse(filename, data)
}

// Parse validates one JSON configuration document in data.
func Parse(data []byte) (*Config, error) {
	return parse(anonymousSource, data)
}

func parse(source string, data []byte) (*Config, error) {
	root, err := parseJSON(data)
	if err != nil {
		return nil, syntaxError(source, data, err)
	}

	validator := configValidator{source: source, data: data}
	if err := validator.validate(root); err != nil {
		return nil, err
	}

	var result Config
	if err := json.Unmarshal(data, &result); err != nil {
		// The positional tree validation above checks all supported types. Keep
		// this defensive branch positioned if the data model changes later.
		return nil, syntaxError(source, data, err)
	}

	for index := range result.Forbidden {
		if result.Forbidden[index].Severity == "" {
			result.Forbidden[index].Severity = SeverityWarn
		}
		if result.Forbidden[index].Scope == "" {
			result.Forbidden[index].Scope = ScopeModule
		}
	}
	for index := range result.Required {
		if result.Required[index].Severity == "" {
			result.Required[index].Severity = SeverityWarn
		}
	}
	if result.AllowedSeverity == "" {
		result.AllowedSeverity = SeverityWarn
	}

	return &result, nil
}

func syntaxError(source string, data []byte, err error) *Error {
	offset := 1

	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) && syntaxErr.Offset > 0 {
		offset = int(syntaxErr.Offset)
	} else if len(data) > 0 {
		offset = len(data)
	}

	return positionedError(source, data, "", offset-1, err)
}

func positionedError(source string, data []byte, path string, offset int, err error) *Error {
	offset = max(0, min(offset, len(data)))
	line := 1
	lineStart := 0
	for index, current := range data[:offset] {
		if current == '\n' {
			line++
			lineStart = index + 1
		}
	}

	return &Error{
		Source: source,
		Path:   path,
		Offset: offset + 1,
		Line:   line,
		Column: utf8.RuneCount(data[lineStart:offset]) + 1,
		Err:    err,
	}
}

type jsonKind uint8

const (
	jsonObject jsonKind = iota
	jsonArray
	jsonString
	jsonNumber
	jsonBoolean
	jsonNull
)

type jsonNode struct {
	kind    jsonKind
	offset  int
	text    string
	members []jsonMember
	items   []*jsonNode
}

type jsonMember struct {
	name       string
	nameOffset int
	value      *jsonNode
}

type jsonParser struct {
	data  []byte
	index int
}

func parseJSON(data []byte) (*jsonNode, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("multiple JSON values are not allowed")
		}

		return nil, err
	}

	parser := jsonParser{data: data}
	root, err := parser.parseValue()
	if err != nil {
		return nil, err
	}
	parser.skipSpace()
	if parser.index != len(data) {
		return nil, fmt.Errorf("unexpected content after JSON document")
	}

	return root, nil
}

func (parser *jsonParser) parseValue() (*jsonNode, error) {
	parser.skipSpace()
	if parser.index >= len(parser.data) {
		return nil, io.ErrUnexpectedEOF
	}

	switch parser.data[parser.index] {
	case '{':
		return parser.parseObject()
	case '[':
		return parser.parseArray()
	case '"':
		return parser.parseString()
	case 't', 'f':
		return parser.parsePrimitive(jsonBoolean), nil
	case 'n':
		return parser.parsePrimitive(jsonNull), nil
	default:
		return parser.parsePrimitive(jsonNumber), nil
	}
}

func (parser *jsonParser) parseObject() (*jsonNode, error) {
	node := &jsonNode{kind: jsonObject, offset: parser.index}
	parser.index++
	parser.skipSpace()
	if parser.consume('}') {
		return node, nil
	}

	for {
		parser.skipSpace()
		nameOffset := parser.index
		name, err := parser.parseString()
		if err != nil {
			return nil, err
		}
		parser.skipSpace()
		if !parser.consume(':') {
			return nil, fmt.Errorf("expected colon after object key")
		}
		value, err := parser.parseValue()
		if err != nil {
			return nil, err
		}
		node.members = append(node.members, jsonMember{
			name:       name.text,
			nameOffset: nameOffset,
			value:      value,
		})
		parser.skipSpace()
		if parser.consume('}') {
			return node, nil
		}
		if !parser.consume(',') {
			return nil, fmt.Errorf("expected comma between object members")
		}
	}
}

func (parser *jsonParser) parseArray() (*jsonNode, error) {
	node := &jsonNode{kind: jsonArray, offset: parser.index}
	parser.index++
	parser.skipSpace()
	if parser.consume(']') {
		return node, nil
	}

	for {
		item, err := parser.parseValue()
		if err != nil {
			return nil, err
		}
		node.items = append(node.items, item)
		parser.skipSpace()
		if parser.consume(']') {
			return node, nil
		}
		if !parser.consume(',') {
			return nil, fmt.Errorf("expected comma between array items")
		}
	}
}

func (parser *jsonParser) parseString() (*jsonNode, error) {
	start := parser.index
	parser.index++
	for parser.index < len(parser.data) {
		switch parser.data[parser.index] {
		case '\\':
			parser.index += 2
		case '"':
			parser.index++
			var text string
			if err := json.Unmarshal(parser.data[start:parser.index], &text); err != nil {
				return nil, err
			}

			return &jsonNode{kind: jsonString, offset: start, text: text}, nil
		default:
			parser.index++
		}
	}

	return nil, io.ErrUnexpectedEOF
}

func (parser *jsonParser) parsePrimitive(kind jsonKind) *jsonNode {
	start := parser.index
	for parser.index < len(parser.data) && !strings.ContainsRune(" \t\r\n,]}", rune(parser.data[parser.index])) {
		parser.index++
	}

	return &jsonNode{
		kind:   kind,
		offset: start,
		text:   string(parser.data[start:parser.index]),
	}
}

func (parser *jsonParser) skipSpace() {
	for parser.index < len(parser.data) && strings.ContainsRune(" \t\r\n", rune(parser.data[parser.index])) {
		parser.index++
	}
}

func (parser *jsonParser) consume(want byte) bool {
	if parser.index >= len(parser.data) || parser.data[parser.index] != want {
		return false
	}
	parser.index++

	return true
}
