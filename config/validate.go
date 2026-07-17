package config

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type configValidator struct {
	source string
	data   []byte
}

const reservedAllowedViolationRuleName = "not-in-allowed"

type ruleKind uint8

const (
	forbiddenRuleKind ruleKind = iota
	requiredRuleKind
	allowedRuleKind
)

func (validator configValidator) validate(root *jsonNode) error {
	fields, err := validator.object(root, "$", "forbidden", "required", "allowed", "allowedSeverity")
	if err != nil {
		return err
	}

	constraintNames := make(map[string]struct{})
	if forbidden, ok := fields["forbidden"]; ok {
		if err := validator.rules(forbidden.value, "$.forbidden", forbiddenRuleKind, constraintNames); err != nil {
			return err
		}
	}
	if required, ok := fields["required"]; ok {
		if err := validator.rules(required.value, "$.required", requiredRuleKind, constraintNames); err != nil {
			return err
		}
	}
	if allowed, ok := fields["allowed"]; ok {
		if err := validator.rules(
			allowed.value,
			"$.allowed",
			allowedRuleKind,
			make(map[string]struct{}),
		); err != nil {
			return err
		}
	}
	if severity, ok := fields["allowedSeverity"]; ok {
		if err := validator.severity(severity.value, "$.allowedSeverity"); err != nil {
			return err
		}
	}

	return nil
}

func (validator configValidator) rules(
	node *jsonNode,
	path string,
	kind ruleKind,
	names map[string]struct{},
) error {
	if err := validator.kind(node, path, jsonArray); err != nil {
		return err
	}

	for index, item := range node.items {
		rulePath := indexedPath(path, index)
		if err := validator.rule(item, rulePath, kind); err != nil {
			return err
		}
		var nameNode *jsonNode
		for _, member := range item.members {
			if member.name == "name" {
				nameNode = member.value

				break
			}
		}
		if _, duplicate := names[nameNode.text]; duplicate {
			return validator.at(nameNode, fieldPath(rulePath, "name"), fmt.Errorf("duplicate rule name %q", nameNode.text))
		}
		names[nameNode.text] = struct{}{}
	}

	return nil
}

func (validator configValidator) rule(node *jsonNode, path string, kind ruleKind) error {
	allowedFields := []string{"name", "comment", "from", "to"}
	if kind != allowedRuleKind {
		allowedFields = append(allowedFields, "severity")
	}
	if kind == forbiddenRuleKind {
		allowedFields = append(allowedFields, "scope")
	}
	fields, err := validator.object(node, path, allowedFields...)
	if err != nil {
		return err
	}

	name, err := validator.required(fields, node, path, "name")
	if err != nil {
		return err
	}
	if validationErr := validator.nonEmptyString(name.value, fieldPath(path, "name")); validationErr != nil {
		return validationErr
	}
	if name.value.text == reservedAllowedViolationRuleName {
		return validator.at(
			name.value,
			fieldPath(path, "name"),
			fmt.Errorf("rule name %q is reserved", name.value.text),
		)
	}
	from, err := validator.required(fields, node, path, "from")
	if err != nil {
		return err
	}
	to, err := validator.required(fields, node, path, "to")
	if err != nil {
		return err
	}

	if comment, ok := fields["comment"]; ok {
		if validationErr := validator.kind(comment.value, fieldPath(path, "comment"), jsonString); validationErr != nil {
			return validationErr
		}
	}
	if severity, ok := fields["severity"]; ok {
		if validationErr := validator.severity(severity.value, fieldPath(path, "severity")); validationErr != nil {
			return validationErr
		}
	}
	ruleScope := ScopeModule
	if scope, ok := fields["scope"]; ok {
		ruleScope, err = validator.scope(scope.value, fieldPath(path, "scope"))
		if err != nil {
			return err
		}
	}

	fromPatterns, err := validator.from(
		from.value,
		fieldPath(path, "from"),
		kind != requiredRuleKind,
		ruleScope,
	)
	if err != nil {
		return err
	}
	if err := validator.to(
		to.value,
		fieldPath(path, "to"),
		fromPatterns,
		kind,
		ruleScope,
	); err != nil {
		return err
	}

	return nil
}

