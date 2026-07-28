package domain

type AuthorizedAccount struct {
	AccountID       string
	AccountStringID string
	ShopID          string
	AccountName     string
	AccountRole     string
	AccountType     string
	AdvertiserName  string
	IsValid         *bool
	AdvertiserIDs   []string
}

type AccountDiscoveryIssue struct {
	AccountID string
	Role      string
	Code      string
	Reason    string
}

type AdvertiserSnapshot struct {
	Accounts        []AuthorizedAccount
	AdvertiserIDs   []string
	DiscoveryIssues []AccountDiscoveryIssue
}
