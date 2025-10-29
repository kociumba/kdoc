package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kociumba/kdoc/git"
)

type Parser struct {
	Files        []File
	ElementIndex map[string]string
	RepoInfo     *git.RepoInfo
}

type File struct {
	Language   string
	Path       string
	ModuleDesc string
	Elements   []Element
	GitInfo    *git.FileInfo
}

type Element struct {
	ID          string
	Description string
	Signature   string
}

func ParseFile(filePath string, f *File, docPrefix string, ignoreIndented bool) error {
	f.Path = filePath
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")
	f.ModuleDesc, lines = extractTopComment(lines, docPrefix, ignoreIndented)
	f.Elements, _ = extractElements(lines, docPrefix, ignoreIndented)

	return nil
}

func extractTopComment(lines []string, prefix string, ignoreIndented bool) (string, []string) {
	var desc []string
	i := 0
	prefixLen := len(prefix)

	for i < len(lines) {
		line := lines[i]
		trimmedLine := strings.TrimSpace(line)

		if !strings.HasPrefix(trimmedLine, prefix) {
			break
		}

		if ignoreIndented {
			prefixPos := strings.Index(line, prefix)
			if prefixPos > 0 && strings.TrimSpace(line[:prefixPos]) == "" {
				i++
				continue
			}
		}

		content := trimmedLine[prefixLen:]
		if len(content) > 0 && content[0] == ' ' {
			content = content[1:]
		}

		desc = append(desc, content)
		i++
	}

	return strings.Join(desc, "\n"), lines[i:]
}

func extractElements(lines []string, prefix string, ignoreIndented bool) ([]Element, []string) {
	var elements []Element
	i := 0
	prefixLen := len(prefix)

	for i < len(lines) {
		line := lines[i]
		trimmedLine := strings.TrimSpace(line)

		if !strings.HasPrefix(trimmedLine, prefix) {
			i++
			continue
		}

		if ignoreIndented {
			prefixPos := strings.Index(line, prefix)
			if prefixPos > 0 && strings.TrimSpace(line[:prefixPos]) == "" {
				i++
				continue
			}
		}

		var desc []string
		for i < len(lines) {
			line := lines[i]
			trimmedLine := strings.TrimSpace(line)

			if !strings.HasPrefix(trimmedLine, prefix) {
				break
			}

			content := trimmedLine[prefixLen:]
			if len(content) > 0 && content[0] == ' ' {
				content = content[1:]
			}

			desc = append(desc, content)
			i++
		}

		if len(desc) == 0 {
			continue
		}

		descMD := strings.Join(desc, "\n")

		for i < len(lines) && (strings.TrimSpace(lines[i]) == "" || strings.HasPrefix(strings.TrimSpace(lines[i]), prefix)) {
			i++
		}

		sig := ""
		if i < len(lines) && strings.TrimSpace(lines[i]) != "" {
			sig = strings.TrimSpace(lines[i])
			sig = regexp.MustCompile(`\s*{$`).ReplaceAllString(sig, "")
			i++
		}

		id := extractIDFromSig(sig)
		if id == "" {
			id = fmt.Sprintf("unnamed_%d", len(elements))
		}

		elements = append(elements, Element{
			ID:          id,
			Description: descMD,
			Signature:   sig,
		})
	}

	return elements, lines[i:]
}

func extractIDFromSig(sig string) string {
	classRe := regexp.MustCompile(`^(?:class|struct)\s+(\w+)`)
	if matches := classRe.FindStringSubmatch(sig); len(matches) > 1 {
		return matches[1]
	}

	funcRe := regexp.MustCompile(`(\w+)\s*\(`)
	if matches := funcRe.FindStringSubmatch(sig); len(matches) > 1 {
		return matches[1]
	}

	words := strings.Fields(sig)
	if len(words) > 0 {
		return words[0]
	}

	return ""
}

func ProcessBacklinks(desc string, linkIndex map[string]string, prefix string) string {
	re := regexp.MustCompile(`\[([^\]]+)\]`)
	return re.ReplaceAllStringFunc(desc, func(match string) string {
		submatch := re.FindStringSubmatch(match)
		if len(submatch) == 2 {
			targetID := submatch[1]
			if link, ok := linkIndex[targetID]; ok {
				return fmt.Sprintf("[%s](%s)", targetID, link)
			}
		}

		return match
	})
}

