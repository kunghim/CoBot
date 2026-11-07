package codex

import (
	"context"

	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/platform"
)

// Installation is the Codex of this machine as the engine interface sees it.
type Installation struct {
	Paths    platform.Paths
	Catalogs *CatalogBuilder
}

var (
	_ engine.Installation = Installation{}
	_ engine.Control      = (*Server)(nil)
)

func (Installation) Locate(configuredPath string) (string, error) { return Locate(configuredPath) }

func (Installation) ProbeVersion(ctx context.Context, binary string) (string, error) {
	return ProbeVersion(ctx, binary)
}

func (Installation) Probe(ctx context.Context, binary, version string) engine.Status {
	return Probe(ctx, binary, version)
}

func (i Installation) BuildCatalog(ctx context.Context, binary string, models []engine.ClientModel) (engine.Catalog, error) {
	if i.Catalogs == nil {
		return engine.Catalog{}, nil
	}
	catalog, _, err := i.Catalogs.Build(ctx, binary, EngineEnv(i.Paths, nil), models)
	return engine.Catalog(catalog), err
}