func (validator configValidator) from(
	node *jsonNode,
	path string,
	allowGraphFields bool,
	ruleScope Scope,
) ([]*regexp.Regexp, error) {
	allowedFields := []string{"path", "pathNot", "packageName"}
	if allowGraphFields {
		allowedFields = append(
			allowedFields,
			"orphan",
			"numberOfDependentsLessThan",
			"numberOfDependentsMoreThan",
		)
	}
	fields, validationErr := validator.object(node, path, allowedFields...)
	if validationErr != nil {
		return nil, validationErr
	}
	if ruleScope == ScopeFolder {
		for _, conflict := range []string{"orphan", "packageName"} {
			if member, ok := fields[conflict]; ok {
				return nil, validator.at(
					member.value,
					fieldPath(path, conflict),
					fmt.Errorf(`field %q cannot be combined with scope "folder"`, conflict),
				)
			}
		}
	}

	var fromPatterns []*regexp.Regexp
	if member, ok := fields["path"]; ok {
		fromPatterns, validationErr = validator.regularExpressions(member.value, fieldPath(path, "path"))
		if validationErr != nil {
			return nil, validationErr
		}
	}
	if member, ok := fields["pathNot"]; ok {
		if _, err := validator.regularExpressions(member.value, fieldPath(path, "pathNot")); err != nil {
			return nil, err
		}
	}
	if member, ok := fields["packageName"]; ok {
		if _, err := validator.regularExpressions(member.value, fieldPath(path, "packageName")); err != nil {
			return nil, err
		}
	}
	if member, ok := fields["orphan"]; ok {
		if err := validator.kind(member.value, fieldPath(path, "orphan"), jsonBoolean); err != nil {
			return nil, err
		}
	}

	lessThan, hasLessThan, err := validator.dependentCount(
		fields,
		path,
		"numberOfDependentsLessThan",
	)
	if err != nil {
		return nil, err
	}
	if hasLessThan && lessThan < 1 {
		member := fields["numberOfDependentsLessThan"]
		return nil, validator.at(
			member.value,
			fieldPath(path, "numberOfDependentsLessThan"),
			errors.New("must be at least 1"),
		)
	}
	moreThan, hasMoreThan, err := validator.dependentCount(
		fields,
		path,
		"numberOfDependentsMoreThan",
	)
	if err != nil {
		return nil, err
	}
	if hasLessThan && hasMoreThan && moreThan >= lessThan-1 {
		member := fields["numberOfDependentsMoreThan"]
		return nil, validator.at(
			member.value,
			fieldPath(path, "numberOfDependentsMoreThan"),
			fmt.Errorf(
				"defines an empty integer range: dependent count must be greater than %d and less than %d",
				moreThan,
				lessThan,
			),
		)
	}

	return fromPatterns, nil
}

func (validator configValidator) dependentCount(
	fields map[string]jsonMember,
	path string,
	name string,
) (int, bool, error) {
	member, ok := fields[name]
	if !ok {
		return 0, false, nil
	}
	memberPath := fieldPath(path, name)
	if err := validator.kind(member.value, memberPath, jsonNumber); err != nil {
		return 0, false, err
	}
	if member.value.text == "" || strings.IndexFunc(member.value.text, func(character rune) bool {
		return character < '0' || character > '9'
	}) >= 0 {
		return 0, false, validator.at(
			member.value,
			memberPath,
			errors.New("must be a non-negative integer without decimal or exponent notation"),
		)
	}
	value, err := strconv.Atoi(member.value.text)
	if err != nil {
		return 0, false, validator.at(
			member.value,
			memberPath,
			errors.New("must be a non-negative integer representable as an int"),
		)
	}

	return value, true, nil
}

