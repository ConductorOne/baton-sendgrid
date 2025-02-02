package models

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

type Subuser struct {
	Disabled bool   `json:"disabled"`
	Email    string `json:"email"`
	Id       int    `json:"id"`
	Username string `json:"username"`
}

type SubuserCreate struct {
	Username      string   `json:"username"`
	Email         string   `json:"email"`
	Password      string   `json:"password"`
	Ips           []string `json:"ips"`
	Region        string   `json:"region"`
	IncludeRegion bool     `json:"include_region"`
}

type TeammateSubuser struct {
	Id             int      `json:"id"`
	Username       string   `json:"username"`
	Email          string   `json:"email"`
	Disabled       bool     `json:"disabled"`
	PermissionType string   `json:"permission_type"`
	Scopes         []string `json:"scopes"`
}

type NextParams struct {
	Limit          int    `json:"limit"`
	AfterSubuserId int    `json:"after_subuser_id"`
	Username       string `json:"username"`
}

type TeammateSubuserResponse struct {
	HasRestrictedSubuserAccess bool              `json:"has_restricted_subuser_access"`
	SubuserAccess              []TeammateSubuser `json:"subuser_access"`
	Metadata                   struct {
		NextParams NextParams `json:"next_params,omitempty"`
	} `json:"_metadata"`
}
