package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"

	"horizon/deployment/database"
	"horizon/foundation/system/identity"
)

func main() {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		log.Fatal("DATABASE_URL is required")
	}
	options := database.Options{
		Tenant:   os.Getenv("BOOTSTRAP_TENANT"),
		Username: strings.ToLower(os.Getenv("BOOTSTRAP_USERNAME")),
	}
	if options.Username != "" && !regexp.MustCompile(`^[a-z0-9_.@-]{3,80}$`).MatchString(options.Username) {
		log.Fatal("invalid BOOTSTRAP_USERNAME")
	}
	if password := os.Getenv("BOOTSTRAP_PASSWORD"); password != "" && password != "111111" {
		hash, err := identity.HashPassword(password)
		if err != nil {
			log.Fatal(err)
		}
		options.PasswordHash = hash
	}
	created, err := database.Initialize(context.Background(), url, options)
	if err != nil {
		log.Fatal(err)
	}
	if !created {
		fmt.Println("Horizon database is already initialized.")
		return
	}
	if options.Username == "" {
		options.Username = "admin"
	}
	fmt.Printf("Horizon database initialized with administrator %s.\n", options.Username)
}
