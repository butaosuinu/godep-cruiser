package cruiser

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/butaosuinu/godep-cruiser/config"
	"github.com/butaosuinu/godep-cruiser/internal/baseline"
	"github.com/butaosuinu/godep-cruiser/internal/engine"
	"github.com/butaosuinu/godep-cruiser/internal/scanner"
)

// Validate scans one Go module and evaluates configuration against every Go
// file below Options.ScanRoot. Programmatically constructed configurations are
// normalized and validated with the same rules as config.Load.
func Validate(configuration *config.Config, options Options) (Result, error) {
	validatedConfig, err := normalizeConfiguration(configuration)
	if err != nil {
		return Result{}, err
	}

	scanRoot := options.ScanRoot
	if scanRoot == "" {
		scanRoot = "."
	}
	goModPath := options.GoModPath
	if goModPath == "" {
		goModPath = filepath.Join(scanRoot, "go.mod")
	}

	resolver, err := scanner.NewResolverFromGoMod(goModPath)
	if err != nil {
		return Result{}, fmt.Errorf("initialize resolver: %w", err)
	}
	files, err := scanner.Scan(scanRoot, resolver)
	if err != nil {
		return Result{}, fmt.Errorf("scan dependencies: %w", err)
	}
	violations, err := engine.Evaluate(validatedConfig, files)
	if err != nil {
		return Result{}, fmt.Errorf("evaluate dependency rules: %w", err)
	}

	if options.Baseline == nil {
		return Result{Violations: violations}, nil
	}

	known := *options.Baseline
	if err := baseline.Write(io.Discard, known); err != nil {
		return Result{}, fmt.Errorf("validate baseline: %w", err)
	}
	partitioned := baseline.Apply(known, violations)

	return Result{
		Violations: partitioned.Violations,
		Known:      partitioned.Known,
		Stale:      partitioned.Stale,
	}, nil
}

func normalizeConfiguration(configuration *config.Config) (*config.Config, error) {
	if configuration == nil {
		return nil, errors.New("validate configuration: configuration is nil")
	}
	if err := validateProgrammaticMatcherSlices(configuration); err != nil {
		return nil, fmt.Errorf("validate configuration: %w", err)
	}

	encoded, err := json.Marshal(configuration)
	if err != nil {
		return nil, fmt.Errorf("validate configuration: encode configuration: %w", err)
	}
	validated, err := config.Parse(encoded)
	if err != nil {
		return nil, fmt.Errorf("validate configuration: %w", err)
	}

	return validated, nil
}

func validateProgrammaticMatcherSlices(configuration *config.Config) error {
	for index, rule := range configuration.Forbidden {
		prefix := fmt.Sprintf("$.forbidden[%d]", index)
		if err := validateFromSlices(rule.From, prefix+".from"); err != nil {
			return err
		}
		if err := validateToSlices(rule.To, prefix+".to"); err != nil {
			return err
		}
	}
	for index, rule := range configuration.Required {
		prefix := fmt.Sprintf("$.required[%d]", index)
		if err := validateFromSlices(rule.From, prefix+".from"); err != nil {
			return err
		}
		if err := validateToSlices(rule.To, prefix+".to"); err != nil {
			return err
		}
	}
	for index, rule := range configuration.Allowed {
		prefix := fmt.Sprintf("$.allowed[%d]", index)
		if err := validateFromSlices(rule.From, prefix+".from"); err != nil {
			return err
		}
		if err := validateToSlices(rule.To, prefix+".to"); err != nil {
			return err
		}
	}

	return nil
}

func validateFromSlices(from config.From, prefix string) error {
	for _, field := range []struct {
		path   string
		values []string
	}{
		{path: prefix + ".path", values: from.Path},
		{path: prefix + ".pathNot", values: from.PathNot},
		{path: prefix + ".packageName", values: from.PackageName},
	} {
		if field.values != nil && len(field.values) == 0 {
			return fmt.Errorf("%s must contain at least one item", field.path)
		}
	}

	return nil
}

func validateToSlices(to config.To, prefix string) error {
	for _, field := range []struct {
		path    string
		present bool
		empty   bool
	}{
		{path: prefix + ".path", present: to.Path != nil, empty: len(to.Path) == 0},
		{path: prefix + ".pathNot", present: to.PathNot != nil, empty: len(to.PathNot) == 0},
		{
			path:    prefix + ".dependencyTypes",
			present: to.DependencyTypes != nil,
			empty:   len(to.DependencyTypes) == 0,
		},
		{
			path:    prefix + ".dependencyTypesNot",
			present: to.DependencyTypesNot != nil,
			empty:   len(to.DependencyTypesNot) == 0,
		},
	} {
		if field.present && field.empty {
			return fmt.Errorf("%s must contain at least one item", field.path)
		}
	}

	return nil
}
