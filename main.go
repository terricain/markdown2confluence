package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/alecthomas/kong"
	"github.com/cseeger-epages/confluence-go-api"
	"github.com/ericaro/frontmatter"
	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/ast"
	"github.com/gomarkdown/markdown/html"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type FrontMatterStruct struct {
	Space       string
	PageTitle   string `yaml:"page_title"`
	ParentID    string `yaml:"parent_id"`
	ParentTitle string `yaml:"parent_title"`
	Content     string `fm:"content" yaml:"-"`
	ContentSHA  string
}

var CLI struct {
	LogLevel string `env:"LOG_LEVEL" enum:"DEBUG,INFO,WARNING,ERROR" default:"INFO" help:"Logger level"`
	BaseURL         string   `required:"" env:"CONFLUENCE_BASE_URL" help:"Confluence base URL"`
	User            string   `required:"" env:"CONFLUENCE_USER" help:"Confluence username"`
	Password        string   `required:"" env:"CONFLUENCE_PASSWORD" help:"Confluence password or API token"`
	DefaultSpace    string   `env:"CONFLUENCE_DEFAULT_SPACE" help:"Default space to use when uploading markdown documents"`
	DefaultAncestor string   `env:"CONFLUENCE_DEFAULT_ANCESTOR" help:"Default ancestor to upload documents under, is expected to be a page ID"`
	Recursive       bool     `env:"CONFLUENCE_RECURSIVE" type:"bool" help:""`
	Paths           []string `arg:"" name:"path" env:"CONFLUENCE_FILEPATH" default:"." type:"path" help:"Paths to upload to confluence"`
}

var LogLevelMap = map[string]zerolog.Level{
	"DEBUG": zerolog.DebugLevel,
	"INFO": zerolog.InfoLevel,
	"WARNING": zerolog.WarnLevel,
	"ERROR": zerolog.ErrorLevel,
}

const MACRO_XML_START = `<ac:structured-macro ac:name="code">`
const MACRO_XML_LANGUAGE = `<ac:parameter ac:name="language">LANGUAGE</ac:parameter>`
const MACRO_XML_BODY = `<ac:plain-text-body><![CDATA[BODY]]></ac:plain-text-body>`
const MACRO_XML_STOP = `</ac:structured-macro>`


func main() {
	kong.Parse(&CLI)
	logLevel, _ := LogLevelMap[CLI.LogLevel]
	zerolog.SetGlobalLevel(logLevel)

	log.Info().Msg("Starting markdown2confluence")

	files := make([]string, 0)

	for _, item := range CLI.Paths {
		newFiles, err := findFiles(item, CLI.Recursive)
		if err != nil {
			fmt.Printf("Got err: %s", err.Error())
			os.Exit(1)
		}
		files = append(files, newFiles...)
	}

	api, err := goconfluence.NewAPI(CLI.BaseURL+"/wiki/rest/api", CLI.User, CLI.Password)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create API client")
	}

	success := true

	// Set up markdown renderer for later use
	opts := html.RendererOptions{
		Flags: html.CommonFlags,
		RenderNodeHook: renderHookDropCodeBlock,
	}
	renderer := html.NewRenderer(opts)

	// Start uploading files
	for _, file := range files {
		log.Debug().Msgf("Processing %s", file)

		data, err := ioutil.ReadFile(file)
		if err != nil {
			log.Error().Err(err).Msgf("Failed to read contents of %s, skipping", file)
			success = false
			continue
		}

		// Parse the frontmatter
		frontmatterPass := FrontMatterStruct{}
		err = frontmatter.Unmarshal(data, &frontmatterPass)
		if err != nil {
			log.Error().Err(err).Msgf("Failed to process the frontmatter of %s, skipping", file)
			success = false
			continue
		}
		h := sha256.New()
		_, err = h.Write([]byte(frontmatterPass.Content))
		if err != nil {
			log.Error().Err(err).Msgf("Failed to hash the contents of %s, skipping", file)
			success = false
			continue
		}
		frontmatterPass.ContentSHA = "sha-" + hex.EncodeToString(h.Sum(nil))[0:8]

		// Merge in defaults if we need them
		if len(frontmatterPass.Space) == 0 {
			if len(CLI.DefaultSpace) == 0 {
				log.Error().Msgf("Missing space or default space for file %s", file)
				success = false
				continue
			} else {
				frontmatterPass.Space = CLI.DefaultSpace
			}
		}

		if len(frontmatterPass.ParentID) == 0 {
			if len(frontmatterPass.ParentTitle) != 0 {
				// Get page id of parent if title is provided
				page, _, err := GetPageFromName(api, frontmatterPass.Space, frontmatterPass.ParentTitle)
				if err != nil {
					log.Error().Err(err).Msgf("Got error looking up page id for file %s", file)
					success = false
					continue
				}
				frontmatterPass.ParentID = page.ID

			} else if len(CLI.DefaultAncestor) == 0 {
				log.Error().Msgf("Missing parent id/title or default ancestor for file %s", file)
				success = false
				continue
			} else {
				frontmatterPass.ParentID = CLI.DefaultAncestor
			}
		}

		if len(frontmatterPass.PageTitle) == 0 {
			log.Error().Msgf("Frontmatter missing page title for file %s", file)
			success = false
			continue
		}

		// Check if the page exists
		page, found, err := GetPageFromName(api, frontmatterPass.Space, frontmatterPass.PageTitle)

		// TODO customise html
		htmlData := markdown.ToHTML([]byte(frontmatterPass.Content), nil, renderer)

		if found && err == nil {
			// get page version
			version, err := GetPageVersion(api, page.ID)
			if err != nil {
				fmt.Printf("Got error getting page hash: %s\n", err.Error())
				success = false
				continue
			}

			// update page
			pageHashLabel, err := GetHashFromLabels(api, page.ID)
			if err != nil {
				log.Error().Err(err).Msgf("Got error looking up page labels for file %s", file)
				success = false
				continue
			}

			if pageHashLabel.Name == frontmatterPass.ContentSHA {
				log.Info().Msgf("No update needed for %s", file)
				continue
			}

			// Have update, so need to remove label, update, add new label
			if len(pageHashLabel.Name) != 0 {
				if _, err := api.DeleteLabel(page.ID, pageHashLabel.Name); err != nil {
					log.Error().Err(err).Msgf("Got error removing page label for file %s", file)
					success = false
					continue
				}
			}

			pageContent := goconfluence.Content{
				ID:    page.ID,
				Title: frontmatterPass.PageTitle,
				Version: goconfluence.Version{
					Number: version + 1,
				},
				Type:   "page",
				Space:  goconfluence.Space{Key: frontmatterPass.Space},
				Status: "current",
				Ancestors: []goconfluence.Ancestor{
					{ID: frontmatterPass.ParentID},
				},
				Body: goconfluence.Body{
					Storage: goconfluence.Storage{
						Value:          string(htmlData),
						Representation: "storage",
					},
				},
			}

			if _, err := api.UpdateContent(&pageContent); err != nil {
				log.Error().Err(err).Msgf("Failed to update page content for file %s", file)
				success = false
				continue
			}
			log.Info().Msgf("Updated page successfully for %s", file)

			labels := []goconfluence.Label{
				{Name: frontmatterPass.ContentSHA},
			}
			if _, err := api.AddLabels(page.ID, &labels); err != nil {
				log.Error().Err(err).Msgf("Failed to update page labels for file %s", file)
				success = false
				continue
			}

		} else {
			// Create page
			pageContent := goconfluence.Content{
				Title:  frontmatterPass.PageTitle,
				Type:   "page",
				Space:  goconfluence.Space{Key: frontmatterPass.Space},
				Status: "current",
				Ancestors: []goconfluence.Ancestor{
					{ID: frontmatterPass.ParentID},
				},
				Body: goconfluence.Body{
					Storage: goconfluence.Storage{
						Value:          string(htmlData),
						Representation: "storage",
					},
				},
			}

			newPage, err := api.CreateContent(&pageContent)
			if err != nil {
				log.Error().Err(err).Msgf("Failed to create page for file %s", file)
				success = false
				continue
			}
			log.Info().Msgf("Created page successfully for %s", file)

			labels := []goconfluence.Label{
				{Name: frontmatterPass.ContentSHA},
			}
			if _, err := api.AddLabels(newPage.ID, &labels); err != nil {
				log.Error().Err(err).Msgf("Failed to update page labels for file %s", file)
				success = false
				continue
			}
		}
	}

	if !success {
		os.Exit(1)
	}
}

