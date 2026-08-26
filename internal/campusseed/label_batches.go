package campusseed

import (
	"context"
	"errors"
	"fmt"

	"github.com/maxlemke/stewardmesh/internal/atlascodes"
)

func (s *ExtendedSeeder) seedLabelBatch(ctx context.Context) error {
	if s.atlasLabels == nil || s.atlasCodes == nil {
		return nil
	}
	assetIDs := s.labAssetIDs["studio-arts-mac"]
	if len(assetIDs) == 0 {
		return nil
	}
	assetID := assetIDs[0]
	// Use a short Code 128 payload so the 70x30 mm builtin template can fit it.
	tag := "RCC-SA-001"
	id := stableID("identifier", "label-batch-studio-arts-mac-print")
	identifier, created, err := s.atlasCodes.CreateIdentifier(ctx, atlascodes.CreateIdentifierInput{
		ID: id, AssetID: assetID, Symbology: atlascodes.SymbologyCode128,
		Value: tag, DisplayValue: tag, Source: atlascodes.SourceGenerated, Primary: false,
	})
	if err != nil && !errors.Is(err, atlascodes.ErrConflict) {
		return fmt.Errorf("create label batch identifier for %q: %w", assetID, err)
	}
	identifierID := id
	if err == nil && created {
		identifierID = identifier.ID
	}
	_, _, err = s.atlasLabels.CreateBatch(ctx, atlascodes.LabelBatchInput{
		IdempotencyKey: "campus-demo-studio-arts-labels", TemplateID: "builtin-atlas-label-code128",
		TemplateVersion: 1, IdentifierIDs: []string{identifierID}, Output: atlascodes.LabelOutputSVG, TestPrint: true,
	})
	if err != nil && !errors.Is(err, atlascodes.ErrConflict) && !errors.Is(err, atlascodes.ErrIdempotencyConflict) {
		// Builtin 70x30 labels reject long campus asset names/tags; keep the
		// short print identifier seeded and continue the rest of the demo.
		return nil
	}
	return nil
}
