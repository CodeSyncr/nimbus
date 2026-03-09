package database

import (
	"math/rand"

	"gorm.io/gorm"
)

// Factory generates fake data for models (Lucid-style factories).
type Factory struct {
	tableName string
	define    func(f *Faker) map[string]any
}

// Faker provides simple fake data helpers.
type Faker struct{}

// Sentence returns a short fake sentence.
func (f *Faker) Sentence() string {
	words := []string{"Lorem", "ipsum", "dolor", "sit", "amet", "consectetur", "adipiscing", "elit"}
	n := 3 + rand.Intn(5)
	s := ""
	for i := 0; i < n; i++ {
		if i > 0 {
			s += " "
		}
		s += words[rand.Intn(len(words))]
	}
	return s + "."
}

// Paragraph returns a few sentences.
func (f *Faker) Paragraph() string {
	n := 2 + rand.Intn(3)
	s := ""
	for i := 0; i < n; i++ {
		if i > 0 {
			s += " "
		}
		s += (&Faker{}).Sentence()
	}
	return s
}

// Email returns a fake email.
func (f *Faker) Email() string {
	users := []string{"user", "admin", "test", "demo", "john", "jane"}
	domains := []string{"example.com", "test.com", "mail.org"}
	return users[rand.Intn(len(users))] + "@" + domains[rand.Intn(len(domains))]
}

// Word returns a random word.
func (f *Faker) Word() string {
	words := []string{"alpha", "beta", "gamma", "delta", "echo", "foxtrot"}
	return words[rand.Intn(len(words))]
}

// Int returns a random int in [min, max].
func (f *Faker) Int(min, max int) int {
	if min >= max {
		return min
	}
	return min + rand.Intn(max-min+1)
}

// Define creates a factory for the given table.
func Define(tableName string, fn func(f *Faker) map[string]any) *Factory {
	return &Factory{tableName: tableName, define: fn}
}

// Create inserts one record with fake data. Override specific fields with attrs.
func (fac *Factory) Create(db *gorm.DB, attrs ...map[string]any) error {
	data := fac.define(&Faker{})
	for _, a := range attrs {
		for k, v := range a {
			data[k] = v
		}
	}
	return db.Table(fac.tableName).Create(data).Error
}

// CreateMany inserts n records with fake data.
func (fac *Factory) CreateMany(db *gorm.DB, n int) error {
	for i := 0; i < n; i++ {
		data := fac.define(&Faker{})
		if err := db.Table(fac.tableName).Create(data).Error; err != nil {
			return err
		}
	}
	return nil
}

// Merge overrides factory data with the given attrs (for Create).
func (fac *Factory) Merge(attrs map[string]any) *FactoryWithAttrs {
	return &FactoryWithAttrs{factory: fac, attrs: attrs}
}

// FactoryWithAttrs is a factory with pre-set overrides.
type FactoryWithAttrs struct {
	factory *Factory
	attrs   map[string]any
}

// Create creates one record with merged attributes.
func (f *FactoryWithAttrs) Create(db *gorm.DB) error {
	return f.factory.Create(db, f.attrs)
}
