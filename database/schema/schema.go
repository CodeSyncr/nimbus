package schema

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// BaseSchema is the base for AdonisJS Lucid-style migrations.
// Embed it in your migration struct and implement TableName, Up, and Down.
//
//	type CreateUsers struct {
//	    schema.BaseSchema
//	}
//
//	func (m *CreateUsers) TableName() string { return "users" }
//
//	func (m *CreateUsers) Up(db *gorm.DB) error {
//	    return schema.New(db).CreateTable("users", func(t *schema.Table) {
//	        t.Increments("id")
//	        t.Timestamps()
//	    })
//	}
//
//	func (m *CreateUsers) Down(db *gorm.DB) error {
//	    return schema.New(db).DropTable("users")
//	}
type BaseSchema struct{}

// Schema holds the database connection for migrations.
type Schema struct {
	db *gorm.DB
}

// New creates a Schema for the given DB.
func New(db *gorm.DB) *Schema {
	return &Schema{db: db}
}

// CreateTable creates a table with the given name. The callback receives
// a Table builder to define columns.
func (s *Schema) CreateTable(name string, fn func(*Table)) error {
	t := &Table{
		name:    name,
		db:      s.db,
		columns: make([]columnDef, 0),
		indexes: make([]indexDef, 0),
	}
	fn(t)
	return t.execCreate()
}

// DropTable drops the table.
func (s *Schema) DropTable(name string) error {
	return s.db.Migrator().DropTable(name)
}

// Table builds column definitions for CreateTable.
type Table struct {
	name    string
	db      *gorm.DB
	columns []columnDef
	indexes []indexDef
}

type columnDef struct {
	name     string
	typ      string
	nullable bool
	default_ string
	unique   bool
	unsigned bool
	primary  bool
	comment  string
}

type indexDef struct {
	name    string
	columns []string
	unique  bool
}

// Increments adds an auto-increment primary key (id).
func (t *Table) Increments(name string) *Table {
	t.columns = append(t.columns, columnDef{name: name, typ: "INTEGER PRIMARY KEY AUTOINCREMENT"})
	return t
}

// BigIncrements adds a bigint auto-increment primary key.
func (t *Table) BigIncrements(name string) *Table {
	t.columns = append(t.columns, columnDef{name: name, typ: "INTEGER PRIMARY KEY AUTOINCREMENT"})
	return t
}

// String adds a varchar column.
func (t *Table) String(name string, size int) *Table {
	if size <= 0 {
		size = 255
	}
	t.columns = append(t.columns, columnDef{name: name, typ: fmt.Sprintf("VARCHAR(%d)", size)})
	return t
}

// Text adds a text column.
func (t *Table) Text(name string) *Table {
	t.columns = append(t.columns, columnDef{name: name, typ: "TEXT"})
	return t
}

// LongText adds a long text column.
func (t *Table) LongText(name string) *Table {
	t.columns = append(t.columns, columnDef{name: name, typ: "TEXT"})
	return t
}

// Boolean adds a boolean column.
func (t *Table) Boolean(name string) *Table {
	t.columns = append(t.columns, columnDef{name: name, typ: "BOOLEAN"})
	return t
}

// Integer adds an integer column.
func (t *Table) Integer(name string) *Table {
	t.columns = append(t.columns, columnDef{name: name, typ: "INTEGER"})
	return t
}

// SmallInteger adds a smallint column.
func (t *Table) SmallInteger(name string) *Table {
	t.columns = append(t.columns, columnDef{name: name, typ: "SMALLINT"})
	return t
}

// BigInteger adds a bigint column.
func (t *Table) BigInteger(name string) *Table {
	t.columns = append(t.columns, columnDef{name: name, typ: "BIGINT"})
	return t
}

// Float adds a float column.
func (t *Table) Float(name string, precision, scale int) *Table {
	if precision <= 0 {
		precision = 8
	}
	if scale <= 0 {
		scale = 2
	}
	t.columns = append(t.columns, columnDef{name: name, typ: fmt.Sprintf("FLOAT(%d,%d)", precision, scale)})
	return t
}

// Decimal adds a decimal column.
func (t *Table) Decimal(name string, precision, scale int) *Table {
	if precision <= 0 {
		precision = 8
	}
	if scale <= 0 {
		scale = 2
	}
	t.columns = append(t.columns, columnDef{name: name, typ: fmt.Sprintf("DECIMAL(%d,%d)", precision, scale)})
	return t
}

