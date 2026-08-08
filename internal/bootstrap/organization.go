package bootstrap

import "errors"

type Organization struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func NewOrganization(id, name string) (Organization, error) {
	if id == "" || name == "" {
		return Organization{}, errors.New("organization id and name are required")
	}
	return Organization{ID: id, Name: name}, nil
}
