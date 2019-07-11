package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
)

// assertEnv logs error messages if some env variables are not defined
func assertEnv(vars ...string) {
	log.Info("Asserting environment variables...")
	undef := []string{}
	for _, varName := range vars {
		if _, ok := os.LookupEnv(varName); !ok {
			undef = append(undef, varName)
		}
	}
	if len(undef) != 0 {
		log.Fatal(fmt.Sprintf("Env required but undefined: %s", strings.Join(undef, ", ")))
	}
	log.Info("Environment is fine")
}

// prettyPrint prints arbitrary structure in human-readable format
func prettyPrint(i interface{}) string {
	s, _ := json.MarshalIndent(i, "", "\t")
	return string(s)
}
