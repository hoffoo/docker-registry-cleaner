package registry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// entrypoint of stuff
func DeleteOldImages(registry string, since time.Duration, pretend bool) error {

	r := &Registry{root: registry}

	// get all tags
	tags, err := r.GetTags()
	if err != nil {
		return err
	}

	markTagsOlderThan(tags, since)
	markImagesThatAreStale(tags)

	err = reallyDeleteOldImages(r, tags, pretend)
	return err
}

// holds config and sorts tags by last_update date
type Registry struct {
	root string
	name string
}

type Image struct {
	id  string
	del bool
}

// holds tag and information about it
type Tag struct {
	reg            *Registry
	repo           string
	name           string
	last_update    string
	lastUpdateTime time.Time
	image          Image
	ansestry       []*Image
	old            bool
}

func (img Image) String() string {
	return img.id
}

func (t *Tag) String() string {
	return fmt.Sprintf("%s %s %s", t.name, t.last_update, t.image)
}

// convenience wrapper to keep path and file of a repo file together
type RepoFile struct {
	info os.FileInfo
	path string
}

// get tags for all the repositries in the registry
func (r *Registry) GetTags() ([]*Tag, error) {

	var files []RepoFile
	walk := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rf := RepoFile{}
		rf.info = info
		rf.path = path
		files = append(files, rf)
		return nil
	}

	// read stuff from the repositories directory. it has information about
	// each docker tag
	filepath.Walk(filepath.Join(r.root, "repositories"), walk)

	// XXX rename to tagsMap
	tagsMap := map[string]*Tag{}

	for _, rf := range files {

		// skip junk data
		if rf.info.IsDir() || rf.info.Name() == "_index_images" {
			continue
		}

		var tag string

		// update our data structure for tag
		update := func(property, val string) {

			if _, ok := tagsMap[tag]; !ok {
				tagsMap[tag] = &Tag{
					reg: r,
				}
			}

			switch property {
			case "image":
				tagsMap[tag].image = Image{id: val}
			case "last_update":
				tagsMap[tag].last_update = val
				lu, _ := strconv.ParseInt(val, 10, 32)
				tagsMap[tag].lastUpdateTime = time.Unix(lu, 0)
			case "name":
				tagsMap[tag].name = val
			case "repo":
				tagsMap[tag].repo = val
			default:
				panic("unreachable: unknown property " + property)
			}
		}

		// the json file has some information about the tag, such as last update and
		// docker crap
		if strings.HasSuffix(rf.info.Name(), "_json") {

			f, err := os.Open(filepath.Join(rf.path))
			if err != nil {
				return nil, err
			}
			dec := json.NewDecoder(f)

			data := map[string]interface{}{}
			err = dec.Decode(&data)
			if err != nil {
				return nil, err
			}
			f.Close()

			tag = rf.info.Name()[len("tag") : len(rf.info.Name())-len("_json")]

			update("last_update", fmt.Sprintf("%0.f", data["last_update"].(float64)))
			update("name", tag)
			update("repo", rf.path)

			continue
		}

		// files starting with tag_ have the sha of the image this tag currently points at
		if strings.HasPrefix(rf.info.Name(), "tag_") {

			f, err := os.Open(filepath.Join(rf.path))
			if err != nil {
				return nil, err
			}

			var buf bytes.Buffer
			_, err = io.Copy(&buf, f)
			if err != nil {
				return nil, err
			}
			f.Close()

			tag = rf.info.Name()[len("tag_"):]

			update("image", buf.String())

			continue
		}

		panic("unreachable - dont know what to do with file " + rf.info.Name())
	}

	// copy crap from our map to a clean array and get each tag's ansestry
	tags := make([]*Tag, 0, len(tagsMap))
	for _, tag := range tagsMap {

		ansestry, err := getAncestry(*tag)
		if err != nil {
			return nil, err
		}

		tag.ansestry = make([]*Image, len(ansestry))

		for i, _ := range tag.ansestry {
			tag.ansestry[i] = &Image{id: ansestry[i]}
		}

		tags = append(tags, tag)
	}

	return tags, nil
}

// marks all tags older than the cuttoff duration, and returns the number of old tags
func markTagsOlderThan(tags []*Tag, cutoff time.Duration) {

	old := fmt.Sprint(time.Now().UnixNano() - int64(cutoff))[:10]
	for _, tag := range tags {
		// sanity check
		if len(old) != len(tag.last_update) {
			panic("omg")
		}
		if tag.last_update < old {
			tag.old = true
		}
	}
}

// marks images which are safe to delete
func markImagesThatAreStale(tags []*Tag) {

	// lets just be super dumb about this
	// safe meaning that we will not delete anything from this map
	safe := map[string]struct{}{}

	for _, t := range tags {
		if !t.old {
			safe[t.image.id] = struct{}{}
			for _, aimg := range t.ansestry {
				safe[aimg.id] = struct{}{}
			}
		}
	}

	for _, t := range tags {

		if !t.old {
			continue
		}

		// sanity check
		if _, ok := safe[t.image.id]; ok {
			continue
		}

		for _, aimg := range t.ansestry {
			if _, ok := safe[aimg.id]; ok {
				continue
			}
		}

		// if we get this far this tag and its ancestors are safe to delete
		for _, aimg := range t.ansestry {
			aimg.del = true
		}
	}
}

func reallyDeleteOldImages(r *Registry, tags []*Tag, pretend bool) error {

	var safe []*Tag
	var del []*Tag
	for _, tag := range tags {

		if tag.old == false {
			safe = append(safe, tag)
			continue
		}

		if pretend {
			del = append(del, tag)
			continue
		}

		// not pretend - for real deleting stuff
		for _, img := range tag.ansestry {

			if img.del == false {
				continue
			}

			r.deleteImage(img)
		}
	}

	if pretend {
		for _, tag := range safe {
			fmt.Printf("+ %-32s %s\n", tag.name, tag.lastUpdateTime.Format(time.RFC822))
		}

		for _, tag := range del {
			fmt.Printf("- %-32s %s\n", tag.name, tag.lastUpdateTime.Format(time.RFC822))
		}
	}

	return nil
}

func (r *Registry) deleteImage(img *Image) error {

	err := os.RemoveAll(filepath.Join(r.root, "images", img.id))
	return err
}

// get an array of image ids that make up this tag
func getAncestry(tag Tag) ([]string, error) {

	f, err := os.Open(filepath.Join(tag.reg.root, "images", tag.image.String(), "ancestry"))
	if err != nil {
		return []string{}, err
	}

	var ansestry []string

	dec := json.NewDecoder(f)
	err = dec.Decode(&ansestry)
	if err != nil {
		return []string{}, err
	}

	return ansestry, err
}
