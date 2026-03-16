package nosql

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// ══════════════════════════════════════════════════════════════════
// MongoDB Driver — implements nosql.Driver
// ══════════════════════════════════════════════════════════════════
//
// Usage:
//
//	client, _ := nosql.ConnectMongo(ctx, nosql.MongoConfig{
//	    URI:      "mongodb://localhost:27017",
//	    Database: "myapp",
//	})
//	nosql.Register("mongo", client)
//
//	// Later:
//	coll := nosql.Connection("mongo").Collection("users")
//	coll.InsertOne(ctx, User{Name: "Alice", Email: "alice@example.com"})

// MongoConfig configures a MongoDB connection.
type MongoConfig struct {
	// URI is the MongoDB connection string (e.g. "mongodb://localhost:27017").
	URI string

	// Database is the default database name.
	Database string

	// ConnectTimeout is the connection timeout (default: 10s).
	ConnectTimeout time.Duration

	// MaxPoolSize sets the max number of connections in the pool (default: 100).
	MaxPoolSize uint64

	// MinPoolSize sets the min connections to keep in the pool.
	MinPoolSize uint64
}

// MongoDriver wraps the official MongoDB Go driver.
type MongoDriver struct {
	client   *mongo.Client
	database *mongo.Database
	dbName   string
}

// ConnectMongo creates a new MongoDB connection.
func ConnectMongo(ctx context.Context, cfg MongoConfig) (*MongoDriver, error) {
	if cfg.ConnectTimeout == 0 {
		cfg.ConnectTimeout = 10 * time.Second
	}

	opts := options.Client().ApplyURI(cfg.URI)
	if cfg.MaxPoolSize > 0 {
		opts.SetMaxPoolSize(cfg.MaxPoolSize)
	}
	if cfg.MinPoolSize > 0 {
		opts.SetMinPoolSize(cfg.MinPoolSize)
	}

	client, err := mongo.Connect(opts)
	if err != nil {
		return nil, fmt.Errorf("nosql/mongo: connect: %w", err)
	}

	// Verify connection
	pingCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()
	if err := client.Ping(pingCtx, nil); err != nil {
		return nil, fmt.Errorf("nosql/mongo: ping: %w", err)
	}

	db := client.Database(cfg.Database)
	return &MongoDriver{
		client:   client,
		database: db,
		dbName:   cfg.Database,
	}, nil
}

// ── Driver Interface ────────────────────────────────────────────

func (d *MongoDriver) Name() string { return "mongodb" }

func (d *MongoDriver) Collection(name string) Collection {
	return &MongoCollection{
		coll: d.database.Collection(name),
	}
}

func (d *MongoDriver) Ping(ctx context.Context) error {
	return d.client.Ping(ctx, nil)
}

func (d *MongoDriver) Close(ctx context.Context) error {
	return d.client.Disconnect(ctx)
}

func (d *MongoDriver) Database(name string) Driver {
	return &MongoDriver{
		client:   d.client,
		database: d.client.Database(name),
		dbName:   name,
	}
}

func (d *MongoDriver) DropDatabase(ctx context.Context) error {
	return d.database.Drop(ctx)
}

// Client returns the underlying *mongo.Client for advanced usage.
func (d *MongoDriver) Client() *mongo.Client {
	return d.client
}

// DB returns the underlying *mongo.Database for advanced usage.
func (d *MongoDriver) DB() *mongo.Database {
	return d.database
}

// ── Collection Implementation ───────────────────────────────────

// MongoCollection implements nosql.Collection using MongoDB.
type MongoCollection struct {
	coll *mongo.Collection
}

func (c *MongoCollection) Name() string {
	return c.coll.Name()
}

// ── Insert ──────────────────────────────────────────────────────

func (c *MongoCollection) InsertOne(ctx context.Context, doc any) (*InsertResult, error) {
	res, err := c.coll.InsertOne(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("nosql/mongo: insertOne: %w", err)
	}
	return &InsertResult{InsertedID: res.InsertedID}, nil
}

func (c *MongoCollection) InsertMany(ctx context.Context, docs []any) (*InsertManyResult, error) {
	res, err := c.coll.InsertMany(ctx, docs)
	if err != nil {
		return nil, fmt.Errorf("nosql/mongo: insertMany: %w", err)
	}
	ids := make([]any, len(res.InsertedIDs))
	for i, id := range res.InsertedIDs {
		ids[i] = id
	}
	return &InsertManyResult{InsertedIDs: ids}, nil
}

