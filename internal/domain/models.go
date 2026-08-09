package domain

import "time"

type Asset struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Kind         string     `json:"kind"`
	SerialNumber string     `json:"serialNumber,omitempty"`
	DepartmentID string     `json:"departmentId,omitempty"`
	UserID       string     `json:"userId,omitempty"`
	Status       string     `json:"status"`
	PurchaseDate *time.Time `json:"purchaseDate,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

type Tag struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	ParentID         string `json:"parentId,omitempty"`
	InheritByDefault bool   `json:"inheritByDefault"`
}

type Goal struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}