// Date adds a DATE column.
func (t *Table) Date(name string) *Table {
	t.columns = append(t.columns, columnDef{name: name, typ: "DATE"})
	return t
}

// Time adds a TIME column.
func (t *Table) Time(name string) *Table {
	t.columns = append(t.columns, columnDef{name: name, typ: "TIME"})
	return t
}

// Timestamp adds a timestamp column (created_at, updated_at).
func (t *Table) Timestamp(name string) *Table {
	t.columns = append(t.columns, columnDef{name: name, typ: "DATETIME"})
	return t
}

// DateTime adds a DATETIME column.
func (t *Table) DateTime(name string) *Table {
	t.columns = append(t.columns, columnDef{name: name, typ: "DATETIME"})
	return t
}

// JSON adds a JSON column.
func (t *Table) JSON(name string) *Table {
	t.columns = append(t.columns, columnDef{name: name, typ: "JSON"})
	return t
}

// JSONB adds a JSONB column (primarily for Postgres).
func (t *Table) JSONB(name string) *Table {
	t.columns = append(t.columns, columnDef{name: name, typ: "JSONB"})
	return t
}

// UUID adds a UUID column.
func (t *Table) UUID(name string) *Table {
	t.columns = append(t.columns, columnDef{name: name, typ: "UUID"})
	return t
}

// Binary adds a binary/BLOB column.
func (t *Table) Binary(name string) *Table {
	t.columns = append(t.columns, columnDef{name: name, typ: "BLOB"})
	return t
}

// Enum adds an enum-like column. Backed by VARCHAR; values are not enforced at DB level.
func (t *Table) Enum(name string, _ []string) *Table {
	t.columns = append(t.columns, columnDef{name: name, typ: "VARCHAR(255)"})
	return t
}

// Timestamps adds created_at and updated_at columns.
func (t *Table) Timestamps() *Table {
	t.columns = append(t.columns, columnDef{name: "created_at", typ: "DATETIME"})
	t.columns = append(t.columns, columnDef{name: "updated_at", typ: "DATETIME"})
	return t
}

// SoftDeletes adds deleted_at column (nullable) for GORM soft delete.
func (t *Table) SoftDeletes() *Table {
	t.columns = append(t.columns, columnDef{name: "deleted_at", typ: "DATETIME", nullable: true})
	return t
}

// ID is a shorthand for an auto-increment primary key named "id".
func (t *Table) ID() *Table {
	return t.Increments("id")
}

// UUIDPrimary adds a UUID primary key named "id".
func (t *Table) UUIDPrimary() *Table {
	t.columns = append(t.columns, columnDef{name: "id", typ: "UUID", primary: true})
	return t
}

// Nullable marks the last column as nullable.
func (t *Table) Nullable() *Table {
	if len(t.columns) > 0 {
		t.columns[len(t.columns)-1].nullable = true
	}
	return t
}

// Default sets the default value for the last column.
func (t *Table) Default(val string) *Table {
	if len(t.columns) > 0 {
		t.columns[len(t.columns)-1].default_ = val
	}
	return t
}

// Unique marks the last column as UNIQUE.
func (t *Table) Unique() *Table {
	if len(t.columns) > 0 {
		t.columns[len(t.columns)-1].unique = true
	}
	return t
}

// NotNull marks the last column as NOT NULL.
func (t *Table) NotNull() *Table {
	if len(t.columns) > 0 {
		t.columns[len(t.columns)-1].nullable = false
	}
	return t
}

// Unsigned marks the last numeric column as UNSIGNED.
func (t *Table) Unsigned() *Table {
	if len(t.columns) > 0 {
		t.columns[len(t.columns)-1].unsigned = true
	}
	return t
}

// Comment sets a comment for the last column.
func (t *Table) Comment(text string) *Table {
	if len(t.columns) > 0 {
		t.columns[len(t.columns)-1].comment = text
	}
	return t
}

// Primary marks the last column as PRIMARY KEY.
func (t *Table) Primary() *Table {
	if len(t.columns) > 0 {
		t.columns[len(t.columns)-1].primary = true
	}
	return t
}