func (validator configValidator) to(
	node *jsonNode,
	path string,
	fromPatterns []*regexp.Regexp,
	kind ruleKind,
	ruleScope Scope,
) error {
	allowedFields := []string{
		"path",
		"pathNot",
		"dependencyTypes",
		"dependencyTypesNot",
	}
	if kind == forbiddenRuleKind {
		allowedFields = append(allowedFields, "reachable", "reachableFilePathNot", "moreUnstable")
	}
	fields, err := validator.object(
		node,
		path,
		allowedFields...,
	)
	if err != nil {
		return err
	}
	if kind == requiredRuleKind && len(fields) == 0 {
		return validator.at(node, path, errors.New("must define at least one condition"))
	}
	if ruleScope == ScopeFolder {
		for _, conflict := range []string{
			"reachable",
			"reachableFilePathNot",
			"dependencyTypes",
			"dependencyTypesNot",
		} {
			if member, ok := fields[conflict]; ok {
				return validator.at(
					member.value,
					fieldPath(path, conflict),
					fmt.Errorf(`field %q cannot be combined with scope "folder"`, conflict),
				)
			}
		}
	}

	allowCaptures := true
	if member, ok := fields["moreUnstable"]; ok {
		moreUnstable, validationErr := validator.boolean(member.value, fieldPath(path, "moreUnstable"))
		if validationErr != nil {
			return validationErr
		}
		if !moreUnstable {
			return validator.at(
				member.value,
				fieldPath(path, "moreUnstable"),
				errors.New("must be true when specified"),
			)
		}
		if conflictingMember, ok := fields["reachable"]; ok {
			return validator.at(
				conflictingMember.value,
				fieldPath(path, "reachable"),
				errors.New(`field "reachable" cannot be combined with "moreUnstable"`),
			)
		}
	}
	if member, ok := fields["reachable"]; ok {
		reachable, validationErr := validator.boolean(member.value, fieldPath(path, "reachable"))
		if validationErr != nil {
			return validationErr
		}
		if _, ok := fields["path"]; !ok {
			return validator.at(node, path, errors.New(`field "reachable" requires field "path"`))
		}
		for _, conflict := range []string{"dependencyTypes", "dependencyTypesNot"} {
			if conflictingMember, ok := fields[conflict]; ok {
				return validator.at(
					conflictingMember.value,
					fieldPath(path, conflict),
					fmt.Errorf(`field %q cannot be combined with "reachable"`, conflict),
				)
			}
		}
		allowCaptures = reachable
	}
	if member, ok := fields["reachableFilePathNot"]; ok {
		if _, reachable := fields["reachable"]; !reachable {
			return validator.at(
				member.value,
				fieldPath(path, "reachableFilePathNot"),
				errors.New(`field "reachableFilePathNot" requires field "reachable"`),
			)
		}
		if _, err := validator.regularExpressions(
			member.value,
			fieldPath(path, "reachableFilePathNot"),
		); err != nil {
			return err
		}
	}

	if member, ok := fields["path"]; ok {
		if err := validator.templates(member.value, fieldPath(path, "path"), fromPatterns, allowCaptures); err != nil {
			return err
		}
	}
	if member, ok := fields["pathNot"]; ok {
		if err := validator.templates(member.value, fieldPath(path, "pathNot"), fromPatterns, allowCaptures); err != nil {
			return err
		}
	}
	if member, ok := fields["dependencyTypes"]; ok {
		if err := validator.dependencyTypes(member.value, fieldPath(path, "dependencyTypes")); err != nil {
			return err
		}
	}
	if member, ok := fields["dependencyTypesNot"]; ok {
		if err := validator.dependencyTypes(member.value, fieldPath(path, "dependencyTypesNot")); err != nil {
			return err
		}
	}
	if _, ok := fields["moreUnstable"]; ok {
		if member, specified := fields["dependencyTypes"]; specified &&
			!dependencyTypeNodeContains(member.value, DependencyTypeLocal) {
			return validator.at(
				member.value,
				fieldPath(path, "dependencyTypes"),
				errors.New(`must include "local" when combined with "moreUnstable"`),
			)
		}
		if member, specified := fields["dependencyTypesNot"]; specified &&
			dependencyTypeNodeContains(member.value, DependencyTypeLocal) {
			return validator.at(
				member.value,
				fieldPath(path, "dependencyTypesNot"),
				errors.New(`must not include "local" when combined with "moreUnstable"`),
			)
		}
	}

	return nil
}

func dependencyTypeNodeContains(node *jsonNode, want DependencyType) bool {
	for _, item := range node.items {
		if DependencyType(item.text) == want {
			return true
		}
	}

	return false
}

