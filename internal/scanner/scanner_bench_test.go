package scanner

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkScan(b *testing.B) {
	resolver, err := NewResolver("example.com/benchmark")
	if err != nil {
		b.Fatalf("NewResolver() error = %v", err)
	}

	for _, fileCount := range []int{1_000, 10_000} {
		b.Run(fmt.Sprintf("files=%d", fileCount), func(b *testing.B) {
			root := createScannerBenchmarkTree(b, fileCount)

			b.ReportAllocs()

			var files []File
			for b.Loop() {
				var scanErr error
				files, scanErr = Scan(root, resolver)
				if scanErr != nil {
					b.Fatalf("Scan() error = %v", scanErr)
				}
			}

			if len(files) != fileCount {
				b.Fatalf("Scan() returned %d files, want %d", len(files), fileCount)
			}
		})
	}
}

func createScannerBenchmarkTree(b *testing.B, fileCount int) string {
	b.Helper()

	root := b.TempDir()
	writeScannerBenchmarkFile(
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
		writeScannerBenchmarkFile(
			b,
			filepath.Join(directory, fmt.Sprintf("file%05d.go", index)),
			source,
		)
	}

	return root
}

func writeScannerBenchmarkFile(b *testing.B, filename string, contents []byte) {
	b.Helper()

	if err := os.WriteFile(filename, contents, 0o600); err != nil {
		b.Fatalf("os.WriteFile(%q) error = %v", filename, err)
	}
}
