package client

type SubUserAccess struct {
	Id             string   `json:"id"`
	Username       string   `json:"username"`
	Email          string   `json:"email"`
	Disabled       bool     `json:"disabled"`
	PermissionType string   `json:"permission_type"`
	Scopes         []string `json:"scopes"`
}

type PaginationData struct {
	Limit          string `json:"limit"`
	AfterSubuserId string `json:"after_subuser_id"`
	Username       string `json:"username"`
}
