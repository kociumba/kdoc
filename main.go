// this is the main implementation for kdoc
//
// it is a "dumb" code documentation generator, this means that kdoc unlike generators
// such as doxygen or godoc does not actually understand the code it generates the docs for
//
// this allows it to be very simple, quite fast and mostly language agnostic
//
// the main use case is meant for c/c++ but any language with comments will work with it
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/kociumba/kdoc/config"
	"github.com/kociumba/kdoc/git"
	"github.com/kociumba/kdoc/parser"
	"github.com/urfave/cli/v3"
)

func If[T any](condition bool, true_val, false_val T) T {
	if condition {
		return true_val
	}

	return false_val
}

var out, root string

func matchesExclude(path string, excludes []string) bool {
	for _, exc := range excludes {
		if matched, _ := doublestar.Match(exc, path); matched {
			return true
		}
	}

	return false
}

func collectFiles(scan_root string, excludes []string, ext_to_lang map[string]string) []string {
	var files []string
	err := filepath.WalkDir(scan_root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(scan_root, path)
		if err != nil {
			log.Printf("Error getting relative path for %s: %v", path, err)
			return nil
		}

		normPath := filepath.ToSlash(relPath)

		if matchesExclude(normPath, excludes) {
			if d.IsDir() {
				return filepath.SkipDir
			}

			return nil
		}

		if d.IsDir() {
			return nil
		}

		ext := filepath.Ext(path)
		if _, ok := ext_to_lang[ext]; ok {
			files = append(files, path)
		}

		return nil
	})
	if err != nil {
		log.Printf("Walk error: %v", err)
	}

	return files
}

func outputFilename(scan_root, file_path, out_path, ext string) string {
	rel, _ := filepath.Rel(scan_root, file_path)
	rel_no_ext := strings.TrimSuffix(rel, ext)
	out_rel := filepath.Join(out_path, rel_no_ext+".md")
	return filepath.ToSlash(out_rel)
}

func main() {
	cmd := &cli.Command{
		Name:  "kdoc",
		Usage: "addon to klarity, for generating docs directly from source code",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "output",
				Aliases: []string{"o"},
				Usage:   "where should kdoc output the generated docs",
				Value:   config.CFG.OutputPath,
			},
			&cli.StringFlag{
				Name:    "root",
				Aliases: []string{"r"},
				Usage:   "where is the kdoc.toml config file located",
			},
			&cli.BoolFlag{
				Name:    "recurse_scan",
				Aliases: []string{"rs"},
				Usage:   "allows kdoc to also scan it's output directory for files to document",
			},
			&cli.BoolFlag{
				Name:    "no-git",
				Aliases: []string{"ng"},
				Usage:   "disable git metadata collection, and embedding",
			},
		},
		Before: func(ctx context.Context, c *cli.Command) (context.Context, error) {
			out = filepath.Clean(c.String("output"))

			var config_path string
			if len(c.String("root")) != 0 {
				root = filepath.Clean(c.String("root"))
				config_path = filepath.Join(root, "kdoc.toml")
			} else {
				var err error
				root, err = os.Getwd()
				if err != nil {
					return ctx, err
				}
				config_path = filepath.Join(root, "kdoc.toml")
			}

			if err := config.Load(config_path); err != nil {
				return ctx, err
			}

			if err := os.MkdirAll(out, 0755); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			p := parser.Parser{
				Files:        []parser.File{},
				ElementIndex: make(map[string]string),
			}

			wd, err := os.Getwd()
			if err != nil {
				return err
			}

			scan_root := filepath.Clean(If(len(config.CFG.ScanRoot) == 0, wd, config.CFG.ScanRoot))
			scan_root = filepath.ToSlash(scan_root)
			scan_root, err = filepath.Abs(scan_root)
			if err != nil {
				return err
			}

			enableGit := !c.Bool("no-git")
			if enableGit {
				p.RepoInfo = git.GetRepoInfo(scan_root)
				if p.RepoInfo.IsRepo {
					log.Printf("Git repository detected: %s/%s", p.RepoInfo.RepoOwner, p.RepoInfo.RepoName)
				} else {
					log.Printf("Not a git repository, skipping git metadata")
					enableGit = false
				}
			}

			scan_excludes := []string{}
			config.CFG.ScanExclusions = append(config.CFG.ScanExclusions, filepath.Join(scan_root, "kdoc.toml"))
			if !c.Bool("recurse_scan") {
				config.CFG.ScanExclusions = append(config.CFG.ScanExclusions, config.CFG.OutputPath)
			}

			for _, pattern := range config.CFG.ScanExclusions {
				scan_excludes = append(scan_excludes, filepath.ToSlash(pattern))
			}

			matchedFiles := collectFiles(scan_root, scan_excludes, config.CFG.ExtensionsToLangs)
			if len(matchedFiles) == 0 {
				return fmt.Errorf("no files matched extensions in %s\nConfigured extensions: %v", scan_root, config.CFG.ExtensionsToLangs)
			}

			for _, filePath := range matchedFiles {
				ext := filepath.Ext(filePath)
				lang, ok := config.CFG.ExtensionsToLangs[ext]
				if !ok {
					continue
				}

				var f parser.File
				f.Language = lang
				if err := parser.ParseFile(filePath, &f, config.CFG.DocComment, config.CFG.IgnoreIndented); err != nil {
					log.Printf("Error parsing %s: %v", filePath, err)
					continue
				}

				if enableGit && p.RepoInfo.IsRepo {
					relPath, err := filepath.Rel(scan_root, filePath)
					if err == nil {
						gitInfo, err := git.GetFileInfo(scan_root, relPath)
						if err != nil {
							log.Printf("Warning: Could not get git info for %s: %v", filePath, err)
						} else {
							f.GitInfo = gitInfo
						}
					}
				}

				p.Files = append(p.Files, f)
			}

			linkIndex := make(map[string]string)
			for _, f := range p.Files {
				outFile := outputFilename(scan_root, f.Path, out, filepath.Ext(f.Path))
				if f.ModuleDesc != "" {
				}

				for _, e := range f.Elements {
					headerID := strings.ToLower(strings.ReplaceAll(e.ID, " ", "-"))
					linkIndex[e.ID] = fmt.Sprintf("%s#%s", filepath.Base(outFile), headerID)
				}
			}

			for i := range p.Files {
				p.Files[i].ModuleDesc = parser.ProcessBacklinks(p.Files[i].ModuleDesc, linkIndex, config.CFG.DocComment)
				for j := range p.Files[i].Elements {
					p.Files[i].Elements[j].Description = parser.ProcessBacklinks(p.Files[i].Elements[j].Description, linkIndex, config.CFG.DocComment)
				}
			}

			for _, f := range p.Files {
				outFile := outputFilename(scan_root, f.Path, out, filepath.Ext(f.Path))
				outDir := filepath.Dir(outFile)
				if err := os.MkdirAll(outDir, 0755); err != nil {
					log.Printf("Error creating dir %s: %v", outDir, err)
					continue
				}

				mdContent := p.GenerateMarkdownForFile(&f)
				if err := os.WriteFile(outFile, []byte(mdContent), 0644); err != nil {
					log.Printf("Error writing %s: %v", outFile, err)
				}
			}

			return nil
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}
