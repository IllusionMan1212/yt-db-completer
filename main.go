package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/urfave/cli/v3"
)

const YT_DB_CMP_VERSION = "0.0.1"

const (
	InvalidCategory Category = "invalid"
	All             Category = "all"
	Shorts          Category = "shorts"
	Video           Category = "video"
	Livestream      Category = "livestream"
	Membership      Category = "membership"
)

type Category string

type Metadata struct {
	Id            string `json:"id"`
	Title         string `json:"title"`
	Date          string `json:"upload_date"`
	PlaylistTitle string `json:"playlist_title"`
}

type CategoryFlag = cli.FlagBase[Category, cli.NoConfig, categoryValue]

type categoryValue struct {
	destination *Category
}

// Below functions are to satisfy the ValueCreator interface

func (s categoryValue) Create(val Category, p *Category, c cli.NoConfig) cli.Value {
	*p = val
	cat := Category(*p)
	return &categoryValue{
		destination: &cat,
	}
}

func (c categoryValue) ToString(val Category) string {
	if val == "" {
		return string(All)
	}
	return string(val)
}

func (c *categoryValue) String() string {
	return string(*c.destination)
}

func (c *categoryValue) Get() any { return *c.destination }

func (c *categoryValue) Set(value string) error {
	switch strings.ToLower(value) {
	case "all":
		*c.destination = All
	case "shorts":
		*c.destination = Shorts
	case "video":
		*c.destination = Video
	case "livestream":
		*c.destination = Livestream
	case "membership":
		*c.destination = Membership
	default:
		*c.destination = InvalidCategory
	}
	return nil
}

func getChannelID(handle string) string {
	resp, err := http.Get(fmt.Sprintf("https://www.youtube.com/%s", handle))
	if err != nil {
		log.Fatal(err)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}

	re := regexp.MustCompile(`<meta[^>]*property=["']og:url["'][^>]*content=["']([^"']+)["']`)

	results := re.FindAllStringSubmatch(string(body), -1)
	parsedUrl := results[0][1]

	splits := strings.Split(parsedUrl, "/")
	channelID := splits[len(splits)-1]

	return channelID
}

func getMembersPlaylistID(channelID string) string {
	return fmt.Sprintf("UUMO%s", channelID[2:])
}

