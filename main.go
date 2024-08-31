package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
)

const (
	All        Category = "all"
	Shorts              = "shorts"
	Video               = "video"
	Livestream          = "livestream"
	// TODO: Can't test this until I get my hands on a membership.
	Membership          = "membership"
)

type Category string

type Metadata struct {
	Id            string `json:"id"`
	Title         string `json:"title"`
	Date          string `json:"upload_date"`
	PlaylistTitle string `json:"playlist_title"`
}

func containsStr(s []string, id string) bool {
	for _, x := range s {
		if x == id {
			return true
		}
	}

	return false
}

func containsMeta(s []Metadata, id string) bool {
	for _, x := range s {
		if x.Id == id {
			return true
		}
	}

	return false
}

func (c *Category) String() string {
	return string(*c)
}

func (c *Category) Set(value string) error {
	switch strings.ToLower(value) {
	case "all":
		*c = All
	case "shorts":
		*c = Shorts
	case "video":
		*c = Video
	case "livestream":
		*c = Livestream
	case "membership":
		*c = Membership
	default:
		panic(fmt.Sprintf("Invalid category: %s", value))
	}
	return nil
}

func CategoryFlag(name string, value Category, usage string) *Category {
	c := value
	flag.Var(&c, name, usage)
	return &c
}

func main() {
	cat := CategoryFlag("category", All, "Category of videos to check against.\nValid options are all, shorts, video, livestream, membership")
	dumpToFile := flag.Bool("dump-to-file", false, "The program will dump the missing IDs to a file that can be used with yt-dlp")

	flag.Parse()

	args := flag.Args()

	if len(args) < 1 {
		fmt.Println("Please provide a json file dumped from yt-dlp as the first argument")
		return
	}

	if len(args) < 2 {
		fmt.Println("Please provide a directory with folder names formatted like this \"[YYYYMMDD] [XXXXXXXXXXXX] ...\" where X is the video ID")
		return
	}

	existingIds := make([]string, 0, 128)
	missingMetadata := make([]Metadata, 0, 128)
	extraIds := make([]string, 0, 128)
	allMetadata := make([]Metadata, 0, 1024)

	filePath := args[0]
	dirPath := args[1]

	dir, dirErr := os.Open(dirPath)
	defer dir.Close()
	if dirErr != nil {
		fmt.Printf("Error while opening provided directory: %v\n", dirErr)
		return
	}

	file, fileErr := os.Open(filePath)
	defer file.Close()
	if fileErr != nil {
		fmt.Printf("Error while opening provided json file: %v\n", fileErr)
		return
	}

	stat, statErr := dir.Stat()
	if statErr != nil {
		fmt.Printf("Error while stating the provided directory: %v\n", statErr)
		return
	}
	if !stat.IsDir() {
		fmt.Println("Second argument is not a directory")
		return
	}

	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		metadataJson := scanner.Text()

		var metadata Metadata
		json.Unmarshal([]byte(metadataJson), &metadata)

		switch *cat {
		case All: // Do nothing
		case Shorts:
			if !strings.HasSuffix(metadata.PlaylistTitle, "Shorts") {
				continue
			}
		case Video:
			if !strings.HasSuffix(metadata.PlaylistTitle, "Videos") {
				continue
			}
		case Livestream:
			if !strings.HasSuffix(metadata.PlaylistTitle, "Live") {
				continue
			}
		case Membership:
			if !strings.HasSuffix(metadata.PlaylistTitle, "Membership") {
				continue
			}
		}

		allMetadata = append(allMetadata, metadata)
	}

	sort.Slice(allMetadata, func(a, b int) bool {
		return allMetadata[a].Date < allMetadata[b].Date
	})

	entries, dirErr := dir.ReadDir(0)
	if dirErr != nil {
		fmt.Printf("Error while reading directory contents: %v\n", dirErr)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// TODO: make sure all the directories are formatted properly by looking at the id and making sure
		// it looks like a youtube video id.
		// Warn the user if a directory doesn't conform to the id format.

		name := entry.Name()
		fields := strings.Fields(name)
		id := strings.Trim(strings.Trim(fields[1], "["), "]")
		existingIds = append(existingIds, id)
	}

	for _, meta := range allMetadata {
		if !containsStr(existingIds, meta.Id) {
			missingMetadata = append(missingMetadata, meta)
		}
	}

	for _, id := range existingIds {
		if !containsMeta(allMetadata, id) {
			extraIds = append(extraIds, id)
		}
	}

	if len(missingMetadata) == 0 {
		fmt.Printf("Found %d IDs. No missing IDs. Congrats\n", len(existingIds))
	} else {
		fmt.Printf("Found %d IDs. Still missing %d IDs. For a total of %d IDs\n", len(existingIds), len(allMetadata)-(len(existingIds) - len(extraIds)), len(allMetadata))

		fmt.Println("Missing Videos are:")
		fmt.Println(missingMetadata)
	}

	if len(extraIds) != 0 {
		fmt.Println("Found extra existing videos:")
		fmt.Println(extraIds)
	}

	if *dumpToFile {
		file, fileErr := os.Create("./missing.txt")
		defer file.Close()
		if fileErr != nil {
			fmt.Printf("Error while opening dump file: %v\n", fileErr)
			return
		}

		for _, meta := range missingMetadata {
			file.WriteString(meta.Id + "\n")
		}
	}
}
