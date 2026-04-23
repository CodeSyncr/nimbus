package commands

import "testing"

func TestBuildMySQLTimestampToDatetime6Alter_PreservesCurrentTimestampSemantics(t *testing.T) {
	def := "CURRENT_TIMESTAMP"
	stmt := buildMySQLTimestampToDatetime6Alter("users", "updated_at", false, &def, "DEFAULT_GENERATED on update CURRENT_TIMESTAMP")
	wantSub := "ALTER TABLE `users` MODIFY COLUMN `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6)"
	if stmt != wantSub {
		t.Fatalf("got %q want %q", stmt, wantSub)
	}
}

func TestBuildMySQLTimestampToDatetime6Alter_QuotesDefaultLiteral(t *testing.T) {
	def := "2020-01-01 00:00:00"
	stmt := buildMySQLTimestampToDatetime6Alter("t", "c", true, &def, "")
	want := "ALTER TABLE `t` MODIFY COLUMN `c` DATETIME(6) NULL DEFAULT '2020-01-01 00:00:00'"
	if stmt != want {
		t.Fatalf("got %q want %q", stmt, want)
	}
}

