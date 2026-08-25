package campusseed

import (
	"context"
	"errors"
	"fmt"

	"github.com/maxlemke/stewardmesh/internal/atlascodes"
)

func (s *Seeder) seedIdentifiers(ctx context.Context) error {
	if s.atlasCodes == nil {
		return nil
	}
	if s.identifierIDs == nil {
		s.identifierIDs = make(map[string]string)
	}
	for _, lab := range campusLabs {
		assetIDs := s.labAssetsBySlug[lab.Slug]
		if len(assetIDs) == 0 {
			continue
		}
		assetID := assetIDs[0]
		stationNumber := 1
		tag := labStationAssetTag(lab.Slug, stationNumber)
		id := stableID("identifier", "lab-"+lab.Slug+"-station-01")
		identifier, created, err := s.atlasCodes.CreateIdentifier(ctx, atlascodes.CreateIdentifierInput{
			ID: id, AssetID: assetID, Symbology: atlascodes.SymbologyCode128,
			Value: tag, DisplayValue: tag, Source: atlascodes.SourceGenerated, Primary: true,
		})
		if err != nil && !errors.Is(err, atlascodes.ErrConflict) {
			return fmt.Errorf("create lab identifier for %q: %w", lab.Name, err)
		}
		if err == nil && created {
			s.identifierIDs[lab.Slug] = identifier.ID
		}
		if err != nil && errors.Is(err, atlascodes.ErrConflict) {
			s.identifierIDs[lab.Slug] = id
		}
	}
	if len(s.tabletAssetIDs) > 0 {
		tabletID := s.tabletAssetIDs[0]
		qrID := stableID("identifier", "custodial-ipad-qr")
		tag := "RCC-IPD-0001"
		identifier, _, err := s.atlasCodes.CreateIdentifier(ctx, atlascodes.CreateIdentifierInput{
			ID: qrID, AssetID: tabletID, Symbology: atlascodes.SymbologyQR,
			Value: tag, DisplayValue: "Custodial iPad", Source: atlascodes.SourceGenerated, Primary: true,
		})
		if err != nil && !errors.Is(err, atlascodes.ErrConflict) {
			return fmt.Errorf("create custodial tablet identifier: %w", err)
		}
		if err == nil {
			s.identifierIDs["custodial-ipad"] = identifier.ID
		} else {
			s.identifierIDs["custodial-ipad"] = qrID
		}
	}
	return nil
}
