# Full-Text Search

Nimbus provides a unified full-text search abstraction supporting multiple backends.

## Supported Drivers
| Driver | Package | Description |
|--------|---------|-------------|
| `meilisearch` | Built-in | Meilisearch search engine |
| `typesense` | Built-in | Typesense search engine |
| `postgresql` | Built-in | PostgreSQL `tsvector` full-text search |

## Configuration
```go
search.Config{
    Driver: "meilisearch",
    Meilisearch: search.MeilisearchConfig{
        Host:   "http://localhost:7700",
        APIKey: "masterKey",
    },
}
```

## Indexing
```go
// Index a document
search.Index("products", product.ID, map[string]any{
    "name":        product.Name,
    "description": product.Description,
    "price":       product.Price,
})

// Remove from index
search.Delete("products", product.ID)
```

## Searching
```go
results, err := search.Search("products", "wireless headphones", search.Options{
    Limit:  20,
    Offset: 0,
    Filter: "price < 100",
    Sort:   []string{"price:asc"},
})
```

## Model Integration
```go
type Product struct {
    database.Model
    Name        string
    Description string
}

func (p *Product) SearchableAs() string { return "products" }
func (p *Product) ToSearchableMap() map[string]any {
    return map[string]any{"name": p.Name, "description": p.Description}
}
```