func (validator configValidator) regularExpressions(node *jsonNode, path string) ([]*regexp.Regexp, error) {
	items, err := validator.nonEmptyStringArray(node, path)
	if err != nil {
		return nil, err
	}

	patterns := make([]*regexp.Regexp, 0, len(items))
	for index, item := range items {
		pattern, compileErr := regexp.Compile(item.text)
		if compileErr != nil {
			return nil, validator.at(
				item,
				indexedPath(path, index),
				fmt.Errorf("invalid regular expression %q: %w", item.text, compileErr),
			)
		}
		patterns = append(patterns, pattern)
	}

	return patterns, nil
}

func (validator configValidator) templates(
	node *jsonNode,
	path string,
	fromPatterns []*regexp.Regexp,
	allowCaptures bool,
) error {
	items, err := validator.nonEmptyStringArray(node, path)
	if err != nil {
		return err
	}

	for index, item := range items {
		templatePath := indexedPath(path, index)
		masked, highestReference, rewriteErr := rewriteCaptures(item.text, func(int) (string, error) {
			return "(?:capture)", nil
		})
		if rewriteErr != nil {
			return validator.at(item, templatePath, rewriteErr)
		}
		if _, compileErr := regexp.Compile(masked); compileErr != nil {
			return validator.at(
				item,
				templatePath,
				fmt.Errorf("invalid regular expression %q: %w", item.text, compileErr),
			)
		}
		if !allowCaptures && highestReference > 0 {
			return validator.at(
				item,
				templatePath,
				errors.New("capture references are not allowed with reachable: false"),
			)
		}
		if highestReference == 0 {
			continue
		}
		if len(fromPatterns) == 0 {
			return validator.at(
				item,
				templatePath,
				fmt.Errorf("capture reference $%d requires from.path", highestReference),
			)
		}
		for fromIndex, pattern := range fromPatterns {
			if pattern.NumSubexp() < highestReference {
				return validator.at(
					item,
					templatePath,
					fmt.Errorf(
						"capture reference $%d exceeds the %d groups in from.path[%d]",
						highestReference,
						pattern.NumSubexp(),
						fromIndex,
					),
				)
			}
		}
	}

	return nil
}

func (validator configValidator) boolean(node *jsonNode, path string) (bool, error) {
	if err := validator.kind(node, path, jsonBoolean); err != nil {
		return false, err
	}

	// parseJSON has already verified the document syntax, so a boolean node at
	// its recorded offset starts with either 't' or 'f'.
	return validator.data[node.offset] == 't', nil
}

func (validator configValidator) dependencyTypes(node *jsonNode, path string) error {
	items, err := validator.nonEmptyStringArray(node, path)
	if err != nil {
		return err
	}

	for index, item := range items {
		if !validDependencyType(DependencyType(item.text)) {
			return validator.at(
				item,
				indexedPath(path, index),
				fmt.Errorf("unknown dependency type %q", item.text),
			)
		}
	}

	return nil
}

func (validator configValidator) severity(node *jsonNode, path string) error {
	if err := validator.kind(node, path, jsonString); err != nil {
		return err
	}
	if !validSeverity(Severity(node.text)) {
		return validator.at(node, path, fmt.Errorf("unknown severity %q", node.text))
	}

	return nil
}

func (validator configValidator) scope(node *jsonNode, path string) (Scope, error) {
	if err := validator.kind(node, path, jsonString); err != nil {
		return "", err
	}
	scope := Scope(node.text)
	if !validScope(scope) {
		return "", validator.at(node, path, fmt.Errorf("unknown scope %q", node.text))
	}

	return scope, nil
}

func (validator configValidator) nonEmptyString(node *jsonNode, path string) error {
	if err := validator.kind(node, path, jsonString); err != nil {
		return err
	}
	if node.text == "" {
		return validator.at(node, path, errors.New("must not be empty"))
	}

	return nil
}

func (validator configValidator) nonEmptyStringArray(node *jsonNode, path string) ([]*jsonNode, error) {
	if err := validator.kind(node, path, jsonArray); err != nil {
		return nil, err
	}
	if len(node.items) == 0 {
		return nil, validator.at(node, path, errors.New("must contain at least one item"))
	}
	for index, item := range node.items {
		if err := validator.kind(item, indexedPath(path, index), jsonString); err != nil {
			return nil, err
		}
	}

	return node.items, nil
}

