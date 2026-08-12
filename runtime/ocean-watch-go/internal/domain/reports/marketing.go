package reports

type MarketingField struct {
	Field       string   `json:"field,omitempty"`
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	SortAble    *bool    `json:"sort_able,omitempty"`
	FilterAble  *bool    `json:"filter_able,omitempty"`
	Exclusions  []string `json:"exclusions,omitempty"`
}

type MarketingTopic struct {
	DataTopic  string           `json:"data_topic"`
	Dimensions []MarketingField `json:"dimensions"`
	Metrics    []MarketingField `json:"metrics"`
}

type MarketingSchema struct {
	Topics    []MarketingTopic
	RequestID string
	Message   string
	Response  map[string]any
}

type MarketingReportRow struct {
	Dimensions map[string]string `json:"dimensions"`
	Metrics    map[string]string `json:"metrics"`
}

type MarketingReportPage struct {
	Rows         []MarketingReportRow
	TotalMetrics map[string]string
	Page         int
	PageSize     int
	TotalPages   int
	TotalNumber  int
	RequestID    string
	Message      string
}

type MarketingVideoMaterial struct {
	MaterialID         string
	VideoID            string
	VideoCoverID       string
	MaterialStatus     string
	MaterialOptStatus  string
	ImageMode          string
	MaterialCreateTime string
}

type MarketingPromotion struct {
	ProjectID             string
	PromotionID           string
	PromotionName         string
	PromotionStatus       string
	PromotionStatusFirst  string
	PromotionStatusSecond []string
	PromotionOptStatus    string
	Materials             []MarketingVideoMaterial
}

type MarketingPromotionPage struct {
	Rows        []MarketingPromotion
	Page        int
	PageSize    int
	TotalPages  int
	TotalNumber int
	RequestID   string
	Message     string
}
