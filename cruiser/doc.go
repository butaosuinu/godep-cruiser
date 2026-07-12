// Package cruiser validates dependency rules for Go source trees.
//
// Validate composes configuration validation, module-aware source scanning,
// rule evaluation, optional baseline filtering, and reporter-ready results.
// The package does not call os.Exit or write implicitly, so command-line tools
// and future testing helpers can share the same validation path.
package cruiser
