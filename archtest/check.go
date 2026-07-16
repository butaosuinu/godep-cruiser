package archtest

import (
	"bytes"
	"testing"

	"github.com/butaosuinu/godep-cruiser/config"
	"github.com/butaosuinu/godep-cruiser/cruiser"
)

// Check validates dependencies and reports the result through tb.
func Check(tb testing.TB, configuration *config.Config, options cruiser.Options) {
	tb.Helper()

	result, err := cruiser.Validate(configuration, options)
	if err != nil {
		tb.Fatalf("godep-cruiser validation failed: %v", err)

		return
	}

	var report bytes.Buffer
	if err := cruiser.WriteReport(&report, cruiser.OutputTypeErr, result); err != nil {
		tb.Fatalf("godep-cruiser report failed: %v", err)

		return
	}

	if result.ErrorCount() > 0 {
		tb.Errorf("%s", report.String())

		return
	}
	if report.Len() > 0 {
		tb.Logf("%s", report.String())
	}
}
