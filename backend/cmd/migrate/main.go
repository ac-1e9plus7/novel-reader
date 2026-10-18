// Apply offline database migrations without starting workers or model requests.
package main

import (
	"fmt"
	"novel-reader/internal/app"
	"os"
)

func main() {
	c := app.LoadConfig()
	s, e := app.OpenStore(c.DataDir)
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	defer s.Close()
	var version, invalidated int
	if e = s.DB.QueryRow("SELECT max(version) FROM schema_migrations").Scan(&version); e != nil {
		fmt.Fprintln(os.Stderr, "Cannot inspect schema version")
		os.Exit(1)
	}
	if e = s.DB.QueryRow("SELECT count(*) FROM jobs WHERE kind='translation' AND status='invalidated'").Scan(&invalidated); e != nil {
		fmt.Fprintln(os.Stderr, "Cannot inspect jobs")
		os.Exit(1)
	}
	fmt.Printf("Database version: %d; invalidated translation jobs: %d\n", version, invalidated)
	if s.MigrationBackup != "" {
		fmt.Println("Pre-migration backup:", s.MigrationBackup)
	}
}
