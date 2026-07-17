// Package config loads and validates godep-cruiser rule configuration.
package config

import "encoding/json"

// Severity controls how a rule violation affects reporting.
type Severity string

// Supported rule severities.
const (
	SeverityError  Severity = "error"
	SeverityWarn   Severity = "warn"
	SeverityInfo   Severity = "info"
	SeverityIgnore Severity = "ignore"
)

// Scope controls the graph coordinate at which a forbidden rule is evaluated.
type Scope string

// Supported forbidden-rule scopes.
const (
	ScopeModule Scope = "module"
	ScopeFolder Scope = "folder"
)

// DependencyType classifies a Go import dependency.
type DependencyType string

// Supported dependency classifications.
const (
	DependencyTypeStdlib     DependencyType = "stdlib"
	DependencyTypeLocal      DependencyType = "local"
	DependencyTypeModule     DependencyType = "module"
	DependencyTypeUnresolved DependencyType = "unresolved"
)

// Config is a validated godep-cruiser configuration.
//
// A nil Allowed slice means allowed-rule checking is disabled. A non-nil empty
// Allowed slice enables fail-closed checking without allowing any dependency.
type Config struct {
	Forbidden       []ForbiddenRule `json:"forbidden,omitempty"`
	Required        []RequiredRule  `json:"required,omitempty"`
	Allowed         []AllowedRule   `json:"allowed,omitempty"`
	AllowedSeverity Severity        `json:"allowedSeverity,omitempty"`
}

// MarshalJSON preserves the semantic difference between an omitted allowed
// field and an explicitly empty allowed array.
func (config Config) MarshalJSON() ([]byte, error) {
	type wireConfig struct {
		Forbidden       []ForbiddenRule `json:"forbidden,omitempty"`
		Required        []RequiredRule  `json:"required,omitempty"`
		Allowed         *[]AllowedRule  `json:"allowed,omitempty"`
		AllowedSeverity Severity        `json:"allowedSeverity,omitempty"`
	}

	var allowed *[]AllowedRule
	if config.Allowed != nil {
		allowed = &config.Allowed
	}

	return json.Marshal(wireConfig{
		Forbidden:       config.Forbidden,
		Required:        config.Required,
		Allowed:         allowed,
		AllowedSeverity: config.AllowedSeverity,
	})
}

// ForbiddenRule describes dependencies or files that must be reported.
type ForbiddenRule struct {
	Name     string   `json:"name"`
	Comment  string   `json:"comment,omitempty"`
	Severity Severity `json:"severity,omitempty"`
	Scope    Scope    `json:"scope,omitempty"`
	From     From     `json:"from"`
	To       To       `json:"to"`
}

// RequiredRule describes an import that every matching source file must have.
type RequiredRule struct {
	Name     string   `json:"name"`
	Comment  string   `json:"comment,omitempty"`
	Severity Severity `json:"severity,omitempty"`
	From     From     `json:"from"`
	To       To       `json:"to"`
}

// AllowedRule describes dependencies accepted by fail-closed allowed checking.
// Violations use Config.AllowedSeverity because an unmatched dependency has no
// individual allowed rule from which to obtain a severity.
type AllowedRule struct {
	Name    string `json:"name"`
	Comment string `json:"comment,omitempty"`
	From    From   `json:"from"`
	To      To     `json:"to"`
}

// From contains conditions matched against the importing Go source file or,
// for a folder-scoped forbidden rule, the source package. Path patterns use
// module-relative package paths for folder scope. Pattern slices use OR
// semantics within a field and AND semantics across fields. Pointer fields
// distinguish omitted conditions from false or zero.
type From struct {
	Path                       []string `json:"path,omitempty"`
	PathNot                    []string `json:"pathNot,omitempty"`
	Orphan                     *bool    `json:"orphan,omitempty"`
	PackageName                []string `json:"packageName,omitempty"`
	NumberOfDependentsLessThan *int     `json:"numberOfDependentsLessThan,omitempty"`
	NumberOfDependentsMoreThan *int     `json:"numberOfDependentsMoreThan,omitempty"`
}

// To contains conditions matched against an imported dependency or a package
// in the local dependency graph for reachable and folder-scoped forbidden
// rules. Folder scope uses module-relative package paths. Pattern and
// dependency-type slices use OR semantics within a field and AND semantics
// across fields. Pointer fields distinguish omitted conditions from false.
// ReachableFilePathNot contains ordinary Go regular expressions that
// exclude files from transitive local-package edges by scan-root-relative
// path. An edge remains when any file forming it is not excluded, and
// from.path captures are not expanded.
type To struct {
	Path                 []string         `json:"path,omitempty"`
	PathNot              []string         `json:"pathNot,omitempty"`
	Reachable            *bool            `json:"reachable,omitempty"`
	ReachableFilePathNot []string         `json:"reachableFilePathNot,omitempty"`
	MoreUnstable         *bool            `json:"moreUnstable,omitempty"`
	DependencyTypes      []DependencyType `json:"dependencyTypes,omitempty"`
	DependencyTypesNot   []DependencyType `json:"dependencyTypesNot,omitempty"`
}
