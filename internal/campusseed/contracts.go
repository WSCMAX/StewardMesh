package campusseed

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/maxlemke/stewardmesh/internal/ledger"
)

func (s *Seeder) seedContracts(ctx context.Context) error {
	now := s.now()
	startsOn := time.Date(now.Year(), 7, 1, 0, 0, 0, 0, time.UTC)
	endsOn := time.Date(now.Year()+1, 6, 30, 0, 0, 0, 0, time.UTC)
	renewOn := time.Date(now.Year()+1, 5, 1, 0, 0, 0, 0, time.UTC)
	adobeEndsOn := now.Add(45 * 24 * time.Hour)

	definitions := []struct {
		id, name, vendorSlug string
		ceilingMinor         int64
		documentSlugs        []string
		endsOn               time.Time
	}{
		{
			id: "contract-m365-campus", name: "Microsoft 365 A3 Volume Agreement",
			vendorSlug: "microsoft", ceilingMinor: 316_800_000,
			documentSlugs: []string{"m365-contract-docx"}, endsOn: endsOn,
		},
		{
			id: "contract-adobe-campus", name: "Adobe Creative Cloud Campus Agreement",
			vendorSlug: "adobe", ceilingMinor: 64_620_000,
			documentSlugs: []string{"adobe-contract-html"}, endsOn: adobeEndsOn,
		},
		{
			id: "contract-autodesk-campus", name: "Autodesk Education Lab Agreement",
			vendorSlug: "autodesk", ceilingMinor: 42_500_000,
			documentSlugs: []string{"autodesk-quote-pdf"}, endsOn: endsOn,
		},
		{
			id: "contract-warranty-campus", name: "Campus Hardware Warranty Addendum",
			vendorSlug: "cdw", ceilingMinor: 18_500_000,
			documentSlugs: []string{"warranty-pdf"}, endsOn: endsOn,
		},
	}

	if s.contractIDs == nil {
		s.contractIDs = make(map[string]string)
	}
	for _, definition := range definitions {
		documents := make([]string, 0, len(definition.documentSlugs))
		for _, slug := range definition.documentSlugs {
			if blobID := s.blobIDs[slug]; blobID != "" {
				documents = append(documents, blobID)
			}
		}
		created, err := s.ledger.CreateContract(ctx, ledger.CreateContractInput{
			ID: definition.id, Name: definition.name,
			VendorID: s.vendorIDs[definition.vendorSlug],
			OperationalStatus: "active", FinancialStatus: "committed",
			Currency: "USD", CeilingMinor: definition.ceilingMinor,
			StartsOn: startsOn, EndsOn: definition.endsOn, RenewsOn: &renewOn,
			DocumentIDs: documents,
		})
		if err != nil && !errors.Is(err, ledger.ErrConflict) {
			return fmt.Errorf("create contract %q: %w", definition.name, err)
		}
		contractID := definition.id
		if err == nil {
			contractID = created.ID
		}
		s.contractIDs[definition.id] = contractID
	}
	return nil
}

func (s *Seeder) contractID(slug string) string {
	if s.contractIDs == nil {
		return slug
	}
	if id := s.contractIDs[slug]; id != "" {
		return id
	}
	return slug
}
