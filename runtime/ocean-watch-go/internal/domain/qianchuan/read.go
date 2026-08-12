package qianchuan

import "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"

type PageInfo struct {
	Page        int
	TotalPages  int
	TotalNumber int
}

type ProductImage struct {
	URL string `json:"img_url,omitempty"`
}

type Product struct {
	ProductID    string         `json:"product_id"`
	Name         string         `json:"name,omitempty"`
	Image        string         `json:"image,omitempty"`
	CategoryName string         `json:"category_name,omitempty"`
	ChannelID    string         `json:"channel_id,omitempty"`
	ChannelType  string         `json:"channel_type,omitempty"`
	SellNumber   *int64         `json:"sell_num,omitempty"`
	StockNumber  *int64         `json:"stock_num,omitempty"`
	AuditTime    string         `json:"audit_time,omitempty"`
	SquareImages []ProductImage `json:"square_image_list"`
	Tags         []string       `json:"tags"`
	GrayReasons  []string       `json:"gray_reasons"`
}

type ProductPage struct {
	Rows      []Product
	PageInfo  PageInfo
	RequestID string
}

type Creator struct {
	AwemeID   string `json:"aweme_id,omitempty"`
	VisibleID string `json:"aweme_show_id,omitempty"`
	Name      string `json:"aweme_name,omitempty"`
	Avatar    string `json:"aweme_avatar,omitempty"`
}

type AuthorizedCreator struct {
	AwemeID                  string   `json:"aweme_id,omitempty"`
	VisibleID                string   `json:"aweme_show_id,omitempty"`
	Name                     string   `json:"aweme_name,omitempty"`
	Avatar                   string   `json:"aweme_avatar,omitempty"`
	AuthTypes                []string `json:"auth_type"`
	HasAuthorized            *bool    `json:"has_authorized,omitempty"`
	ProductPromotionDisabled *bool    `json:"is_product_uni_prom_disabled,omitempty"`
	ProductDisableReasons    []string `json:"product_disable_reasons,omitempty"`
	ProductPromotionApply    string   `json:"product_uni_prom_apply_type,omitempty"`
	CanControlPromotion      *bool    `json:"can_control_uniprom,omitempty"`
	CanApplyPromotion        *bool    `json:"can_apply_uniprom,omitempty"`
	HasShopPermission        *bool    `json:"has_shop_permission,omitempty"`
	HasLivePermission        *bool    `json:"has_live_permission,omitempty"`
}

type AuthorizedCreatorPage struct {
	Rows      []AuthorizedCreator
	PageInfo  PageInfo
	RequestID string
}

type CreatorVideo struct {
	AwemeItemID       string   `json:"aweme_item_id,omitempty"`
	ImageMode         string   `json:"image_mode,omitempty"`
	VideoID           string   `json:"video_id,omitempty"`
	MaterialID        string   `json:"material_id,omitempty"`
	Title             string   `json:"title,omitempty"`
	VideoCoverURL     string   `json:"video_cover_url,omitempty"`
	URL               string   `json:"url,omitempty"`
	Width             *int32   `json:"width,omitempty"`
	Height            *int32   `json:"height,omitempty"`
	Duration          *int64   `json:"duration,omitempty"`
	IsRecommend       *int64   `json:"is_recommend,omitempty"`
	ViewCount         *int64   `json:"view_count,omitempty"`
	LikeCount         *int64   `json:"like_count,omitempty"`
	ShareCount        *int64   `json:"share_count,omitempty"`
	CommentCount      *int64   `json:"comment_count,omitempty"`
	IsAICreated       *bool    `json:"is_ai_create,omitempty"`
	MatchedProductIDs []string `json:"matched_product_ids,omitempty"`
}

type CreatorVideoPage struct {
	Rows       []CreatorVideo
	NextCursor *int64
	HasMore    bool
	RequestID  string
}

type PlanProduct struct {
	ProductID    string   `json:"product_id"`
	ProductName  string   `json:"product_name,omitempty"`
	ProductImage string   `json:"product_image,omitempty"`
	ChannelID    string   `json:"channel_id,omitempty"`
	ChannelType  string   `json:"channel_type,omitempty"`
	Reasons      []string `json:"recommend_reasons,omitempty"`
}

type CostGuarantee struct {
	Status           string `json:"status,omitempty"`
	CompensateStatus string `json:"compensate_status,omitempty"`
	Reason           string `json:"reason,omitempty"`
}

type Plan struct {
	AdID          string          `json:"ad_id"`
	Name          string          `json:"name,omitempty"`
	Status        string          `json:"status,omitempty"`
	OptStatus     string          `json:"opt_status,omitempty"`
	CreateTime    string          `json:"create_time,omitempty"`
	ModifyTime    string          `json:"modify_time,omitempty"`
	StartTime     string          `json:"start_time,omitempty"`
	EndTime       string          `json:"end_time,omitempty"`
	MarketingGoal string          `json:"marketing_goal,omitempty"`
	AdlabScene    string          `json:"adlab_scene,omitempty"`
	Creators      []Creator       `json:"creators"`
	Products      []PlanProduct   `json:"products"`
	Budget        *domain.Decimal `json:"budget,omitempty"`
	BudgetMode    string          `json:"budget_mode,omitempty"`
	SmartBidType  string          `json:"smart_bid_type,omitempty"`
	ROI2Goal      *domain.Decimal `json:"roi2_goal,omitempty"`
	Guarantee     *CostGuarantee  `json:"compensate_info,omitempty"`
}

type PlanPage struct {
	Rows      []Plan
	PageInfo  PageInfo
	RequestID string
}

type PlanDetail struct {
	AdID          string          `json:"ad_id"`
	Name          string          `json:"name,omitempty"`
	Status        string          `json:"status,omitempty"`
	OptStatus     string          `json:"opt_status,omitempty"`
	CreateTime    string          `json:"create_time,omitempty"`
	ModifyTime    string          `json:"modify_time,omitempty"`
	MarketingGoal string          `json:"marketing_goal,omitempty"`
	AwemeID       string          `json:"aweme_id,omitempty"`
	Creators      []Creator       `json:"creators"`
	Products      []PlanProduct   `json:"products"`
	Budget        *domain.Decimal `json:"budget,omitempty"`
	BudgetMode    string          `json:"budget_mode,omitempty"`
	SmartBidType  string          `json:"smart_bid_type,omitempty"`
	ROI2Goal      *domain.Decimal `json:"roi2_goal,omitempty"`
}

type PlanMaterial struct {
	MaterialID         string   `json:"material_id,omitempty"`
	AwemeItemID        string   `json:"aweme_item_id,omitempty"`
	VideoID            string   `json:"video_id,omitempty"`
	Title              string   `json:"title,omitempty"`
	URL                string   `json:"url,omitempty"`
	MaterialType       string   `json:"material_type,omitempty"`
	MaterialSelectType string   `json:"material_select_type,omitempty"`
	MaterialStatus     string   `json:"material_status,omitempty"`
	AuditStatus        string   `json:"audit_status,omitempty"`
	Source             string   `json:"source,omitempty"`
	Duration           *int64   `json:"video_duration,omitempty"`
	Deleted            *bool    `json:"is_delete,omitempty"`
	AwemeIDs           []string `json:"aweme_id_list"`
	ProductIDs         []string `json:"product_id_list"`
	DeliveryReasons    []string `json:"delivery_not_reason"`
}

type MaterialPage struct {
	Rows      []PlanMaterial
	PageInfo  PageInfo
	RequestID string
}
