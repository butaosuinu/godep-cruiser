package engine

import (
	"cmp"
	"errors"
	"fmt"
	"path"
	"slices"

	"github.com/butaosuinu/godep-cruiser/config"
	"github.com/butaosuinu/godep-cruiser/internal/scanner"
)

// Evaluate applies configuration to files and returns violations in a stable
// order. Forbidden rules with ignore severity are not evaluated. A nil Allowed
// slice disables fail-closed checking; a non-nil empty slice rejects every
// dependency. An ignore AllowedSeverity also disables fail-closed checking.
func Evaluate(configuration *config.Config, files []scanner.File) ([]Violation, error) {
	if configuration == nil {
		return nil, errors.New("configuration is nil")
	}

	forbiddenRules, err := compileForbiddenRules(configuration.Forbidden)
	if err != nil {
		return nil, err
	}
	allowedSeverity := effectiveSeverity(configuration.AllowedSeverity)
	var allowedRules []compiledAllowedRule
	if configuration.Allowed != nil && allowedSeverity != config.SeverityIgnore {
		allowedRules, err = compileAllowedRules(configuration.Allowed)
		if err != nil {
			return nil, err
		}
	}

	orphans := findOrphans(files)
	violations := make([]Violation, 0)
	for _, rule := range forbiddenRules {
		if rule.sourceOnly {
			violations = appendSourceViolations(violations, rule, files, orphans)
			continue
		}
		for _, file := range files {
			captures, matched := rule.from.matches(file, orphans[file.Path])
			if !matched {
				continue
			}
			for _, dependency := range file.Imports {
				matched, matchErr := rule.to.matches(dependency, captures)
				if matchErr != nil {
					return nil, fmt.Errorf("forbidden rule %q to: %w", rule.name, matchErr)
				}
				if matched {
					violations = append(violations, edgeViolation(
						rule.name,
						rule.comment,
						rule.severity,
						ViolationKindForbidden,
						file,
						dependency,
					))
				}
			}
		}
	}

	if configuration.Allowed != nil && allowedSeverity != config.SeverityIgnore {
		for _, file := range files {
			for _, dependency := range file.Imports {
				allowed, matchErr := isAllowed(allowedRules, file, dependency, orphans[file.Path])
				if matchErr != nil {
					return nil, matchErr
				}
				if !allowed {
					violations = append(violations, edgeViolation(
						NotInAllowedRuleName,
						"",
						allowedSeverity,
						ViolationKindNotAllowed,
						file,
						dependency,
					))
				}
			}
		}
	}

	sortViolations(violations)

	return violations, nil
}

func appendSourceViolations(
	violations []Violation,
	rule compiledForbiddenRule,
	files []scanner.File,
	orphans map[string]bool,
) []Violation {
	for _, file := range files {
		if _, matched := rule.from.matches(file, orphans[file.Path]); !matched {
			continue
		}
		violations = append(violations, Violation{
			Rule:     rule.name,
			Comment:  rule.comment,
			Severity: rule.severity,
			Kind:     ViolationKindForbidden,
			From: Source{
				Path:        file.Path,
				Line:        file.PackageLine,
				PackageName: file.Package,
			},
		})
	}

	return violations
}

func isAllowed(
	rules []compiledAllowedRule,
	file scanner.File,
	dependency scanner.Import,
	orphan bool,
) (bool, error) {
	for _, rule := range rules {
		captures, matched := rule.from.matches(file, orphan)
		if !matched {
			continue
		}
		matched, err := rule.to.matches(dependency, captures)
		if err != nil {
			return false, fmt.Errorf("allowed rule %q to: %w", rule.name, err)
		}
		if matched {
			return true, nil
		}
	}

	return false, nil
}

func edgeViolation(
	rule, comment string,
	severity config.Severity,
	kind ViolationKind,
	file scanner.File,
	dependency scanner.Import,
) Violation {
	return Violation{
		Rule:     rule,
		Comment:  comment,
		Severity: severity,
		Kind:     kind,
		From: Source{
			Path:        file.Path,
			Line:        dependency.Line,
			PackageName: file.Package,
		},
		To: &Dependency{
			Path:       effectiveDependencyPath(dependency),
			ImportPath: dependency.Path,
			Type:       dependency.Type,
		},
	}
}

func findOrphans(files []scanner.File) map[string]bool {
	importedPackages := make(map[string]struct{})
	for _, file := range files {
		for _, dependency := range file.Imports {
			if dependency.Type == scanner.DependencyTypeLocal && dependency.ResolvedPath != "" {
				importedPackages[path.Clean(dependency.ResolvedPath)] = struct{}{}
			}
		}
	}

	orphans := make(map[string]bool, len(files))
	for _, file := range files {
		_, imported := importedPackages[path.Dir(file.Path)]
		orphans[file.Path] = len(file.Imports) == 0 && !imported
	}

	return orphans
}

func sortViolations(violations []Violation) {
	slices.SortFunc(violations, func(left, right Violation) int {
		if byRule := cmp.Compare(left.Rule, right.Rule); byRule != 0 {
			return byRule
		}
		if bySeverity := cmp.Compare(left.Severity, right.Severity); bySeverity != 0 {
			return bySeverity
		}
		if byPath := cmp.Compare(left.From.Path, right.From.Path); byPath != 0 {
			return byPath
		}
		if byLine := cmp.Compare(left.From.Line, right.From.Line); byLine != 0 {
			return byLine
		}
		if byTarget := cmp.Compare(violationTargetPath(left), violationTargetPath(right)); byTarget != 0 {
			return byTarget
		}
		if byType := cmp.Compare(violationDependencyType(left), violationDependencyType(right)); byType != 0 {
			return byType
		}
		if byKind := cmp.Compare(left.Kind, right.Kind); byKind != 0 {
			return byKind
		}

		return cmp.Compare(violationImportPath(left), violationImportPath(right))
	})
}

func violationTargetPath(violation Violation) string {
	if violation.To == nil {
		return ""
	}

	return violation.To.Path
}

func violationDependencyType(violation Violation) scanner.DependencyType {
	if violation.To == nil {
		return ""
	}

	return violation.To.Type
}

func violationImportPath(violation Violation) string {
	if violation.To == nil {
		return ""
	}

	return violation.To.ImportPath
}
