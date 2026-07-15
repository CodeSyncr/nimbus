package models

import (
	"github.com/CodeSyncr/nimbus/database"
	"github.com/CodeSyncr/nimbus/lucid"
)

// Migrations creates the Passport (OAuth2 server) tables. Exposed via the
// plugin's Migrations() so they run alongside application migrations.
func Migrations() []database.Migration {
	return []database.Migration{
		{
			Name: "2026_07_15_000101_create_oauth_clients",
			Up:   func(db *lucid.DB) error { return db.AutoMigrate(&OAuthClient{}) },
			Down: func(db *lucid.DB) error { return db.Migrator().DropTable(&OAuthClient{}) },
		},
		{
			Name: "2026_07_15_000102_create_oauth_auth_codes",
			Up:   func(db *lucid.DB) error { return db.AutoMigrate(&OAuthAuthCode{}) },
			Down: func(db *lucid.DB) error { return db.Migrator().DropTable(&OAuthAuthCode{}) },
		},
		{
			Name: "2026_07_15_000103_create_oauth_access_tokens",
			Up:   func(db *lucid.DB) error { return db.AutoMigrate(&OAuthAccessToken{}) },
			Down: func(db *lucid.DB) error { return db.Migrator().DropTable(&OAuthAccessToken{}) },
		},
		{
			Name: "2026_07_15_000104_create_oauth_refresh_tokens",
			Up:   func(db *lucid.DB) error { return db.AutoMigrate(&OAuthRefreshToken{}) },
			Down: func(db *lucid.DB) error { return db.Migrator().DropTable(&OAuthRefreshToken{}) },
		},
	}
}