func (p *Parser) GenerateMarkdownForFile(f *File) string {
	var sb strings.Builder
	base := filepath.Base(f.Path)
	sb.WriteString(fmt.Sprintf("# %s\n\n", base))

	if f.GitInfo != nil && p.RepoInfo != nil && p.RepoInfo.IsRepo {
		sb.WriteString(p.generateGitMetadata(f))
	}

	if f.ModuleDesc != "" {
		sb.WriteString(f.ModuleDesc + "\n\n")
	}

	if len(f.Elements) > 0 {
		sb.WriteString("## Table of Contents\n\n")
		for _, e := range f.Elements {
			anchor := strings.ToLower(strings.ReplaceAll(e.ID, " ", "-"))
			linkText := e.ID
			if e.Signature != "" {
				sig := strings.SplitN(strings.TrimSpace(e.Signature), "\n", 2)[0]
				sig = strings.TrimSuffix(sig, "{")
				sig = strings.TrimSpace(sig)
				linkText = fmt.Sprintf("%s `%s`", e.ID, sig)
			}

			sb.WriteString(fmt.Sprintf("- [%s](#%s)\n", linkText, anchor))
		}
		sb.WriteString("\n")
	}

	for _, e := range f.Elements {
		sb.WriteString(fmt.Sprintf("#### %s\n\n", e.ID))

		if e.Description != "" {
			sb.WriteString(e.Description + "\n\n")
		}
		sb.WriteString(fmt.Sprintf("```%s\n%s\n```\n\n", f.Language, e.Signature))
	}

	return sb.String()
}

func (p *Parser) generateGitMetadata(f *File) string {
	var sb strings.Builder

	sb.WriteString(p.generateDetailedCard(f))

	return sb.String()
}

func (p *Parser) generateDetailedCard(f *File) string {
	var sb strings.Builder

	sb.WriteString("<div>\n\n")

	sb.WriteString("### File Information\n\n")

	sb.WriteString("<table>\n")
	sb.WriteString("<tr>\n")

	sb.WriteString("<td>\n")

	commitShort := f.GitInfo.LastCommitHash
	if len(commitShort) > 7 {
		commitShort = commitShort[:7]
	}

	sb.WriteString("<strong>Last Update</strong><br/>\n")
	commitURL := git.GetCommitURL(p.RepoInfo, f.GitInfo.LastCommitHash)
	if commitURL != "" {
		sb.WriteString(fmt.Sprintf(
			"<a href=\"%s\"><code>%s</code></a><br/>\n",
			commitURL, commitShort))
	} else {
		sb.WriteString(fmt.Sprintf("<code>%s</code><br/>\n", commitShort))
	}
	sb.WriteString(fmt.Sprintf("<small>%s</small><br/>\n", f.GitInfo.LastCommitDate))

	if f.GitInfo.LastCommitMessage != "" {
		msg := f.GitInfo.LastCommitMessage
		if len(msg) > 60 {
			msg = msg[:57] + "..."
		}
		sb.WriteString(fmt.Sprintf("<em>%s</em>\n", msg))
	}

	sb.WriteString("</td>\n")

	sb.WriteString("<td>\n")

	if p.RepoInfo.RepoOwner != "" && p.RepoInfo.RepoName != "" {
		sb.WriteString("<strong>Repository</strong><br/>\n")
		fileURL := git.GetFileURL(p.RepoInfo, f.GitInfo.LastCommitHash, f.Path)
		if fileURL != "" {
			sb.WriteString(fmt.Sprintf(
				"<a href=\"%s\">%s/%s</a><br/>\n",
				fileURL, p.RepoInfo.RepoOwner, p.RepoInfo.RepoName))
		}
	}

	if p.RepoInfo.CurrentBranch != "" {
		sb.WriteString(fmt.Sprintf(
			"<strong>Branch:</strong> <code>%s</code><br/>\n",
			p.RepoInfo.CurrentBranch))
	}

	if f.GitInfo.TotalCommits > 0 {
		sb.WriteString(fmt.Sprintf(
			"<strong>History:</strong> %d commits\n",
			f.GitInfo.TotalCommits))
	}

	sb.WriteString("</td>\n")
	sb.WriteString("</tr>\n")

	if len(f.GitInfo.Authors) > 0 {
		sb.WriteString("<tr>\n")
		sb.WriteString("<td colspan=\"2\">\n")
		sb.WriteString("<strong>Contributors</strong><br/>\n")
		sb.WriteString("<div>\n")

		for _, author := range f.GitInfo.Authors {
			avatarURL := git.GetAvatarURL(p.RepoInfo, author, 40)
			sb.WriteString("<div>\n")
			sb.WriteString(fmt.Sprintf(
				"<img src=\"%s\" alt=\"%s\" width=\"32\" height=\"32\" />\n",
				avatarURL, author.Name))
			sb.WriteString(fmt.Sprintf("<span>%s</span>\n", author.Name))
			sb.WriteString("</div>\n")
		}

		sb.WriteString("</div>\n")
		sb.WriteString("</td>\n")
		sb.WriteString("</tr>\n")
	}

	sb.WriteString("</table>\n\n")
	sb.WriteString("</div>\n\n")

	return sb.String()
}

