package gbp

// Account represents a Google Business Profile account.
type Account struct {
	Name        string `json:"name"`        // e.g., "accounts/123456"
	AccountName string `json:"accountName"` // display name
	Type        string `json:"type"`        // PERSONAL, LOCATION_GROUP, etc.
}

// ListAccountsResponse is the response from Account Management API.
type ListAccountsResponse struct {
	Accounts []Account `json:"accounts"`
}

// Location represents a Google Business Profile location.
type Location struct {
	Name              string         `json:"name"` // e.g., "locations/789"
	Title             string         `json:"title"`
	StorefrontAddress *PostalAddress `json:"storefrontAddress,omitempty"`
}

// PostalAddress is a simplified postal address.
type PostalAddress struct {
	AddressLines []string `json:"addressLines,omitempty"`
	Locality     string   `json:"locality,omitempty"`
	RegionCode   string   `json:"regionCode,omitempty"`
}

// ListLocationsResponse is the response from Business Information API.
type ListLocationsResponse struct {
	Locations []Location `json:"locations"`
}

// ErrorResponse is a Google API error.
type ErrorResponse struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}
