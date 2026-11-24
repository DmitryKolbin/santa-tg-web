package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"

	appdb "github.com/DmitryKolbin/santa-tg-web/internal/database"
	"github.com/DmitryKolbin/santa-tg-web/internal/migrateold"
)

func main() {
	var sourcePath, targetPath string
	flag.StringVar(&sourcePath, "source", "", "path to the old SQLite database")
	flag.StringVar(&targetPath, "target", "", "path for the new SQLite database")
	flag.Parse()
	if sourcePath == "" || targetPath == "" {
		log.Fatal("set --source and --target")
	}
	var err error
	sourcePath, err = filepath.Abs(sourcePath)
	if err != nil {
		log.Fatal(err)
	}
	targetPath, err = filepath.Abs(targetPath)
	if err != nil {
		log.Fatal(err)
	}
	if sourcePath == targetPath {
		log.Fatal("source and target must be different files")
	}
	if info, err := os.Stat(sourcePath); err != nil || !info.Mode().IsRegular() {
		log.Fatal("source must be an existing regular file")
	}
	targetExisted := false
	if info, err := os.Stat(targetPath); err == nil {
		targetExisted = true
		if !info.Mode().IsRegular() || info.Size() != 0 {
			log.Fatal("target must not exist or must be an empty regular file")
		}
	} else if !os.IsNotExist(err) {
		log.Fatal(err)
	}

	sourceURL := (&url.URL{Scheme: "file", Path: sourcePath, RawQuery: "mode=ro"}).String()
	source, err := appdb.Open(sourceURL)
	if err != nil {
		log.Fatal(err)
	}
	defer source.Close()
	target, err := appdb.Open(targetPath)
	if err != nil {
		log.Fatal(err)
	}
	report, err := migrateold.Migrate(context.Background(), appdb.New(source), appdb.New(target))
	target.Close()
	if err != nil {
		if !targetExisted {
			os.Remove(targetPath)
		}
		log.Fatal(err)
	}
	fmt.Printf("Imported %d users, %d games, %d members and %d assignments.\n", report.Users, report.Games, report.Members, report.Assignments)
	for _, id := range report.ResetGames {
		fmt.Printf("Warning: game %d had an invalid route and was imported as not started.\n", id)
	}
}