func GetPageFromName(api *goconfluence.API, space, pageName string) (goconfluence.Content, bool, error) {
	contentSearch, err := api.GetContent(goconfluence.ContentQuery{Title: pageName, SpaceKey: space})
	if err != nil {
		return goconfluence.Content{}, false, err
	}

	if contentSearch.Size == 0 {
		return goconfluence.Content{}, false, nil
	}

	return contentSearch.Results[0], true, nil
}

func GetPageVersion(api *goconfluence.API, pageId string) (int, error) {
	content, err := api.GetContentByID(pageId, goconfluence.ContentQuery{})
	if err != nil {
		return 0, err
	}

	return content.Version.Number, err
}

func GetHashFromLabels(api *goconfluence.API, pageID string) (goconfluence.Label, error) {
	labels, err := api.GetLabels(pageID)
	if err != nil {
		return goconfluence.Label{}, err
	}
	for _, label := range labels.Labels {
		if strings.HasPrefix(label.Name, "sha-") {
			return label, nil
		}
	}
	return goconfluence.Label{}, nil
}

func findFiles(searchpath string, recursive bool) ([]string, error) {
	fi, err := os.Stat(searchpath)
	if err != nil {
		return []string{}, err
	}

	result := make([]string, 0)

	switch mode := fi.Mode(); {
	case mode.IsDir():
		if recursive {

			err = filepath.Walk(searchpath, func(walkedpath string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if !info.IsDir() && strings.HasSuffix(strings.ToLower(info.Name()), ".md") {
					result = append(result, walkedpath)
				}
				return nil
			})
			if err != nil {
				return []string{}, err
			}

		} else {
			files, err := ioutil.ReadDir(searchpath)
			if err != nil {
				return []string{}, err
			}

			for _, f := range files {
				if !f.IsDir() && strings.HasSuffix(strings.ToLower(f.Name()), ".md") {
					result = append(result, path.Join(searchpath, f.Name()))
				}
			}
		}

	case mode.IsRegular():
		result = append(result, searchpath)
	}

	return result, nil
}


func renderHookDropCodeBlock(w io.Writer, node ast.Node, entering bool) (ast.WalkStatus, bool) {
	if _, ok := node.(*ast.CodeBlock); ok {
		codeBlock := node.(*ast.CodeBlock)
		parts := make([]string, 5)
		parts = append(parts, MACRO_XML_START)

		if len(codeBlock.Info) > 0 {
			parts = append(parts, strings.Replace(MACRO_XML_LANGUAGE, "LANGUAGE", string(codeBlock.Info), 1))
		}
		parts = append(parts, strings.Replace(MACRO_XML_BODY, "BODY", string(codeBlock.Literal), 1))
		parts = append(parts, MACRO_XML_STOP)

		_, _ = io.WriteString(w, strings.Join(parts, "\n"))

		return ast.GoToNext, true
	}

	return ast.GoToNext, false
}