// func (p *Parser) generateDetailedCard(f *File) string {
// 	var sb strings.Builder

// 	sb.WriteString("<div style=\"background: linear-gradient(to right, #f6f8fa, #ffffff); border: 1px solid #d0d7de; border-radius: 8px; padding: 20px; margin-bottom: 24px; box-shadow: 0 1px 3px rgba(0,0,0,0.05);\">\n\n")

// 	sb.WriteString("### File Information\n\n")

// 	sb.WriteString("<table style=\"width: 100%; border-collapse: collapse;\">\n")
// 	sb.WriteString("<tr style=\"border-bottom: 1px solid #d0d7de;\">\n")

// 	sb.WriteString("<td style=\"padding: 12px; vertical-align: top; width: 50%;\">\n")

// 	commitShort := f.GitInfo.LastCommitHash
// 	if len(commitShort) > 7 {
// 		commitShort = commitShort[:7]
// 	}

// 	sb.WriteString("<strong>Last Update</strong><br/>\n")
// 	commitURL := git.GetCommitURL(p.RepoInfo, f.GitInfo.LastCommitHash)
// 	if commitURL != "" {
// 		sb.WriteString(fmt.Sprintf(
// 			"<a href=\"%s\" style=\"font-family: monospace; color: #0969da;\">%s</a><br/>\n",
// 			commitURL, commitShort))
// 	} else {
// 		sb.WriteString(fmt.Sprintf("<code>%s</code><br/>\n", commitShort))
// 	}
// 	sb.WriteString(fmt.Sprintf("<small style=\"color: #656d76;\">%s</small><br/>\n", f.GitInfo.LastCommitDate))

// 	if f.GitInfo.LastCommitMessage != "" {
// 		msg := f.GitInfo.LastCommitMessage
// 		if len(msg) > 60 {
// 			msg = msg[:57] + "..."
// 		}
// 		sb.WriteString(fmt.Sprintf("<em style=\"color: #656d76;\">%s</em>\n", msg))
// 	}

// 	sb.WriteString("</td>\n")

// 	sb.WriteString("<td style=\"padding: 12px; vertical-align: top; width: 50%;\">\n")

// 	if p.RepoInfo.RepoOwner != "" && p.RepoInfo.RepoName != "" {
// 		sb.WriteString("<strong>Repository</strong><br/>\n")
// 		fileURL := git.GetFileURL(p.RepoInfo, f.GitInfo.LastCommitHash, f.Path)
// 		if fileURL != "" {
// 			sb.WriteString(fmt.Sprintf(
// 				"<a href=\"%s\" style=\"color: #0969da;\">%s/%s</a><br/>\n",
// 				fileURL, p.RepoInfo.RepoOwner, p.RepoInfo.RepoName))
// 		}
// 	}

// 	if p.RepoInfo.CurrentBranch != "" {
// 		sb.WriteString(fmt.Sprintf(
// 			"<strong>Branch:</strong> <code style=\"background: #f6f8fa; padding: 2px 6px; border-radius: 3px;\">%s</code><br/>\n",
// 			p.RepoInfo.CurrentBranch))
// 	}

// 	if f.GitInfo.TotalCommits > 0 {
// 		sb.WriteString(fmt.Sprintf(
// 			"<strong>History:</strong> %d commits\n",
// 			f.GitInfo.TotalCommits))
// 	}

// 	sb.WriteString("</td>\n")
// 	sb.WriteString("</tr>\n")

// 	if len(f.GitInfo.Authors) > 0 {
// 		sb.WriteString("<tr>\n")
// 		sb.WriteString("<td colspan=\"2\" style=\"padding: 12px;\">\n")
// 		sb.WriteString("<strong>Contributors</strong><br/>\n")
// 		sb.WriteString("<div style=\"display: flex; gap: 12px; flex-wrap: wrap; margin-top: 8px;\">\n")

// 		for _, author := range f.GitInfo.Authors {
// 			avatarURL := git.GetAvatarURL(p.RepoInfo, author, 40)
// 			sb.WriteString("<div style=\"display: flex; align-items: center; gap: 8px;\">\n")
// 			sb.WriteString(fmt.Sprintf(
// 				"<img src=\"%s\" alt=\"%s\" width=\"32\" height=\"32\" style=\"border-radius: 50%%; border: 2px solid #d0d7de;\" />\n",
// 				avatarURL, author.Name))
// 			sb.WriteString(fmt.Sprintf("<span style=\"color: #24292f;\">%s</span>\n", author.Name))
// 			sb.WriteString("</div>\n")
// 		}

// 		sb.WriteString("</div>\n")
// 		sb.WriteString("</td>\n")
// 		sb.WriteString("</tr>\n")
// 	}

// 	sb.WriteString("</table>\n\n")
// 	sb.WriteString("</div>\n\n")

// 	return sb.String()
// }
