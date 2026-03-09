package database

import (
	"fmt"
	"strconv"

	"gorm.io/gorm"
)

// Paginator holds paginated results and metadata (Lucid-style).
type Paginator struct {
	Data       any   `json:"data"`
	Total      int64 `json:"total"`
	PerPage    int   `json:"per_page"`
	CurrentPage int  `json:"current_page"`
	LastPage   int   `json:"last_page"`
	FirstPage  int   `json:"first_page"`
	// URLs for building pagination links
	FirstPageURL string `json:"first_page_url"`
	LastPageURL  string `json:"last_page_url"`
	NextPageURL  string `json:"next_page_url"`
	PrevPageURL  string `json:"previous_page_url"`
	baseURL     string
}

// Meta returns pagination metadata.
func (p *Paginator) Meta() map[string]any {
	return map[string]any{
		"total":         p.Total,
		"perPage":       p.PerPage,
		"currentPage":   p.CurrentPage,
		"lastPage":      p.LastPage,
		"firstPage":     p.FirstPage,
		"firstPageUrl":  p.FirstPageURL,
		"lastPageUrl":   p.LastPageURL,
		"nextPageUrl":   p.NextPageURL,
		"previousPageUrl": p.PrevPageURL,
	}
}

// BaseUrl sets the base URL for pagination links (e.g. "/posts").
func (p *Paginator) BaseUrl(url string) *Paginator {
	p.baseURL = url
	p.FirstPageURL = p.pageURL(1)
	p.LastPageURL = p.pageURL(p.LastPage)
	if p.CurrentPage < p.LastPage {
		p.NextPageURL = p.pageURL(p.CurrentPage + 1)
	}
	if p.CurrentPage > 1 {
		p.PrevPageURL = p.pageURL(p.CurrentPage - 1)
	}
	return p
}

func (p *Paginator) pageURL(page int) string {
	if p.baseURL == "" {
		return ""
	}
	sep := "?"
	if len(p.baseURL) > 0 && p.baseURL[len(p.baseURL)-1] == '?' {
		sep = ""
	}
	return p.baseURL + sep + "page=" + strconv.Itoa(page)
}

// Paginate runs the query with limit/offset and returns a Paginator.
func Paginate(db *gorm.DB, dest any, page, perPage int) (*Paginator, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}

	var total int64
	countDB := db.Session(&gorm.Session{})
	if err := countDB.Count(&total).Error; err != nil {
		return nil, err
	}

	offset := (page - 1) * perPage
	if err := db.Offset(offset).Limit(perPage).Find(dest).Error; err != nil {
		return nil, err
	}

	lastPage := int(total) / perPage
	if int(total)%perPage > 0 {
		lastPage++
	}
	if lastPage < 1 {
		lastPage = 1
	}

	p := &Paginator{
		Data:        dest,
		Total:       total,
		PerPage:     perPage,
		CurrentPage: page,
		LastPage:    lastPage,
		FirstPage:   1,
	}
	p.BaseUrl("")
	return p, nil
}

// GetUrlsForRange returns anchors for building pagination UI.
func (p *Paginator) GetUrlsForRange(start, end int) []struct {
	Page     int
	URL      string
	IsActive bool
} {
	if p.baseURL == "" {
		p.BaseUrl("")
	}
	var result []struct {
		Page     int
		URL      string
		IsActive bool
	}
	for i := start; i <= end && i <= p.LastPage; i++ {
		result = append(result, struct {
			Page     int
			URL      string
			IsActive bool
		}{
			Page:     i,
			URL:      p.pageURL(i),
			IsActive: i == p.CurrentPage,
		})
	}
	return result
}

// PaginateQuery is a convenience that paginates a model query.
// Example: PaginateQuery(Post.Query().Where("status","published"), &posts, page, 20)
func PaginateQuery(q *gorm.DB, dest any, page, perPage int) (*Paginator, error) {
	return Paginate(q, dest, page, perPage)
}

// PaginateWithBaseURL runs Paginate and sets the base URL.
func PaginateWithBaseURL(db *gorm.DB, dest any, page, perPage int, baseURL string) (*Paginator, error) {
	p, err := Paginate(db, dest, page, perPage)
	if err != nil {
		return nil, err
	}
	p.BaseUrl(baseURL)
	return p, nil
}

func (p *Paginator) String() string {
	return fmt.Sprintf("Page %d of %d (%d total)", p.CurrentPage, p.LastPage, p.Total)
}
