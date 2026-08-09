package directoryexpansion

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

type MemoryImporter struct{ Connectors map[Provider]Connector }

func (m MemoryImporter) Import(ctx context.Context, req ImportRequest) (ImportResult, error) {
	batch := make([]byte, 16)
	_, _ = rand.Read(batch)
	r := ImportResult{Provider: req.Provider, BatchID: hex.EncodeToString(batch), Applied: !req.DryRun}
	connector := m.Connectors[req.Provider]
	if connector == nil {
		r.Errors = []string{"provider connector is not configured"}
		return r, nil
	}
	records, err := connector.Pull(ctx)
	if err != nil {
		return r, err
	}
	if req.DryRun {
		r.Unchanged = len(records)
		return r, nil
	}
	r.Created = len(records)
	return r, nil
}