func (validator configValidator) object(node *jsonNode, path string, allowed ...string) (map[string]jsonMember, error) {
	if err := validator.kind(node, path, jsonObject); err != nil {
		return nil, err
	}

	allowedSet := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		allowedSet[name] = struct{}{}
	}
	result := make(map[string]jsonMember, len(node.members))
	for _, member := range node.members {
		memberPath := fieldPath(path, member.name)
		if _, duplicate := result[member.name]; duplicate {
			return nil, positionedError(
				validator.source,
				validator.data,
				memberPath,
				member.nameOffset,
				fmt.Errorf("duplicate field %q", member.name),
			)
		}
		if _, ok := allowedSet[member.name]; !ok {
			return nil, positionedError(
				validator.source,
				validator.data,
				memberPath,
				member.nameOffset,
				fmt.Errorf("unknown field %q", member.name),
			)
		}
		result[member.name] = member
	}

	return result, nil
}

func (validator configValidator) required(
	fields map[string]jsonMember,
	object *jsonNode,
	path string,
	name string,
) (jsonMember, error) {
	member, ok := fields[name]
	if !ok {
		return jsonMember{}, validator.at(
			object,
			path,
			fmt.Errorf("missing required field %q", name),
		)
	}

	return member, nil
}

func (validator configValidator) kind(node *jsonNode, path string, want jsonKind) error {
	if node.kind == want {
		return nil
	}

	return validator.at(
		node,
		path,
		fmt.Errorf("must be %s, got %s", kindName(want), kindName(node.kind)),
	)
}

func (validator configValidator) at(node *jsonNode, path string, err error) *Error {
	return positionedError(validator.source, validator.data, path, node.offset, err)
}

func validSeverity(severity Severity) bool {
	switch severity {
	case SeverityError, SeverityWarn, SeverityInfo, SeverityIgnore:
		return true
	default:
		return false
	}
}

func validScope(scope Scope) bool {
	switch scope {
	case ScopeModule, ScopeFolder:
		return true
	default:
		return false
	}
}

func validDependencyType(dependencyType DependencyType) bool {
	switch dependencyType {
	case DependencyTypeStdlib, DependencyTypeLocal, DependencyTypeModule, DependencyTypeUnresolved:
		return true
	default:
		return false
	}
}

func kindName(kind jsonKind) string {
	switch kind {
	case jsonObject:
		return "an object"
	case jsonArray:
		return "an array"
	case jsonString:
		return "a string"
	case jsonNumber:
		return "a number"
	case jsonBoolean:
		return "a boolean"
	case jsonNull:
		return "null"
	default:
		return "an unknown JSON value"
	}
}

func fieldPath(parent, field string) string {
	if isJSONPathIdentifier(field) {
		return parent + "." + field
	}

	return parent + "[" + strconv.Quote(field) + "]"
}

func indexedPath(parent string, index int) string {
	return fmt.Sprintf("%s[%d]", parent, index)
}

// ExpandCaptures substitutes $1, $2, and later numeric references in pattern.
// Captures must have the shape returned by regexp.FindStringSubmatch, including
// the complete match at index zero. Substituted text is regexp-quoted and put
// in a non-capturing group so it remains one valid regular-expression atom,
// including when the captured text is empty.
func ExpandCaptures(pattern string, captures []string) (string, error) {
	expanded, _, err := rewriteCaptures(pattern, func(index int) (string, error) {
		if index >= len(captures) {
			return "", fmt.Errorf("capture reference $%d is unavailable", index)
		}

		return "(?:" + regexp.QuoteMeta(captures[index]) + ")", nil
	})

	return expanded, err
}

