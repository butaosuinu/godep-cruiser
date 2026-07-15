package engine

import (
	"fmt"
	"regexp"

	"github.com/butaosuinu/godep-cruiser/config"
	"github.com/butaosuinu/godep-cruiser/internal/scanner"
)

type fromMatcher struct {
	path        []*regexp.Regexp
	pathNot     []*regexp.Regexp
	orphan      *bool
	packageName []*regexp.Regexp
}

type toMatcher struct {
	path               []string
	pathNot            []string
	dependencyTypes    []config.DependencyType
	dependencyTypesNot []config.DependencyType
}

type compiledForbiddenRule struct {
	name       string
	comment    string
	severity   config.Severity
	from       fromMatcher
	to         toMatcher
	sourceOnly bool
}

type compiledAllowedRule struct {
	name string
	from fromMatcher
	to   toMatcher
}

type compiledRequiredRule struct {
	name     string
	comment  string
	severity config.Severity
	from     fromMatcher
	to       toMatcher
}

func compileForbiddenRules(rules []config.ForbiddenRule) ([]compiledForbiddenRule, error) {
	compiled := make([]compiledForbiddenRule, 0, len(rules))
	for index, rule := range rules {
		severity := effectiveSeverity(rule.Severity)
		if severity == config.SeverityIgnore {
			continue
		}
		from, err := compileFrom(rule.From)
		if err != nil {
			return nil, fmt.Errorf("forbidden[%d] %q: %w", index, rule.Name, err)
		}
		compiled = append(compiled, compiledForbiddenRule{
			name:       rule.Name,
			comment:    rule.Comment,
			severity:   severity,
			from:       from,
			to:         compileTo(rule.To),
			sourceOnly: isSourceOnly(rule.From, rule.To),
		})
	}

	return compiled, nil
}

func compileAllowedRules(rules []config.AllowedRule) ([]compiledAllowedRule, error) {
	compiled := make([]compiledAllowedRule, 0, len(rules))
	for index, rule := range rules {
		from, err := compileFrom(rule.From)
		if err != nil {
			return nil, fmt.Errorf("allowed[%d] %q: %w", index, rule.Name, err)
		}
		compiled = append(compiled, compiledAllowedRule{
			name: rule.Name,
			from: from,
			to:   compileTo(rule.To),
		})
	}

	return compiled, nil
}

func compileRequiredRules(rules []config.RequiredRule) ([]compiledRequiredRule, error) {
	compiled := make([]compiledRequiredRule, 0, len(rules))
	for index, rule := range rules {
		severity := effectiveSeverity(rule.Severity)
		if severity == config.SeverityIgnore {
			continue
		}
		from, err := compileFrom(rule.From)
		if err != nil {
			return nil, fmt.Errorf("required[%d] %q: %w", index, rule.Name, err)
		}
		compiled = append(compiled, compiledRequiredRule{
			name:     rule.Name,
			comment:  rule.Comment,
			severity: severity,
			from:     from,
			to:       compileTo(rule.To),
		})
	}

	return compiled, nil
}

func compileFrom(from config.From) (fromMatcher, error) {
	pathPatterns, err := compilePatterns("from.path", from.Path)
	if err != nil {
		return fromMatcher{}, err
	}
	pathNotPatterns, err := compilePatterns("from.pathNot", from.PathNot)
	if err != nil {
		return fromMatcher{}, err
	}
	packagePatterns, err := compilePatterns("from.packageName", from.PackageName)
	if err != nil {
		return fromMatcher{}, err
	}

	return fromMatcher{
		path:        pathPatterns,
		pathNot:     pathNotPatterns,
		orphan:      from.Orphan,
		packageName: packagePatterns,
	}, nil
}

func compilePatterns(field string, patterns []string) ([]*regexp.Regexp, error) {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for index, pattern := range patterns {
		expression, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("%s[%d] %q: %w", field, index, pattern, err)
		}
		compiled = append(compiled, expression)
	}

	return compiled, nil
}

func compileTo(to config.To) toMatcher {
	return toMatcher{
		path:               to.Path,
		pathNot:            to.PathNot,
		dependencyTypes:    to.DependencyTypes,
		dependencyTypesNot: to.DependencyTypesNot,
	}
}

func (matcher fromMatcher) matches(file scanner.File, orphan bool) ([]string, bool) {
	var captures []string
	if len(matcher.path) > 0 {
		for _, pattern := range matcher.path {
			captures = pattern.FindStringSubmatch(file.Path)
			if captures != nil {
				break
			}
		}
		if captures == nil {
			return nil, false
		}
	}
	if matchesAny(matcher.pathNot, file.Path) {
		return nil, false
	}
	if matcher.orphan != nil && *matcher.orphan != orphan {
		return nil, false
	}
	if len(matcher.packageName) > 0 && !matchesAny(matcher.packageName, file.Package) {
		return nil, false
	}

	return captures, true
}

func matchesAny(patterns []*regexp.Regexp, value string) bool {
	for _, pattern := range patterns {
		if pattern.MatchString(value) {
			return true
		}
	}

	return false
}

func (matcher toMatcher) matches(dependency scanner.Import, captures []string) (bool, error) {
	dependencyPath := effectiveDependencyPath(dependency)
	if len(matcher.path) > 0 {
		matched, err := matchesAnyTemplate(matcher.path, dependencyPath, captures)
		if err != nil || !matched {
			return false, err
		}
	}
	if len(matcher.pathNot) > 0 {
		matched, err := matchesAnyTemplate(matcher.pathNot, dependencyPath, captures)
		if err != nil || matched {
			return false, err
		}
	}
	if len(matcher.dependencyTypes) > 0 && !containsDependencyType(matcher.dependencyTypes, dependency.Type) {
		return false, nil
	}
	if containsDependencyType(matcher.dependencyTypesNot, dependency.Type) {
		return false, nil
	}

	return true, nil
}

func matchesAnyTemplate(patterns []string, value string, captures []string) (bool, error) {
	for index, pattern := range patterns {
		expanded, err := config.ExpandCaptures(pattern, captures)
		if err != nil {
			return false, fmt.Errorf("pattern[%d] %q: %w", index, pattern, err)
		}
		expression, err := regexp.Compile(expanded)
		if err != nil {
			return false, fmt.Errorf("pattern[%d] %q expanded to %q: %w", index, pattern, expanded, err)
		}
		if expression.MatchString(value) {
			return true, nil
		}
	}

	return false, nil
}

func containsDependencyType(types []config.DependencyType, dependencyType scanner.DependencyType) bool {
	for _, candidate := range types {
		if string(candidate) == string(dependencyType) {
			return true
		}
	}

	return false
}

func isSourceOnly(from config.From, to config.To) bool {
	hasSourcePredicate := from.Orphan != nil || len(from.PackageName) > 0
	return hasSourcePredicate && isEmptyTo(to)
}

func isEmptyTo(to config.To) bool {
	return len(to.Path) == 0 &&
		len(to.PathNot) == 0 &&
		len(to.DependencyTypes) == 0 &&
		len(to.DependencyTypesNot) == 0
}

func effectiveSeverity(severity config.Severity) config.Severity {
	if severity == "" {
		return config.SeverityWarn
	}

	return severity
}

func effectiveDependencyPath(dependency scanner.Import) string {
	if dependency.ResolvedPath != "" {
		return dependency.ResolvedPath
	}

	return dependency.Path
}
