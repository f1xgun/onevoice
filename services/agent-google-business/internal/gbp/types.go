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

// Review represents a Google Business Profile review.
type Review struct {
	Name        string       `json:"name"` // e.g., "accounts/123/locations/456/reviews/789"
	ReviewID    string       `json:"reviewId"`
	Reviewer    Reviewer     `json:"reviewer"`
	StarRating  string       `json:"starRating"` // ONE, TWO, THREE, FOUR, FIVE
	Comment     string       `json:"comment"`
	CreateTime  string       `json:"createTime"`
	UpdateTime  string       `json:"updateTime"`
	ReviewReply *ReviewReply `json:"reviewReply,omitempty"`
}

// Reviewer contains info about the review author.
type Reviewer struct {
	DisplayName     string `json:"displayName"`
	ProfilePhotoURL string `json:"profilePhotoUrl,omitempty"`
}

// ReviewReply is the business owner's reply to a review.
type ReviewReply struct {
	Comment    string `json:"comment"`
	UpdateTime string `json:"updateTime"`
}

// ListReviewsResponse is the response from the Reviews API.
type ListReviewsResponse struct {
	Reviews          []Review `json:"reviews"`
	AverageRating    float64  `json:"averageRating"`
	TotalReviewCount int      `json:"totalReviewCount"`
	NextPageToken    string   `json:"nextPageToken,omitempty"`
}

// ErrorResponse is a Google API error.
type ErrorResponse struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}