// ── Find ────────────────────────────────────────────────────────

func (c *MongoCollection) FindOne(ctx context.Context, filter Filter, dest any) error {
	f := toBsonDoc(filter)
	res := c.coll.FindOne(ctx, f)
	if err := res.Err(); err != nil {
		return fmt.Errorf("nosql/mongo: findOne: %w", err)
	}
	return res.Decode(dest)
}

func (c *MongoCollection) Find(ctx context.Context, filter Filter, dest any, opts ...FindOption) error {
	f := toBsonDoc(filter)

	findOpts := options.Find()
	if len(opts) > 0 {
		opt := opts[0]
		if opt.Limit > 0 {
			findOpts.SetLimit(opt.Limit)
		}
		if opt.Skip > 0 {
			findOpts.SetSkip(opt.Skip)
		}
		if len(opt.Sort) > 0 {
			sortDoc := bson.D{}
			for k, v := range opt.Sort {
				sortDoc = append(sortDoc, bson.E{Key: k, Value: int(v)})
			}
			findOpts.SetSort(sortDoc)
		}
		if len(opt.Projection) > 0 {
			findOpts.SetProjection(toBsonDoc(Document(opt.Projection)))
		}
	}

	cursor, err := c.coll.Find(ctx, f, findOpts)
	if err != nil {
		return fmt.Errorf("nosql/mongo: find: %w", err)
	}
	defer cursor.Close(ctx)

	return cursor.All(ctx, dest)
}

func (c *MongoCollection) FindByID(ctx context.Context, id any, dest any) error {
	return c.FindOne(ctx, Filter{"_id": id}, dest)
}

func (c *MongoCollection) Count(ctx context.Context, filter Filter) (int64, error) {
	f := toBsonDoc(filter)
	count, err := c.coll.CountDocuments(ctx, f)
	if err != nil {
		return 0, fmt.Errorf("nosql/mongo: count: %w", err)
	}
	return count, nil
}

func (c *MongoCollection) Exists(ctx context.Context, filter Filter) (bool, error) {
	count, err := c.Count(ctx, filter)
	return count > 0, err
}

// ── Update ──────────────────────────────────────────────────────

func (c *MongoCollection) UpdateOne(ctx context.Context, filter Filter, update any) (*UpdateResult, error) {
	f := toBsonDoc(filter)
	u := wrapUpdate(update)
	res, err := c.coll.UpdateOne(ctx, f, u)
	if err != nil {
		return nil, fmt.Errorf("nosql/mongo: updateOne: %w", err)
	}
	return toUpdateResult(res), nil
}

func (c *MongoCollection) UpdateMany(ctx context.Context, filter Filter, update any) (*UpdateResult, error) {
	f := toBsonDoc(filter)
	u := wrapUpdate(update)
	res, err := c.coll.UpdateMany(ctx, f, u)
	if err != nil {
		return nil, fmt.Errorf("nosql/mongo: updateMany: %w", err)
	}
	return toUpdateResult(res), nil
}

func (c *MongoCollection) UpdateByID(ctx context.Context, id any, update any) (*UpdateResult, error) {
	u := wrapUpdate(update)
	res, err := c.coll.UpdateByID(ctx, id, u)
	if err != nil {
		return nil, fmt.Errorf("nosql/mongo: updateByID: %w", err)
	}
	return toUpdateResult(res), nil
}

func (c *MongoCollection) Upsert(ctx context.Context, filter Filter, doc any) (*UpdateResult, error) {
	f := toBsonDoc(filter)
	u := wrapUpdate(doc)
	opts := options.UpdateOne().SetUpsert(true)
	res, err := c.coll.UpdateOne(ctx, f, u, opts)
	if err != nil {
		return nil, fmt.Errorf("nosql/mongo: upsert: %w", err)
	}
	return toUpdateResult(res), nil
}

// ── Delete ──────────────────────────────────────────────────────

func (c *MongoCollection) DeleteOne(ctx context.Context, filter Filter) (*DeleteResult, error) {
	f := toBsonDoc(filter)
	res, err := c.coll.DeleteOne(ctx, f)
	if err != nil {
		return nil, fmt.Errorf("nosql/mongo: deleteOne: %w", err)
	}
	return &DeleteResult{DeletedCount: res.DeletedCount}, nil
}

