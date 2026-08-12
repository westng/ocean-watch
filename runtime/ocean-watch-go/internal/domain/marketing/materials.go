package marketing

type PageInfo struct {
	Page        int `json:"page"`
	PageSize    int `json:"page_size"`
	TotalNumber int `json:"total_number"`
	TotalPages  int `json:"total_page"`
}

type VideoAsset struct {
	ID               string   `json:"id,omitempty"`
	MaterialID       string   `json:"material_id,omitempty"`
	Filename         string   `json:"filename,omitempty"`
	CreateTime       string   `json:"create_time,omitempty"`
	Width            *int64   `json:"width,omitempty"`
	Height           *int64   `json:"height,omitempty"`
	Duration         *float64 `json:"duration,omitempty"`
	BitRate          *float64 `json:"bit_rate,omitempty"`
	Format           string   `json:"format,omitempty"`
	Source           string   `json:"source,omitempty"`
	Signature        string   `json:"signature,omitempty"`
	PosterURL        string   `json:"poster_url,omitempty"`
	URL              string   `json:"url,omitempty"`
	Size             *int64   `json:"size,omitempty"`
	Labels           []string `json:"labels,omitempty"`
	OrganizationTags []string `json:"organization_tags,omitempty"`
	StarAuthorID     string   `json:"star_author_id,omitempty"`
}

type SelectedVideo struct {
	VideoID    string   `json:"video_id,omitempty"`
	MaterialID string   `json:"material_id,omitempty"`
	Filename   string   `json:"filename,omitempty"`
	CreateTime string   `json:"create_time,omitempty"`
	Width      *int64   `json:"width,omitempty"`
	Height     *int64   `json:"height,omitempty"`
	Duration   *float64 `json:"duration,omitempty"`
	Format     string   `json:"format,omitempty"`
	Source     string   `json:"source,omitempty"`
	Signature  string   `json:"signature,omitempty"`
	PosterURL  string   `json:"poster_url,omitempty"`
}

type VideoPage struct {
	Rows      []VideoAsset
	PageInfo  PageInfo
	RequestID string
	Message   string
}

type VideoBatch struct {
	Rows      []VideoAsset
	RequestID string
	Message   string
}

type CoverAsset struct {
	ID     string `json:"id,omitempty"`
	URL    string `json:"url,omitempty"`
	Width  *int64 `json:"width,omitempty"`
	Height *int64 `json:"height,omitempty"`
}

type CoverSuggestion struct {
	Status    string
	Rows      []CoverAsset
	RequestID string
	Message   string
}

type ImageAsset struct {
	ID         string `json:"id,omitempty"`
	MaterialID string `json:"material_id,omitempty"`
	Filename   string `json:"filename,omitempty"`
	CreateTime string `json:"create_time,omitempty"`
	Width      *int64 `json:"width,omitempty"`
	Height     *int64 `json:"height,omitempty"`
	Format     string `json:"format,omitempty"`
	Signature  string `json:"signature,omitempty"`
	URL        string `json:"url,omitempty"`
	Size       *int64 `json:"size,omitempty"`
	AIGC       *bool  `json:"aigc,omitempty"`
}

type ImagePage struct {
	Rows      []ImageAsset
	PageInfo  PageInfo
	RequestID string
	Message   string
}

type ImageBatch struct {
	Rows      []ImageAsset
	RequestID string
	Message   string
}

type CreatorVideo struct {
	ItemID        string   `json:"item_id,omitempty"`
	MaterialID    string   `json:"material_id,omitempty"`
	VideoID       string   `json:"video_id,omitempty"`
	VideoCoverID  string   `json:"video_cover_id,omitempty"`
	VideoCoverURL string   `json:"video_cover_url,omitempty"`
	ImageMode     string   `json:"image_mode,omitempty"`
	Title         string   `json:"title,omitempty"`
	Duration      *float64 `json:"duration,omitempty"`
	PlayURL       string   `json:"aweme_play_url,omitempty"`
}

