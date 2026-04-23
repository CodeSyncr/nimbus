package telescope

import (
	"encoding/json"
	"time"

	"github.com/CodeSyncr/nimbus/lucid"
	"gorm.io/gorm"
)

type dbEntry struct {
	ID        string `gorm:"primaryKey;size:64"`
	Type      string `gorm:"index;size:32"`
	Content   string `gorm:"type:text"`
	Tags      string `gorm:"type:text"`
	BatchID   string `gorm:"index;size:64"`
	CreatedAt time.Time
}

func (dbEntry) TableName() string { return "telescope_entries" }

type dbPersist struct {
	db *lucid.DB
}

func newDBPersist(db *lucid.DB) *dbPersist {
	return &dbPersist{db: db}
}

func (p *dbPersist) migrate() error {
	return p.db.AutoMigrate(&dbEntry{})
}

func (p *dbPersist) Insert(entry *Entry) error {
	if p.db == nil || entry == nil {
		return nil
	}
	c, _ := json.Marshal(entry.Content)
	t, _ := json.Marshal(entry.Tags)
	rec := dbEntry{
		ID:        entry.ID,
		Type:      string(entry.Type),
		Content:   string(c),
		Tags:      string(t),
		BatchID:   entry.BatchID,
		CreatedAt: entry.CreatedAt,
	}
	return p.db.Create(&rec).Error
}

func (p *dbPersist) Latest(limit int) ([]*Entry, error) {
	if p.db == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 100
	}
	var rows []dbEntry
	if err := p.db.Order("created_at desc").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*Entry, 0, len(rows))
	for _, r := range rows {
		var content map[string]any
		_ = json.Unmarshal([]byte(r.Content), &content)
		var tags []string
		_ = json.Unmarshal([]byte(r.Tags), &tags)
		out = append(out, &Entry{
			ID:        r.ID,
			Type:      EntryType(r.Type),
			Content:   content,
			Tags:      tags,
			BatchID:   r.BatchID,
			CreatedAt: r.CreatedAt,
		})
	}
	return out, nil
}

func (p *dbPersist) Clear() error {
	if p.db == nil {
		return nil
	}
	return p.db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&dbEntry{}).Error
}

func (p *dbPersist) PruneBefore(t time.Time) error {
	if p.db == nil {
		return nil
	}
	return p.db.Where("created_at < ?", t).Delete(&dbEntry{}).Error
}
