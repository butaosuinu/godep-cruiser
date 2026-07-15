package cruiser_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/butaosuinu/godep-cruiser/config"
	"github.com/butaosuinu/godep-cruiser/cruiser"
)

func BenchmarkValidate(b *testing.B) {
	configuration := &config.Config{Forbidden: []config.ForbiddenRule{{
		Name:     "no-third-party-dependencies",
		Severity: config.SeverityError,
		From:     config.From{Path: []string{`^pkg`}},
		To: config.To{
			DependencyTypes: []config.DependencyType{config.DependencyTypeModule},
		},
	}}}

	for _, fileCount := range []int{1_000, 10_000} {
		b.Run(fmt.Sprintf("files=%d", fileCount), func(b *testing.B) {
			root := createValidateBenchmarkTree(b, fileCount)
			options := cruiser.Options{ScanRoot: root}

			b.ReportAllocs()

			var result cruiser.Result
			for b.Loop() {
				var validateErr error
				result, validateErr = cruiser.Validate(configuration, options)
				if validateErr != nil {
					b.Fatalf("cruiser.Validate() error = %v", validateErr)
				}
			}

			if len(result.Violations) != 0 || len(result.Known) != 0 || len(result.Stale) != 0 {
				b.Fatalf("cruiser.Validate() result = %#v, want no findings", result)
			}
		})
	}
}

func createValidateBenchmarkTree(b *testing.B, fileCount int) string {
	b.Helper()

	root := b.TempDir()
	writeValidateBenchmarkFile(
		b,
		filepath.Join(root, "go.mod"),
		[]byte("module example.com/benchmark\n\ngo 1.25.8\n"),
	)

	const filesPerDirectory = 100
	source := []byte(`package fixture

import (
	_ "example.com/benchmark/shared"
	_ "fmt"
)
`)
	for index := range fileCount {
		directory := filepath.Join(root, fmt.Sprintf("pkg%03d", index/filesPerDirectory))
		if index%filesPerDirectory == 0 {
			if err := os.Mkdir(directory, 0o700); err != nil {
				b.Fatalf("os.Mkdir(%q) error = %v", directory, err)
			}
		}
		writeValidateBenchmarkFile(
			b,
			filepath.Join(directory, fmt.Sprintf("file%05d.go", index)),
			source,
		)
	}

	return root
}

func writeValidateBenchmarkFile(b *testing.B, filename string, contents []byte) {
	b.Helper()

	if err := os.WriteFile(filename, contents, 0o600); err != nil {
		b.Fatalf("os.WriteFile(%q) error = %v", filename, err)
	}
}