func (c *MongoCollection) DeleteMany(ctx context.Context, filter Filter) (*DeleteResult, error) {
	f := toBsonDoc(filter)
	res, err := c.coll.DeleteMany(ctx, f)
	if err != nil {
		return nil, fmt.Errorf("nosql/mongo: deleteMany: %w", err)
	}
	return &DeleteResult{DeletedCount: res.DeletedCount}, nil
}

func (c *MongoCollection) DeleteByID(ctx context.Context, id any) (*DeleteResult, error) {
	return c.DeleteOne(ctx, Filter{"_id": id})
}

// ── Aggregation ─────────────────────────────────────────────────

func (c *MongoCollection) Aggregate(ctx context.Context, pipeline any, dest any) error {
	cursor, err := c.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return fmt.Errorf("nosql/mongo: aggregate: %w", err)
	}
	defer cursor.Close(ctx)
	return cursor.All(ctx, dest)
}

func (c *MongoCollection) Distinct(ctx context.Context, field string, filter Filter) ([]any, error) {
	f := toBsonDoc(filter)
	res := c.coll.Distinct(ctx, field, f)
	if res.Err() != nil {
		return nil, fmt.Errorf("nosql/mongo: distinct: %w", res.Err())
	}
	var values []any
	if err := res.Decode(&values); err != nil {
		return nil, fmt.Errorf("nosql/mongo: distinct decode: %w", err)
	}
	return values, nil
}

// ── Index ───────────────────────────────────────────────────────

func (c *MongoCollection) CreateIndex(ctx context.Context, keys Document, opts ...IndexOption) (string, error) {
	keysDoc := toBsonDoc(keys)
	indexModel := mongo.IndexModel{Keys: keysDoc}

	if len(opts) > 0 {
		opt := opts[0]
		indexOpts := options.Index()
		if opt.Unique {
			indexOpts.SetUnique(true)
		}
		if opt.Name != "" {
			indexOpts.SetName(opt.Name)
		}
		if opt.ExpireAfterSeconds != nil {
			indexOpts.SetExpireAfterSeconds(*opt.ExpireAfterSeconds)
		}
		if opt.Sparse {
			indexOpts.SetSparse(true)
		}
		indexModel.Options = indexOpts
	}

	name, err := c.coll.Indexes().CreateOne(ctx, indexModel)
	if err != nil {
		return "", fmt.Errorf("nosql/mongo: createIndex: %w", err)
	}
	return name, nil
}

func (c *MongoCollection) DropIndex(ctx context.Context, name string) error {
	err := c.coll.Indexes().DropOne(ctx, name)
	if err != nil {
		return fmt.Errorf("nosql/mongo: dropIndex: %w", err)
	}
	return nil
}

// ── Collection Management ───────────────────────────────────────

func (c *MongoCollection) Drop(ctx context.Context) error {
	return c.coll.Drop(ctx)
}

// ── Helpers ─────────────────────────────────────────────────────

// toBsonDoc converts a map to a bson.D document.
func toBsonDoc(m map[string]any) bson.D {
	if m == nil || len(m) == 0 {
		return bson.D{}
	}
	doc := bson.D{}
	for k, v := range m {
		doc = append(doc, bson.E{Key: k, Value: v})
	}
	return doc
}

// wrapUpdate wraps a document in $set if it's not already an update operator.
func wrapUpdate(update any) any {
	switch u := update.(type) {
	case Filter:
		// Check if any key starts with '$' (MongoDB operator)
		for k := range u {
			if len(k) > 0 && k[0] == '$' {
				return toBsonDoc(u)
			}
		}
		return bson.D{{Key: "$set", Value: toBsonDoc(u)}}
	case Document:
		for k := range u {
			if len(k) > 0 && k[0] == '$' {
				return toBsonDoc(u)
			}
		}
		return bson.D{{Key: "$set", Value: toBsonDoc(u)}}
	case map[string]any:
		for k := range u {
			if len(k) > 0 && k[0] == '$' {
				return toBsonDoc(u)
			}
		}
		return bson.D{{Key: "$set", Value: toBsonDoc(u)}}
	default:
		return bson.D{{Key: "$set", Value: update}}
	}
}

func toUpdateResult(res *mongo.UpdateResult) *UpdateResult {
	r := &UpdateResult{
		MatchedCount:  res.MatchedCount,
		ModifiedCount: res.ModifiedCount,
		UpsertedCount: res.UpsertedCount,
	}
	if res.UpsertedID != nil {
		r.UpsertedID = res.UpsertedID
	}
	return r
}
