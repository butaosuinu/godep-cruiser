// Package baseline records known dependency violations and reports entries
// that no longer match a current violation.
//
// Entries deliberately use exact rule, source path, and target identities
// rather than regular expressions. Edge identities use the raw import path
// when one exists and otherwise use the synthesized target package path. Exact
// identities make it possible to determine when an exception has become stale
// and should be removed. Source-only violations omit the target.
package baseline
