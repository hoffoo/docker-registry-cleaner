package main

import (
	"time"

	"log"
	"registry/registry"
)

func main() {

	path := "registry/t"
	old := time.Hour * 24 * 20
	pretend := true

	err := registry.DeleteOldImages(path, old, pretend)
	if err != nil {
		log.Println(err)
	}
}
