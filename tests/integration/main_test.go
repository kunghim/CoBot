package integration

import (
	"flag"
	"fmt"
	"os"
	"testing"

	"github.com/kunghim/CoBot/tests/contract"
)

// TestMain compares the API traffic of a full run with api/openapi.yaml. A run
// narrowed with -run or -skip cannot tell what stopped being covered, so it
// skips the comparison.
func TestMain(m *testing.M) {
	flag.Parse()
	code := m.Run()
	narrowed := flag.Lookup("test.run").Value.String() != "" || flag.Lookup("test.skip").Value.String() != ""
	if code != 0 || narrowed {
		os.Exit(code)
	}
	if os.Getenv("COBOT_CONTRACT_UPDATE") == "1" {
		if err := contract.Update(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(code)
	}
	for _, problem := range contract.Check() {
		fmt.Fprintln(os.Stderr, problem)
		code = 1
	}
	os.Exit(code)
}
