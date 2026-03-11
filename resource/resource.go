package resource

// Resource transforms a model to API response format (Laravel API Resources).
// Implement ToJSON to control which fields are exposed.
type Resource interface {
	ToJSON() map[string]any
}

// ResourceFunc adapts a function to Resource.
type ResourceFunc func() map[string]any

func (f ResourceFunc) ToJSON() map[string]any {
	return f()
}

// Collection maps a slice of resources to JSON.
func Collection(items []Resource) []map[string]any {
	out := make([]map[string]any, len(items))
	for i, r := range items {
		out[i] = r.ToJSON()
	}
	return out
}