type CreatorAuthorization struct {
	AwemeID            string       `json:"aweme_id,omitempty"`
	AwemeName          string       `json:"aweme_name,omitempty"`
	OpenID             string       `json:"open_id,omitempty"`
	AuthType           string       `json:"auth_type,omitempty"`
	AuthStatus         string       `json:"auth_status,omitempty"`
	StartTime          string       `json:"start_time,omitempty"`
	EndTime            string       `json:"end_time,omitempty"`
	AutoExpireDate     string       `json:"auth_auto_expire_date,omitempty"`
	WarningTypes       []string     `json:"warning_types,omitempty"`
	WarningContent     []string     `json:"warning_content,omitempty"`
	HomepageVisibility *bool        `json:"has_video_hp_visibility_limit,omitempty"`
	Video              CreatorVideo `json:"video_info"`
}

type CreatorAuthorizationPage struct {
	Rows      []CreatorAuthorization
	PageInfo  PageInfo
	RequestID string
	Message   string
}

type CreatorHomepagePage struct {
	Rows      []CreatorVideo
	PageInfo  PageInfo
	RequestID string
	Message   string
}

type MaterialSourceKey struct {
	IDType    string `json:"id_type,omitempty"`
	IDValue   string `json:"id_value,omitempty"`
	Canonical string `json:"canonical,omitempty"`
}

type CreatorCandidate struct {
	Channel                string             `json:"channel"`
	OwnerAdvertiserID      string             `json:"owner_advertiser_id"`
	SourceType             string             `json:"source_type"`
	SourceKey              *MaterialSourceKey `json:"source_key,omitempty"`
	MaterialID             string             `json:"material_id,omitempty"`
	VideoID                string             `json:"video_id,omitempty"`
	ItemID                 string             `json:"item_id,omitempty"`
	ImageMode              string             `json:"image_mode,omitempty"`
	VideoCoverID           string             `json:"video_cover_id,omitempty"`
	VideoCoverURL          string             `json:"video_cover_url,omitempty"`
	Title                  string             `json:"title,omitempty"`
	Duration               *float64           `json:"duration,omitempty"`
	CreatorID              string             `json:"creator_id,omitempty"`
	CreatorName            string             `json:"creator_name,omitempty"`
	AuthorizationSubjectID string             `json:"authorization_subject_id,omitempty"`
	AuthorizationType      string             `json:"authorization_type,omitempty"`
	AuthorizationStatus    string             `json:"authorization_status,omitempty"`
	RawAuthorizationStatus string             `json:"raw_authorization_status,omitempty"`
	AuthorizationStartAt   string             `json:"authorization_start_at,omitempty"`
	AuthorizationExpiresAt string             `json:"authorization_expires_at,omitempty"`
	WarningTypes           []string           `json:"warning_types,omitempty"`
	Usable                 bool               `json:"usable"`
	UnusableReasons        []string           `json:"unusable_reasons"`
}

type ProductImage struct {
	URL string `json:"url,omitempty"`
}

type ProductCategory struct {
	FirstID    string `json:"first_category_id,omitempty"`
	FirstName  string `json:"first_category_name,omitempty"`
	SecondID   string `json:"second_category_id,omitempty"`
	SecondName string `json:"second_category_name,omitempty"`
	ThirdID    string `json:"third_category_id,omitempty"`
	ThirdName  string `json:"third_category_name,omitempty"`
	FourthID   string `json:"fourth_category_id,omitempty"`
	FourthName string `json:"fourth_category_name,omitempty"`
}

type DPAProduct struct {
	ProductID        string           `json:"product_id,omitempty"`
	OuterID          string           `json:"outer_id,omitempty"`
	Name             string           `json:"name,omitempty"`
	Title            string           `json:"title,omitempty"`
	Description      string           `json:"description,omitempty"`
	Feature          string           `json:"feature,omitempty"`
	ImageURL         string           `json:"image_url,omitempty"`
	ImagesURL        []ProductImage   `json:"images_url,omitempty"`
	VideoURL         string           `json:"video_url,omitempty"`
	Status           string           `json:"status,omitempty"`
	AuditStatus      string           `json:"audit_status,omitempty"`
	CompletionStatus string           `json:"completion_status,omitempty"`
	OnlineTime       string           `json:"online_time,omitempty"`
	OfflineTime      string           `json:"offline_time,omitempty"`
	Bought           *int64           `json:"bought,omitempty"`
	Comments         *int64           `json:"comments,omitempty"`
	HasVideo         *bool            `json:"has_video,omitempty"`
	Tags             []string         `json:"tags,omitempty"`
	Category         *ProductCategory `json:"category,omitempty"`
}

type ProductPage struct {
	Rows      []DPAProduct
	PageInfo  PageInfo
	RequestID string
	Message   string
}