func analyze(jsonPath, dirPath string, category Category, dumpToFile bool) error {
	existingIds := make([]string, 0, 128)
	duplicatedIds := make([]string, 0, 128)
	missingMetadata := make([]Metadata, 0, 128)
	extraIds := make([]string, 0, 128)
	allMetadata := make([]Metadata, 0, 1024)

	dir, dirErr := os.Open(dirPath)
	defer dir.Close()
	if dirErr != nil {
		return fmt.Errorf("error while opening provided directory: %v\n", dirErr)
	}

	file, fileErr := os.Open(jsonPath)
	defer file.Close()
	if fileErr != nil {
		return fmt.Errorf("error while opening provided json file: %v\n", fileErr)
	}

	stat, statErr := dir.Stat()
	if statErr != nil {
		return fmt.Errorf("error while stating the provided directory: %v\n", statErr)
	}
	if !stat.IsDir() {
		return fmt.Errorf("second argument is not a directory")
	}

	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		metadataJson := scanner.Text()

		var metadata Metadata
		json.Unmarshal([]byte(metadataJson), &metadata)

		switch category {
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
			if !strings.HasSuffix(metadata.PlaylistTitle, "Members-only videos") {
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
		return fmt.Errorf("error while reading directory contents: %v\n", dirErr)
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
		if slices.Contains(existingIds, id) {
			duplicatedIds = append(duplicatedIds, id)
		}
		existingIds = append(existingIds, id)
	}

	for _, meta := range allMetadata {
		if !slices.Contains(existingIds, meta.Id) {
			missingMetadata = append(missingMetadata, meta)
		}
	}

	for _, id := range existingIds {
		if !slices.ContainsFunc(allMetadata, func(meta Metadata) bool { return meta.Id == id }) {
			extraIds = append(extraIds, id)
		}
	}

	if len(missingMetadata) == 0 {
		fmt.Printf("Found %d IDs. No missing IDs. Congrats\n", len(existingIds))
	} else {
		fmt.Printf("Found %d IDs. Still missing %d IDs. For a total of %d IDs\n", len(existingIds), len(allMetadata)-(len(existingIds)-len(extraIds)), len(allMetadata))

		fmt.Println("Missing Videos are:")
		fmt.Println(missingMetadata)
	}

	if len(extraIds) != 0 {
		fmt.Println("Found extra existing videos:")
		fmt.Println(extraIds)
	}

	if len(duplicatedIds) != 0 {
		fmt.Println("Found duplicated videos:")
		fmt.Println(duplicatedIds)
	}

	if dumpToFile {
		file, fileErr := os.Create("./missing.txt")
		defer file.Close()
		if fileErr != nil {
			return fmt.Errorf("error while opening dump file: %v\n", fileErr)
		}

		for _, meta := range missingMetadata {
			file.WriteString(meta.Id + "\n")
		}
	}

	return nil
}

func format(dirPath string, dryRun bool) error {
	dir, dirErr := os.Open(dirPath)
	defer dir.Close()
	if dirErr != nil {
		return fmt.Errorf("error while opening provided directory: %v\n", dirErr)
	}

	stat, statErr := dir.Stat()
	if statErr != nil {
		return fmt.Errorf("error while stating the provided directory: %v\n", statErr)
	}
	if !stat.IsDir() {
		return fmt.Errorf("argument is not a directory")
	}

	entries, dirErr := dir.ReadDir(0)
	if dirErr != nil {
		return fmt.Errorf("error while reading directory contents: %v\n", dirErr)
	}

	groupedFiles := make(map[string][]string, 0)

	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "archive.txt" {
			continue
		}

		base := filepath.Base(entry.Name())
		ext := filepath.Ext(base)
		nameWithoutExt := base[:len(base)-len(ext)]

		// Remove the extension twice because json files can have up to 2 extensions e.g. .tar.gz, .info.json, etc...
		// HACK: is there a better way of doing this??
		if ext == ".json" || ext == ".vtt" {
			nameWithoutExt = nameWithoutExt[:len(nameWithoutExt)-len(filepath.Ext(nameWithoutExt))]
		}

		groupedFiles[nameWithoutExt] = append(groupedFiles[nameWithoutExt], entry.Name())
	}

	if dryRun {
		fmt.Printf("Found %v Groups\n", len(groupedFiles))
		for name := range groupedFiles {
			fmt.Println(name)
		}
	} else {
		for name, group := range groupedFiles {
			err := os.Mkdir(filepath.Join(dirPath, name), 0o755)
			if err != nil {
				return err
			}

			for _, file := range group {
				// A bit dangerous because `Rename` may overwrite files if newpath already exists.
				err = os.Rename(filepath.Join(dirPath, file), filepath.Join(dirPath, name, file))
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func main() {
	cmd := &cli.Command{
		Name: "yt-db-completer",
		// TODO: better description
		Usage:           "A program to help you categorize, sort, format, and archive youtube channels",
		HideHelpCommand: true,
		Commands: []*cli.Command{
			{
				Name:  "analyze",
				Usage: "analyzes a yt-dlp json file",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:        "dump-to-file",
						Usage:       "dump the missing IDs to a file that can be used with yt-dlp",
						DefaultText: "false",
					},
					&CategoryFlag{
						Name:        "category",
						Usage:       "category of videos to check against. Valid options are \"all\", \"shorts\", \"video\", \"livestream\", \"membership\"",
						DefaultText: "all",
						Action: func(ctx context.Context, c *cli.Command, s Category) error {
							if s == "" {
								return nil
							}

							if s == InvalidCategory {
								return cli.Exit("Invalid category. Valid categories are \"all\", \"shorts\", \"video\", \"livestream\", \"membership\"", 1)
							}

							return nil
						},
					},
				},
				ArgsUsage: "<json file> <directory>",
				Arguments: []cli.Argument{
					&cli.StringArg{
						Name:   "json file",
						Config: cli.StringConfig{TrimSpace: true},
					},
					&cli.StringArg{
						Name:   "directory",
						Config: cli.StringConfig{TrimSpace: true},
					},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					// if --category is missing, assume "all".
					category := All

					if v, ok := c.Value("category").(Category); ok && v != "" {
						category = v
					}
					dumpToFile := c.Bool("dump-to-file")

					jsonPath := c.StringArg("json file")
					dir := c.StringArg("directory")

					if jsonPath == "" {
						return cli.Exit("Please provide a <json file> dumped from yt-dlp as the first argument", 1)
					}

					if dir == "" {
						return cli.Exit("Please provide a <directory> with folder names formatted like this \"[YYYYMMDD] [XXXXXXXXXXXX] ...\" where X is the video ID as the second argument", 1)
					}

					return analyze(jsonPath, dir, category, dumpToFile)
				},
			},
			{
				Name:      "format",
				Usage:     "collect files in <dir> with the same name but a different extension into a directory with that name",
				ArgsUsage: "<dir>",
				HideHelp:  true,
				Arguments: []cli.Argument{
					&cli.StringArg{
						Name:   "dir",
						Config: cli.StringConfig{TrimSpace: true},
					},
				},
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:        "dry-run",
						Usage:       "prints the files that will be moved and their count",
						DefaultText: "false",
					},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					dir := c.StringArg("dir")
					dryRun := c.Bool("dry-run")

					if dir == "" {
						return cli.Exit("<dir> is required", 1)
					}

					return format(dir, dryRun)
				},
			},
			{
				Name:      "membership-id",
				Usage:     "prints the provided channel's membership playlist id",
				ArgsUsage: "<username>",
				HideHelp:  true,
				Arguments: []cli.Argument{
					&cli.StringArg{
						Name:   "username",
						Config: cli.StringConfig{TrimSpace: true},
					},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					username := c.StringArg("username")

					if username == "" {
						return cli.Exit("<username> is required", 1)
					}

					channelId := getChannelID(username)
					playlistId := getMembersPlaylistID(channelId)
					fmt.Printf("Channel ID for %v is: %v\n", username, channelId)
					fmt.Printf("Membership playlist ID is: %v\n", playlistId)

					return nil
				},
			},
			{
				Name:     "version",
				Usage:    "print program version and exit",
				HideHelp: true,
				Action: func(ctx context.Context, c *cli.Command) error {
					fmt.Println(YT_DB_CMP_VERSION)
					return nil
				},
			},
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}
