package marketing

type DiscoveryEnvelope struct {
	Code      int64
	Message   string
	RequestID string
	Response  map[string]any
	PageInfo  *PageInfo
}

type AdminNode struct {
	Name     string
	Code     string
	Children []AdminNode
}

type AdminEnvelope struct {
	Code      int64
	Message   string
	RequestID string
	Response  map[string]any
	Nodes     []AdminNode
}