func rewriteCaptures(
	pattern string,
	replacement func(index int) (string, error),
) (string, int, error) {
	var result strings.Builder
	result.Grow(len(pattern))
	highestReference := 0
	inCharacterClass := false
	characterClassFirst := false
	characterClassCanNegate := false
	inPOSIXClass := false
	inQuotedLiteral := false

	for index := 0; index < len(pattern); {
		current := pattern[index]
		if inQuotedLiteral {
			if current == '\\' && index+1 < len(pattern) && pattern[index+1] == 'E' {
				result.WriteString(`\E`)
				index += 2
				inQuotedLiteral = false
				continue
			}
			if current == '$' && index+1 < len(pattern) && ((pattern[index+1] >= '0' && pattern[index+1] <= '9') ||
				pattern[index+1] == '{' ||
				isASCIIIdentifierStart(pattern[index+1])) {
				return "", 0, errors.New("capture references are not allowed inside \\Q...\\E quoted literals")
			}
			result.WriteByte(current)
			index++
			continue
		}
		if current == '\\' {
			result.WriteByte(current)
			index++
			if index < len(pattern) {
				result.WriteByte(pattern[index])
				if pattern[index] == 'Q' {
					inQuotedLiteral = true
				}
				index++
			}
			if inCharacterClass {
				characterClassFirst = false
			}
			continue
		}
		if inCharacterClass && characterClassFirst && characterClassCanNegate && current == '^' {
			result.WriteByte(current)
			index++
			characterClassCanNegate = false
			continue
		}
		if inCharacterClass && !inPOSIXClass && current == '[' && index+1 < len(pattern) && pattern[index+1] == ':' {
			inPOSIXClass = true
			result.WriteByte(current)
			index++
			continue
		}
		if inPOSIXClass && current == ':' && index+1 < len(pattern) && pattern[index+1] == ']' {
			inPOSIXClass = false
			characterClassFirst = false
			result.WriteString(":]")
			index += 2
			continue
		}
		if current == '[' && !inCharacterClass {
			inCharacterClass = true
			characterClassFirst = true
			characterClassCanNegate = true
			result.WriteByte(current)
			index++
			continue
		}
		if current == ']' && inCharacterClass && !inPOSIXClass && !characterClassFirst {
			inCharacterClass = false
			characterClassFirst = false
			characterClassCanNegate = false
			result.WriteByte(current)
			index++
			continue
		}
		if current == '$' && index+1 < len(pattern) && pattern[index+1] == '{' {
			return "", 0, errors.New("braced capture references are unsupported; use $1, $2, and later numeric references")
		}
		if current == '$' && index+1 < len(pattern) && ((pattern[index+1] >= 'A' && pattern[index+1] <= 'Z') ||
			(pattern[index+1] >= 'a' && pattern[index+1] <= 'z') ||
			pattern[index+1] == '_') {
			return "", 0, errors.New("named capture references are unsupported; use $1, $2, and later numeric references")
		}
		if current != '$' || index+1 >= len(pattern) || pattern[index+1] < '0' || pattern[index+1] > '9' {
			result.WriteByte(current)
			if inCharacterClass {
				characterClassFirst = false
			}
			index++
			continue
		}

		end := index + 1
		for end < len(pattern) && pattern[end] >= '0' && pattern[end] <= '9' {
			end++
		}
		referenceText := pattern[index+1 : end]
		if inCharacterClass {
			return "", 0, fmt.Errorf("capture reference $%s is not allowed in a character class", referenceText)
		}
		if referenceText[0] == '0' {
			return "", 0, fmt.Errorf("invalid capture reference $%s; references start at $1", referenceText)
		}
		reference, err := strconv.Atoi(referenceText)
		if err != nil {
			return "", 0, fmt.Errorf("invalid capture reference $%s: %w", referenceText, err)
		}
		replacementText, err := replacement(reference)
		if err != nil {
			return "", 0, err
		}
		result.WriteString(replacementText)
		highestReference = max(highestReference, reference)
		index = end
	}

	return result.String(), highestReference, nil
}

func isJSONPathIdentifier(value string) bool {
	if value == "" || !isASCIIIdentifierStart(value[0]) {
		return false
	}
	for index := 1; index < len(value); index++ {
		if !isASCIIIdentifierStart(value[index]) && (value[index] < '0' || value[index] > '9') {
			return false
		}
	}

	return true
}

func isASCIIIdentifierStart(value byte) bool {
	return (value >= 'A' && value <= 'Z') || (value >= 'a' && value <= 'z') || value == '_'
}
