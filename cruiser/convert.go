package cruiser

import (
	"github.com/butaosuinu/godep-cruiser/config"
	"github.com/butaosuinu/godep-cruiser/internal/engine"
	"github.com/butaosuinu/godep-cruiser/internal/scanner"
)

func fromEngineViolations(violations []engine.Violation) []Violation {
	converted := make([]Violation, len(violations))
	for index, violation := range violations {
		converted[index] = fromEngineViolation(violation)
	}

	return converted
}

func fromEngineViolation(violation engine.Violation) Violation {
	converted := Violation{
		Rule:     violation.Rule,
		Comment:  violation.Comment,
		Severity: violation.Severity,
		Kind:     ViolationKind(violation.Kind),
		From: Source{
			Path:        violation.From.Path,
			Line:        violation.From.Line,
			PackageName: violation.From.PackageName,
		},
	}
	if violation.To != nil {
		converted.To = &Dependency{
			Path:       violation.To.Path,
			ImportPath: violation.To.ImportPath,
			Type:       config.DependencyType(violation.To.Type),
		}
	}

	return converted
}

func toEngineViolations(violations []Violation) []engine.Violation {
	converted := make([]engine.Violation, len(violations))
	for index, violation := range violations {
		converted[index] = toEngineViolation(violation)
	}

	return converted
}

func toEngineViolation(violation Violation) engine.Violation {
	converted := engine.Violation{
		Rule:     violation.Rule,
		Comment:  violation.Comment,
		Severity: violation.Severity,
		Kind:     engine.ViolationKind(violation.Kind),
		From: engine.Source{
			Path:        violation.From.Path,
			Line:        violation.From.Line,
			PackageName: violation.From.PackageName,
		},
	}
	if violation.To != nil {
		converted.To = &engine.Dependency{
			Path:       violation.To.Path,
			ImportPath: violation.To.ImportPath,
			Type:       scanner.DependencyType(violation.To.Type),
		}
	}

	return converted
}
