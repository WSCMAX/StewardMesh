package domain

import "time"

// Organization is the ownership boundary for every StewardMesh record.
// Requirement: REQ-FOUNDATION-001.
type Organization struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
