package client

type CommonResponse[T any] struct {
	Result T `json:"result"`
}

type Teammate struct {
	Username     string `json:"username"`
	Email        string `json:"email"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	Address      string `json:"address"`
	Address2     string `json:"address2"`
	City         string `json:"city"`
	State        string `json:"state"`
	Zip          string `json:"zip"`
	Country      string `json:"country"`
	Company      string `json:"company"`
	Website      string `json:"website"`
	Phone        string `json:"phone"`
	IsAdmin      bool   `json:"is_admin"`
	IsSso        bool   `json:"is_sso"`
	UserType     string `json:"user_type"`
	IsUnified    bool   `json:"is_unified"`
	IsPartnerSso bool   `json:"is_partner_sso"`
}

type TeammateScope struct {
	Teammate
	Scopes []string `json:"scopes"`
}

type PaginationData struct {
	Limit          string `json:"limit"`
	AfterSubuserId string `json:"after_subuser_id"`
	Username       string `json:"username"`
}

type PendingUserAccess struct {
	Id             int    `json:"id"`
	ScopeGroupName string `json:"scope_group_name"`
	Username       string `json:"username"`
	Email          string `json:"email"`
	FirstName      string `json:"first_name"`
	LastName       string `json:"last_name"`
}
