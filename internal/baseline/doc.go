// Package baseline records known dependency violations and reports entries
// that no longer match a current violation.
//
// Entries deliberately use exact rule, source path, and raw import-path
// identities rather than regular expressions. Exact identities make it
// possible to determine when an exception has become stale and should be
// removed. Source-only violations omit the import path from their identity.
package baseline
