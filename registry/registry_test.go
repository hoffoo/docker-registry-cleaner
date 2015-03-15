package registry

import (
	"testing"
	"time"
)

func TestDeleteOldImages(t *testing.T) {

	path := "./t"
	old := time.Hour * 24 * 7
	pretend := true

	err := DeleteOldImages(path, old, pretend)
	if err != nil {
		t.Fatal(err)
	}
}

func TestGetTags(t *testing.T) {

	r := &Registry{root: "./t"}

	// get all tags
	tags, err := r.GetTags()
	if err != nil {
		t.Fatal(err)
	}

	if len(tags) == 0 {
		t.Fatal("got no tags")
	}

	markTagsOlderThan(tags, time.Hour*24*10)
	markImagesThatAreStale(tags)
}
