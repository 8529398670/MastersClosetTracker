// Package testutil holds helpers shared by the tests of several packages.
// Nothing outside a _test.go file imports it.
package testutil

import (
	filepath "path/filepath"
	testing "testing"

	config "github.com/0187773933/MastersClosetTracker/server/config"
	db "github.com/0187773933/MastersClosetTracker/server/db"
)

// Key is a fixed secret key for test databases.
const Key = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// Config is a resolved config pointing into a fresh temporary directory.
func Config( t testing.TB ) ( cfg *config.Config ) {
	t.Helper()
	dir := t.TempDir()
	cfg = &config.Config{
		ProjectName:   "test",
		AppDir:        dir,
		DatabasePath:  filepath.Join( dir , config.DatabaseFileName ),
		StorageDir:    dir,
		SecretKey:      Key,
		EncryptAtRest:  true,
		DatabaseNoSync: true,
	}
	return
}

// OpenStore opens a database at path (creating it), closed when the test
// ends unless the caller closes it first.
func OpenStore( t testing.TB , path string , extra_buckets ...string ) ( store *db.Store ) {
	t.Helper()
	cfg := &config.Config{ DatabasePath: path , SecretKey: Key , EncryptAtRest: true , DatabaseNoSync: true }
	store , err := db.Open( cfg , extra_buckets... )
	if err != nil {
		t.Fatalf( "open %s: %v" , path , err )
	}
	return
}
