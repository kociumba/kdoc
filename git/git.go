package git

import (
	"crypto/md5"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
)

type FileInfo struct {
	LastCommitHash    string
	LastCommitDate    string
	LastCommitMessage string
	LastAuthorName    string
	LastAuthorEmail   string
	Authors           []Author
	TotalCommits      int
}

type Author struct {
	Name  string
	Email string
}

type RepoInfo struct {
	IsRepo        bool
	RemoteURL     string
	Provider      string
	RepoOwner     string
	RepoName      string
	CurrentBranch string
	GitRoot       string
}

var (
	repoInfoCache *RepoInfo
	repoInfoOnce  sync.Once
)

func GetRepoInfo(repoPath string) *RepoInfo {
	repoInfoOnce.Do(func() {
		repoInfoCache = detectRepoInfo(repoPath)
	})
	return repoInfoCache
}

func detectRepoInfo(repoPath string) *RepoInfo {
	info := &RepoInfo{}

	cmd := exec.Command("git", "-C", repoPath, "rev-parse", "--git-dir")
	if err := cmd.Run(); err != nil {
		return info
	}
	info.IsRepo = true

	cmd = exec.Command("git", "-C", repoPath, "rev-parse", "--show-toplevel")
	if out, err := cmd.Output(); err == nil {
		info.GitRoot = strings.TrimSpace(string(out))
	} else {
		info.GitRoot = repoPath
	}

	cmd = exec.Command("git", "-C", repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	if out, err := cmd.Output(); err == nil {
		info.CurrentBranch = strings.TrimSpace(string(out))
	}

	cmd = exec.Command("git", "-C", repoPath, "config", "--get", "remote.origin.url")
	out, err := cmd.Output()
	if err != nil {
		return info
	}
	info.RemoteURL = strings.TrimSpace(string(out))

	info.Provider, info.RepoOwner, info.RepoName = parseRemoteURL(info.RemoteURL)

	return info
}

func parseRemoteURL(url string) (provider, owner, repo string) {
	httpsRe := regexp.MustCompile(`https?://([^/]+)/([^/]+)/([^/]+?)(?:\.git)?$`)
	if matches := httpsRe.FindStringSubmatch(url); len(matches) == 4 {
		host := matches[1]
		owner = matches[2]
		repo = matches[3]

		if strings.Contains(host, "github.com") {
			provider = "github"
		} else if strings.Contains(host, "gitlab.com") {
			provider = "gitlab"
		} else if strings.Contains(host, "gitea") {
			provider = "gitea"
		} else {
			provider = "other"
		}
		return
	}

	// Handle SSH URLs (git@github.com:user/repo.git)
	sshRe := regexp.MustCompile(`git@([^:]+):([^/]+)/([^/]+?)(?:\.git)?$`)
	if matches := sshRe.FindStringSubmatch(url); len(matches) == 4 {
		host := matches[1]
		owner = matches[2]
		repo = matches[3]

		if strings.Contains(host, "github.com") {
			provider = "github"
		} else if strings.Contains(host, "gitlab.com") {
			provider = "gitlab"
		} else if strings.Contains(host, "gitea") {
			provider = "gitea"
		} else {
			provider = "other"
		}
		return
	}

	return "unknown", "", ""
}

func GetFileInfo(repoPath, filePath string) (*FileInfo, error) {
	info := &FileInfo{}

	cmd := exec.Command("git", "-C", repoPath, "log", "-1",
		"--format=%H|%an|%ae|%ad|%s", "--date=short", "--", filePath)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get last commit: %w", err)
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("no git history for file")
	}

	parts := strings.Split(strings.TrimSpace(string(out)), "|")
	if len(parts) >= 5 {
		info.LastCommitHash = parts[0]
		info.LastAuthorName = parts[1]
		info.LastAuthorEmail = parts[2]
		info.LastCommitDate = parts[3]
		info.LastCommitMessage = parts[4]
	}

	cmd = exec.Command("git", "-C", repoPath, "log", "--follow", "--oneline", "--", filePath)
	out, err = cmd.Output()
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		info.TotalCommits = len(lines)
	}

	cmd = exec.Command("git", "-C", repoPath, "shortlog", "-sne", "--follow", "--", filePath)
	out, err = cmd.Output()
	if err == nil {
		authorMap := make(map[string]Author)
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")

		authorRe := regexp.MustCompile(`^\s*\d+\s+(.+?)\s+<(.+?)>$`)
		for _, line := range lines {
			if matches := authorRe.FindStringSubmatch(line); len(matches) == 3 {
				name := strings.TrimSpace(matches[1])
				email := strings.TrimSpace(matches[2])
				authorMap[email] = Author{Name: name, Email: email}
			}
		}

		for _, author := range authorMap {
			info.Authors = append(info.Authors, author)
		}
	}

	return info, nil
}

func GetCommitURL(repoInfo *RepoInfo, commitHash string) string {
	if repoInfo.Provider == "unknown" || repoInfo.RepoOwner == "" || repoInfo.RepoName == "" {
		return ""
	}

	switch repoInfo.Provider {
	case "github":
		return fmt.Sprintf("https://github.com/%s/%s/commit/%s",
			repoInfo.RepoOwner, repoInfo.RepoName, commitHash)
	case "gitlab":
		return fmt.Sprintf("https://gitlab.com/%s/%s/-/commit/%s",
			repoInfo.RepoOwner, repoInfo.RepoName, commitHash)
	case "gitea":
		return fmt.Sprintf("https://%s/%s/%s/commit/%s",
			"gitea.com", repoInfo.RepoOwner, repoInfo.RepoName, commitHash)
	}

	return ""
}

func GetAvatarURL(repoInfo *RepoInfo, author Author, size int) string {
	switch repoInfo.Provider {
	case "github":
		if strings.HasSuffix(author.Email, "@users.noreply.github.com") {
			parts := strings.Split(author.Email, "@")
			if len(parts) > 0 {
				userPart := parts[0]
				if idx := strings.Index(userPart, "+"); idx != -1 {
					username := userPart[idx+1:]
					return fmt.Sprintf("https://github.com/%s.png?size=%d", username, size)
				}
			}
		}
		return getGravatarURL(author.Email, size)

	case "gitlab":
		return getGravatarURL(author.Email, size)

	default:
		return getGravatarURL(author.Email, size)
	}
}

func getGravatarURL(email string, size int) string {
	email = strings.ToLower(strings.TrimSpace(email))
	hash := fmt.Sprintf("%x", md5.Sum([]byte(email)))
	return fmt.Sprintf("https://www.gravatar.com/avatar/%s?s=%d&d=identicon", hash, size)
}

func GetFileURL(repoInfo *RepoInfo, commitHash, filePath string) string {
	if repoInfo.Provider == "unknown" || repoInfo.RepoOwner == "" || repoInfo.RepoName == "" {
		return ""
	}

	switch repoInfo.Provider {
	case "github":
		return fmt.Sprintf("https://github.com/%s/%s/blob/%s/%s",
			repoInfo.RepoOwner, repoInfo.RepoName, commitHash, filePath)
	case "gitlab":
		return fmt.Sprintf("https://gitlab.com/%s/%s/-/blob/%s/%s",
			repoInfo.RepoOwner, repoInfo.RepoName, commitHash, filePath)
	}

	return ""
}
