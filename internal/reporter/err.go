package reporter

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/butaosuinu/godep-cruiser/internal/engine"
)

// WriteErr writes concise human-readable diagnostics in input order.
func WriteErr(writer io.Writer, violations []engine.Violation) error {
	return WriteErrReport(writer, Report{Violations: violations})
}

// WriteErrReport writes violations followed by stale baseline diagnostics in
// input order. Stale baseline entries are always rendered as errors.
func WriteErrReport(writer io.Writer, report Report) error {
	for _, violation := range report.Violations {
		var diagnostic strings.Builder
		fmt.Fprintf(
			&diagnostic,
			"[%s] rule %q: %s:%d",
			singleLine(string(violation.Severity)),
			violation.Rule,
			singleLine(violation.From.Path),
			violation.From.Line,
		)
		if violation.To == nil {
			fmt.Fprintf(&diagnostic, ": %s\n", sourceReason(violation.Kind))
		} else {
			fmt.Fprintf(
				&diagnostic,
				" -> %s (%s): %s\n",
				singleLine(violation.To.Path),
				singleLine(string(violation.To.Type)),
				edgeReason(violation.Kind),
			)
		}

		if comment := collapseWhitespace(violation.Comment); comment != "" {
			fmt.Fprintf(&diagnostic, "  fix: %s\n", comment)
		}
		if _, err := io.WriteString(writer, diagnostic.String()); err != nil {
			return err
		}
	}
	for _, stale := range report.Stale {
		if _, err := fmt.Fprintf(writer, "[error] %s\n", stale.Error()); err != nil {
			return err
		}
	}

	return nil
}

func edgeReason(kind engine.ViolationKind) string {
	switch kind {
	case engine.ViolationKindForbidden:
		return "forbidden dependency"
	case engine.ViolationKindNotAllowed:
		return "dependency matches no allowed rule"
	default:
		return fmt.Sprintf("unknown violation kind %q", kind)
	}
}

func sourceReason(kind engine.ViolationKind) string {
	if kind == engine.ViolationKindForbidden {
		return "forbidden source"
	}

	return fmt.Sprintf("unknown violation kind %q", kind)
}

func collapseWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func singleLine(value string) string {
	quoted := strconv.QuoteToGraphic(value)

	return quoted[1 : len(quoted)-1]
}
