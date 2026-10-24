// Command gen writes the model catalog that ships with the program from the
// dataset at models.dev. Run it from the repository root when the shipped
// snapshot should move forward:
//
//	go run ./internal/modelcatalog/gen
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/kunghim/CoBot/internal/modelcatalog"
)

func main() {
	resp, err := (&http.Client{Timeout: 2 * time.Minute}).Get(modelcatalog.SourceURL)
	if err != nil {
		fail(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fail(fmt.Errorf("%s answered %d", modelcatalog.SourceURL, resp.StatusCode))
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		fail(err)
	}
	catalog, err := modelcatalog.Convert(raw, time.Now())
	if err != nil {
		fail(err)
	}
	encoded, err := catalog.Encode()
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile("internal/modelcatalog/bundled.json", encoded, 0o644); err != nil {
		fail(err)
	}
	fmt.Printf("wrote internal/modelcatalog/bundled.json: %d models, %d bytes\n", len(catalog.Models), len(encoded))
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gen:", err)
	os.Exit(1)
}
