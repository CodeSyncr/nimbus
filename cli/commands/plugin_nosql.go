package commands

const nosqlConfigFile = `/*
|--------------------------------------------------------------------------
| NoSQL Configuration
|--------------------------------------------------------------------------
|
| Configure MongoDB / NoSQL connections. Set MONGO_URI and
| MONGO_DATABASE in your .env file. The connection is
| established at boot and registered as "mongo" in the
| NoSQL connection manager.
|
| See: /docs/nosql
|
*/

package config

var NoSQL NoSQLConfig

type NoSQLConfig struct {
	// MongoURI is the MongoDB connection string.
	// Example: "mongodb://localhost:27017"
	MongoURI string

	// MongoDatabase is the default database name.
	MongoDatabase string

	// ConnectTimeout controls how long to wait for the initial connection.
	// Default: "10s"
	ConnectTimeout string

	// MaxPoolSize sets the maximum number of connections in the pool.
	// Default: 100
	MaxPoolSize uint64

	// MinPoolSize sets the minimum number of idle connections.
	// Default: 0
	MinPoolSize uint64
}

func loadNoSQL() {
	NoSQL = NoSQLConfig{
		MongoURI:       cfg("mongo.uri", ""),
		MongoDatabase:  cfg("mongo.database", "nimbus"),
		ConnectTimeout: cfg("mongo.connect_timeout", "10s"),
	}
}
`

const nosqlBootFunc = `func bootNoSQL(app *nimbus.App) {
	if config.NoSQL.MongoURI == "" {
		return // NoSQL not configured — skip silently
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoDriver, err := nosql.ConnectMongo(ctx, nosql.MongoConfig{
		URI:      config.NoSQL.MongoURI,
		Database: config.NoSQL.MongoDatabase,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "MongoDB connection failed: %v\n", err)
		os.Exit(1)
	}

	nosql.Register("mongo", mongoDriver)

	nimbus.SetNoSQL(mongoDriver)
	app.Container.Singleton("nosql", func() *nimbus.NoSQL {
		return nimbus.GetNoSQL()
	})
}
`
