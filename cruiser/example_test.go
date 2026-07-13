package cruiser_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/butaosuinu/godep-cruiser/config"
	"github.com/butaosuinu/godep-cruiser/cruiser"
)

func ExampleValidate() {
	root, err := os.MkdirTemp("", "godep-cruiser-example-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(root)

	writeExampleFile(filepath.Join(root, "go.mod"), "module example.com/project\n\ngo 1.25.0\n")
	writeExampleFile(filepath.Join(root, "app.go"), "package project\n\nimport _ \"os\"\n")

	configuration, err := config.Load(strings.NewReader(`{
  "forbidden": [{
    "name": "no-os",
    "comment": "use an injected file system",
    "severity": "error",
    "from": {"path": ["^app\\.go$"]},
    "to": {"path": ["^os$"], "dependencyTypes": ["stdlib"]}
  }]
}`))
	if err != nil {
		panic(err)
	}

	result, err := cruiser.Validate(configuration, cruiser.Options{ScanRoot: root})
	if err != nil {
		panic(err)
	}
	var report bytes.Buffer
	if err := cruiser.WriteReport(&report, cruiser.OutputTypeErr, result); err != nil {
		panic(err)
	}

	fmt.Printf("errors: %d\n%s", result.ErrorCount(), report.String())
	// Output:
	// errors: 1
	// [error] rule "no-os": app.go:3 -> os (stdlib): forbidden dependency
	//   fix: use an injected file system
}

func writeExampleFile(filename, contents string) {
	if err := os.WriteFile(filename, []byte(contents), 0o600); err != nil {
		panic(err)
	}
}
