// This is the main implementation for kdoc
// > this comment is mainly here for testing the tool itself
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

var config_path string

func initState(create_out bool) func(ctx context.Context, c *cli.Command) (context.Context, error) {
	return func(ctx context.Context, c *cli.Command) (context.Context, error) {
		out = filepath.Clean(c.String("output"))

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

		if create_out {
			if err := os.MkdirAll(out, 0755); err != nil {
				return ctx, err
			}
		}

		return ctx, nil
	}
}

var cmds = []*cli.Command{
	{
		Name:   "init",
		Usage:  "initialize the default config, by default uses the working dir, use --root to overwirite",
		Before: initState(false),
		Action: func(ctx context.Context, c *cli.Command) error {
			if err := config.Load(config_path); err != nil {
				return err
			}

			fmt.Printf("kdoc initialized succesfully in %s\n\nedit this file to configure kdoc\nor run 'kdoc generate' to build docs\n", config_path)

			return nil
		},
	},
	{
		Name:    "generate",
		Aliases: []string{"gen"},
		Usage:   "uses the defined config values to generate markdown docs from found source files",
		Before:  initState(true),
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
					fmt.Printf("Git repository detected: %s/%s\n", p.RepoInfo.RepoOwner, p.RepoInfo.RepoName)
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
			totalFiles := len(matchedFiles)
			if totalFiles == 0 {
				return fmt.Errorf("no files matched extensions in %s\nConfigured extensions: %v", scan_root, config.CFG.ExtensionsToLangs)
			}

			for i, filePath := range matchedFiles {
				ext := filepath.Ext(filePath)
				lang, ok := config.CFG.ExtensionsToLangs[ext]
				if !ok {
					continue
				}

				displayPath, err := filepath.Rel(scan_root, filePath)
				if err != nil {
					displayPath = filePath
				}
				displayPath = filepath.ToSlash(displayPath)

				fmt.Printf("\x1b[2K\r[%d/%d] Processing: %s", i+1, totalFiles, displayPath)

				var f parser.File
				f.Language = lang
				if err := parser.ParseFile(filePath, &f, config.CFG.DocComment, config.CFG.IgnoreIndented); err != nil {
					log.Printf("Error parsing %s: %v", filePath, err)
					continue
				}

				var relPath string
				if p.RepoInfo != nil && p.RepoInfo.IsRepo && p.RepoInfo.GitRoot != "" {
					relPath, err = filepath.Rel(p.RepoInfo.GitRoot, filePath)
					if err != nil {
						log.Printf("Warning: failed to get relative path from git root: %v", err)
						relPath, _ = filepath.Rel(scan_root, filePath)
					}
				} else {
					relPath, _ = filepath.Rel(scan_root, filePath)
				}

				relPath = filepath.ToSlash(relPath)

				if p.RepoInfo != nil && p.RepoInfo.IsRepo && p.RepoInfo.GitRoot != "" {
					gitInfo, err := git.GetFileInfo(p.RepoInfo.GitRoot, relPath)
					if err != nil {
						log.Printf("Warning: Could not get git info for %s: %v", filePath, err)
					} else {
						f.GitInfo = gitInfo
					}
				}

				p.Files = append(p.Files, f)
			}

			fmt.Printf("\x1b[2K\r[%d/%d] Processing complete\n", totalFiles, totalFiles)

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

			write_range := len(p.Files)
			for i, f := range p.Files {
				outFile := outputFilename(scan_root, f.Path, out, filepath.Ext(f.Path))
				outDir := filepath.Dir(outFile)
				if err := os.MkdirAll(outDir, 0755); err != nil {
					log.Printf("Error creating dir %s: %v", outDir, err)
					continue
				}

				fmt.Printf("\x1b[2K\r[%d/%d] Writing: %s", i+1, write_range, outFile)

				mdContent := p.GenerateMarkdownForFile(&f)
				if err := os.WriteFile(outFile, []byte(mdContent), 0644); err != nil {
					log.Printf("Error writing %s: %v", outFile, err)
				}
			}

			fmt.Printf("\x1b[2K\r[%d/%d] Writing docs complete\n", write_range, write_range)

			return nil
		},
	},
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
				Aliases: []string{"s"},
				Usage:   "allows kdoc to also scan it's output directory for files to document",
			},
			&cli.BoolFlag{
				Name:    "no-git",
				Aliases: []string{"g"},
				Usage:   "disable git metadata collection, and embedding",
			},
		},
		Commands: cmds,
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}
