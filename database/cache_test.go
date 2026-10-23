package database

import "testing"

// A second CachedFind with the same key is a cache hit, and must still fill
// the caller's destination.
func TestCachedFindFillsDestOnHit(t *testing.T) {
	db := openTestDB(t)
	if err := db.Exec("CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO notes (body) VALUES ('a'), ('b')").Error; err != nil {
		t.Fatal(err)
	}
	type note struct {
		ID   uint
		Body string
	}
	for i := 0; i < 2; i++ {
		var notes []note
		if err := CachedFind(db.Table("notes").Order("id"), "notes:all:test", 0, &notes); err != nil {
			t.Fatal(err)
		}
		if len(notes) != 2 || notes[1].Body != "b" {
			t.Fatalf("call %d: got %+v", i+1, notes)
		}
	}
}
