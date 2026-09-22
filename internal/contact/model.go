package contact

import "time"

type Contact struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Relation    string    `json:"relation"`
	Phone       string    `json:"phone"`
	CountryCode string    `json:"countryCode"`
	CreatedAt   time.Time `json:"createdAt"`
}

type CreateContactRequest struct {
	Name        string `json:"name"`
	Relation    string `json:"relation"`
	Phone       string `json:"phone"`
	CountryCode string `json:"countryCode"`
}
