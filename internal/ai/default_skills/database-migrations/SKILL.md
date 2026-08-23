---
name: database-migrations
description: Relational database schema design, migrations, indexing strategies, transactions, foreign keys, and Lucid ORM relations for PostgreSQL, MySQL, and SQLite.
---

# Database Migrations & Schema Expert

Guidelines for schema migrations, indexing, and transactional data integrity in Nimbus.

## Schema Principles

1. **Safe Migration Practices**:
   - Write reversible migrations (`up` and `down`).
   - Add columns with default values or nullable flags to prevent table locks.
   - Avoid dropping columns in production before deprecating application references.

2. **Index Optimization**:
   - Add composite indexes for multi-column query filters (e.g. `(user_id, status)`).
   - Use unique indexes for idempotency and natural primary constraints.

3. **Transactions**:
   - Wrap multi-table mutation operations in database transactions to maintain consistency:
     ```go
     err := db.Transaction(func(tx *gorm.DB) error {
         if err := tx.Create(&order).Error; err != nil {
             return err
         }
         return tx.Model(&inventory).Update("stock", gorm.Expr("stock - ?", 1)).Error
     })
     ```