// Index registers a non-unique index for the given column. If column is empty,
// the last defined column is used.
func (t *Table) Index(column string) *Table {
	if column == "" && len(t.columns) > 0 {
		column = t.columns[len(t.columns)-1].name
	}
	if column == "" {
		return t
	}
	name := fmt.Sprintf("%s_%s_index", t.name, column)
	t.indexes = append(t.indexes, indexDef{
		name:    name,
		columns: []string{column},
		unique:  false,
	})
	return t
}

// CompositeIndex registers a non-unique index for multiple columns.
func (t *Table) CompositeIndex(cols []string) *Table {
	if len(cols) == 0 {
		return t
	}
	name := fmt.Sprintf("%s_%s_index", t.name, strings.Join(cols, "_"))
	t.indexes = append(t.indexes, indexDef{
		name:    name,
		columns: cols,
		unique:  false,
	})
	return t
}

// UniqueComposite registers a unique index for multiple columns.
func (t *Table) UniqueComposite(cols []string) *Table {
	if len(cols) == 0 {
		return t
	}
	name := fmt.Sprintf("%s_%s_unique", t.name, strings.Join(cols, "_"))
	t.indexes = append(t.indexes, indexDef{
		name:    name,
		columns: cols,
		unique:  true,
	})
	return t
}

// Check is a no-op placeholder for future CHECK constraint support.
func (t *Table) Check(_ string) *Table {
	return t
}

// After is a no-op placeholder for column positioning (MySQL) in ALTER TABLE.
func (t *Table) After(_ string) *Table {
	return t
}

// First is a no-op placeholder for column positioning (MySQL) in ALTER TABLE.
func (t *Table) First() *Table {
	return t
}

// Generated is a no-op placeholder for generated columns.
func (t *Table) Generated(_ string, _ string) *Table {
	return t
}

// ForeignId adds a foreign key column (e.g. user_id) referencing table.id.
func (t *Table) ForeignId(column, references string) *Table {
	t.columns = append(t.columns, columnDef{name: column, typ: "INTEGER"})
	return t
}

// AlterTable alters an existing table (add column). Use in migrations for schema changes.
func (s *Schema) AlterTable(name string, fn func(*Table)) error {
	t := &Table{name: name, db: s.db, columns: make([]columnDef, 0)}
	fn(t)
	return t.execAlter()
}

func (t *Table) execAlter() error {
	for _, c := range t.columns {
		s := fmt.Sprintf("%q %s", c.name, c.typ)
		if !c.nullable && c.typ != "INTEGER PRIMARY KEY AUTOINCREMENT" {
			s += " NOT NULL"
		}
		if c.unique {
			s += " UNIQUE"
		}
		if c.default_ != "" {
			s += " DEFAULT " + c.default_
		}
		sql := fmt.Sprintf("ALTER TABLE %q ADD COLUMN %s", t.name, s)
		if err := t.db.Exec(sql).Error; err != nil {
			return err
		}
	}
	// Apply indexes defined during AlterTable as well.
	for _, idx := range t.indexes {
		if err := t.execIndex(idx); err != nil {
			return err
		}
	}
	return nil
}

func (t *Table) execCreate() error {
	parts := make([]string, 0, len(t.columns))
	for _, c := range t.columns {
		s := fmt.Sprintf("%q %s", c.name, c.typ)
		if !c.nullable && c.typ != "INTEGER PRIMARY KEY AUTOINCREMENT" {
			s += " NOT NULL"
		}
		if c.unique {
			s += " UNIQUE"
		}
		if c.default_ != "" {
			s += " DEFAULT " + c.default_
		}
		parts = append(parts, s)
	}
	sql := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %q (%s)", t.name, strings.Join(parts, ", "))
	if err := t.db.Exec(sql).Error; err != nil {
		return err
	}
	for _, idx := range t.indexes {
		if err := t.execIndex(idx); err != nil {
			return err
		}
	}
	return nil
}

func (t *Table) execIndex(idx indexDef) error {
	if len(idx.columns) == 0 {
		return nil
	}
	cols := make([]string, 0, len(idx.columns))
	for _, c := range idx.columns {
		cols = append(cols, fmt.Sprintf("%q", c))
	}
	stmt := "CREATE INDEX IF NOT EXISTS %q ON %q (%s)"
	if idx.unique {
		stmt = "CREATE UNIQUE INDEX IF NOT EXISTS %q ON %q (%s)"
	}
	sql := fmt.Sprintf(stmt, idx.name, t.name, strings.Join(cols, ", "))
	return t.db.Exec(sql).Error
}
