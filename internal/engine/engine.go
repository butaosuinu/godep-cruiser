package engine

import (
	"cmp"
	"errors"
	"fmt"
	"path"
	"slices"

	"github.com/butaosuinu/godep-cruiser/config"
	"github.com/butaosuinu/godep-cruiser/internal/graph"
	"github.com/butaosuinu/godep-cruiser/internal/scanner"
)

// Evaluate applies configuration to files and returns violations in a stable
// order. Forbidden and required rules with ignore severity are not evaluated.
// A nil Allowed slice disables fail-closed checking; a non-nil empty slice
// rejects every dependency. An ignore AllowedSeverity also disables fail-closed
// checking.
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
	requiredRules, err := compileRequiredRules(configuration.Required)
	if err != nil {
		return nil, err
	}

	packageGraph := graph.Build(files)
	factsByPath := collectFileFacts(files, packageGraph)
	violations := make([]Violation, 0)
	for _, rule := range forbiddenRules {
		if rule.scope == config.ScopeFolder {
			violations, err = appendFolderViolations(
				violations,
				rule,
				files,
				packageGraph,
			)
			if err != nil {
				return nil, fmt.Errorf("forbidden rule %q to: %w", rule.name, err)
			}
			continue
		}
		if rule.to.reachable != nil {
			var matchErr error
			if *rule.to.reachable {
				violations, matchErr = appendReachableViolations(
					violations,
					rule,
					files,
					factsByPath,
					packageGraph,
				)
			} else {
				violations, matchErr = appendUnreachableViolations(
					violations,
					rule,
					files,
					factsByPath,
					packageGraph,
				)
			}
			if matchErr != nil {
				return nil, fmt.Errorf("forbidden rule %q to: %w", rule.name, matchErr)
			}
			continue
		}
		if rule.sourceOnly {
			violations = appendSourceViolations(violations, rule, files, factsByPath)
			continue
		}
		for _, file := range files {
			captures, matched := rule.from.matches(file, factsByPath[file.Path])
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
				allowed, matchErr := isAllowed(allowedRules, file, dependency, factsByPath[file.Path])
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

	for _, rule := range requiredRules {
		for _, file := range files {
			captures, matched := rule.from.matches(file, factsByPath[file.Path])
			if !matched {
				continue
			}
			satisfied := false
			for _, dependency := range file.Imports {
				matched, matchErr := rule.to.matches(dependency, captures)
				if matchErr != nil {
					return nil, fmt.Errorf("required rule %q to: %w", rule.name, matchErr)
				}
				if matched {
					satisfied = true
					break
				}
			}
			if !satisfied {
				violations = append(violations, sourceViolation(
					rule.name,
					rule.comment,
					rule.severity,
					ViolationKindRequired,
					file,
				))
			}
		}
	}

	sortViolations(violations)

	return violations, nil
}

func appendFolderViolations(
	violations []Violation,
	rule compiledForbiddenRule,
	files []scanner.File,
	packageGraph graph.Graph,
) ([]Violation, error) {
	for _, packagePath := range scannedPackagePaths(files) {
		captures, matched := rule.from.matches(scanner.File{Path: packagePath}, fileFacts{
			numberOfDependents: packageGraph.FanIn(packagePath),
		})
		if !matched {
			continue
		}
		for _, dependencyPath := range packageGraph.Dependencies(packagePath) {
			matched, err := rule.to.matchesPackagePath(dependencyPath, captures)
			if err != nil {
				return nil, err
			}
			if matched {
				violations = append(violations, folderViolation(rule, packagePath, dependencyPath))
			}
		}
	}

	return violations, nil
}

func scannedPackagePaths(files []scanner.File) []string {
	packages := make(map[string]struct{})
	for _, file := range files {
		if file.PackagePath == "" {
			continue
		}
		packages[path.Clean(file.PackagePath)] = struct{}{}
	}
	packagePaths := make([]string, 0, len(packages))
	for packagePath := range packages {
		packagePaths = append(packagePaths, packagePath)
	}
	slices.Sort(packagePaths)

	return packagePaths
}

func folderViolation(rule compiledForbiddenRule, from, to string) Violation {
	return Violation{
		Rule:     rule.name,
		Comment:  rule.comment,
		Severity: rule.severity,
		Kind:     ViolationKindForbidden,
		From:     Source{Path: from},
		To: &Dependency{
			Path: to,
			Type: scanner.DependencyTypeLocal,
		},
	}
}

func appendReachableViolations(
	violations []Violation,
	rule compiledForbiddenRule,
	files []scanner.File,
	factsByPath map[string]fileFacts,
	packageGraph graph.Graph,
) ([]Violation, error) {
	for _, file := range files {
		captures, matched := rule.from.matches(file, factsByPath[file.Path])
		if !matched {
			continue
		}

		lowestLineByTarget := make(map[string]int)
		for _, dependency := range file.Imports {
			if dependency.Type != scanner.DependencyTypeLocal || dependency.ResolvedPath == "" {
				continue
			}
			for _, targetPackage := range packageGraph.ForwardClosure(dependency.ResolvedPath) {
				matched, err := rule.to.matchesPackagePath(targetPackage, captures)
				if err != nil {
					return nil, err
				}
				if !matched {
					continue
				}
				line, exists := lowestLineByTarget[targetPackage]
				if !exists || dependency.Line < line {
					lowestLineByTarget[targetPackage] = dependency.Line
				}
			}
		}

		for targetPackage, line := range lowestLineByTarget {
			violations = append(violations, reachableViolation(
				rule.name,
				rule.comment,
				rule.severity,
				file,
				targetPackage,
				line,
			))
		}
	}

	return violations, nil
}

func appendUnreachableViolations(
	violations []Violation,
	rule compiledForbiddenRule,
	files []scanner.File,
	factsByPath map[string]fileFacts,
	packageGraph graph.Graph,
) ([]Violation, error) {
	seedPackages := make([]string, 0)
	for _, file := range files {
		if _, matched := rule.from.matches(file, factsByPath[file.Path]); matched {
			seedPackages = append(seedPackages, file.PackagePath)
		}
	}

	reachablePackages := make(map[string]struct{})
	for _, packagePath := range packageGraph.ForwardClosure(seedPackages...) {
		reachablePackages[packagePath] = struct{}{}
	}
	for _, file := range files {
		matched, err := rule.to.matchesPackagePath(file.PackagePath, nil)
		if err != nil {
			return nil, err
		}
		if !matched {
			continue
		}
		if _, reachable := reachablePackages[file.PackagePath]; reachable {
			continue
		}
		violations = append(violations, sourceViolation(
			rule.name,
			rule.comment,
			rule.severity,
			ViolationKindUnreachable,
			file,
		))
	}

	return violations, nil
}

func appendSourceViolations(
	violations []Violation,
	rule compiledForbiddenRule,
	files []scanner.File,
	factsByPath map[string]fileFacts,
) []Violation {
	for _, file := range files {
		if _, matched := rule.from.matches(file, factsByPath[file.Path]); !matched {
			continue
		}
		violations = append(violations, sourceViolation(
			rule.name,
			rule.comment,
			rule.severity,
			ViolationKindForbidden,
			file,
		))
	}

	return violations
}

func sourceViolation(
	rule, comment string,
	severity config.Severity,
	kind ViolationKind,
	file scanner.File,
) Violation {
	return Violation{
		Rule:     rule,
		Comment:  comment,
		Severity: severity,
		Kind:     kind,
		From: Source{
			Path:        file.Path,
			Line:        file.PackageLine,
			PackageName: file.Package,
		},
	}
}

func isAllowed(
	rules []compiledAllowedRule,
	file scanner.File,
	dependency scanner.Import,
	facts fileFacts,
) (bool, error) {
	for _, rule := range rules {
		captures, matched := rule.from.matches(file, facts)
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

func reachableViolation(
	rule, comment string,
	severity config.Severity,
	file scanner.File,
	targetPackage string,
	line int,
) Violation {
	return Violation{
		Rule:     rule,
		Comment:  comment,
		Severity: severity,
		Kind:     ViolationKindReachable,
		From: Source{
			Path:        file.Path,
			Line:        line,
			PackageName: file.Package,
		},
		To: &Dependency{
			Path: targetPackage,
			Type: scanner.DependencyTypeLocal,
		},
	}
}

func findOrphans(files []scanner.File, packageGraph graph.Graph) map[string]bool {
	orphans := make(map[string]bool, len(files))
	for _, file := range files {
		orphans[file.Path] = len(file.Imports) == 0 && !packageGraph.IsImported(file.PackagePath)
	}

	return orphans
}

func collectFileFacts(files []scanner.File, packageGraph graph.Graph) map[string]fileFacts {
	orphans := findOrphans(files, packageGraph)
	factsByPath := make(map[string]fileFacts, len(files))
	for _, file := range files {
		factsByPath[file.Path] = fileFacts{
			orphan:             orphans[file.Path],
			numberOfDependents: packageGraph.FanIn(file.PackagePath),
		}
	}

	return factsByPath
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
