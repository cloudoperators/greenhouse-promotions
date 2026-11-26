package source

import (
	"context"
	"fmt"
	"time"
)

const (
	KeyUsername = "username"
	KeyPassword = "password"
	KeyToken    = "token"

	KeyGitHubAppID             = "githubAppID"
	KeyGitHubAppInstallationID = "githubAppInstallationID"
	KeyGitHubAppPrivateKey     = "githubAppPrivateKey"
	KeyGitHubAppBaseURL        = "githubAppBaseURL"
)

type BasicAuth struct {
	Username string
	Password string
}

type GitHubApp struct {
	AppID          string
	InstallationID string
	PrivateKey     []byte
	BaseURL        string
}

type Auth struct {
	Basic *BasicAuth
	Token string
	App   *GitHubApp
}

func AuthFromData(data map[string][]byte) Auth {
	if id, ok := data[KeyGitHubAppID]; ok && len(id) > 0 {
		return Auth{App: &GitHubApp{
			AppID:          string(data[KeyGitHubAppID]),
			InstallationID: string(data[KeyGitHubAppInstallationID]),
			PrivateKey:     data[KeyGitHubAppPrivateKey],
			BaseURL:        string(data[KeyGitHubAppBaseURL]),
		}}
	}
	if u, ok := data[KeyUsername]; ok && len(u) > 0 {
		return Auth{Basic: &BasicAuth{
			Username: string(data[KeyUsername]),
			Password: string(data[KeyPassword]),
		}}
	}
	if t, ok := data[KeyToken]; ok && len(t) > 0 {
		return Auth{Token: string(t)}
	}
	return Auth{}
}

// Version is a single discovered version. Version is the resolved tag or
// branch name; Revision is the commit SHA (git) or digest (oci).
type Version struct {
	Version   string
	Revision  string
	CreatedAt time.Time
}

// Client discovers versions from a source, newest first, and fetches the
// definition file at a specific discovered ref.
type Client interface {
	Discover(ctx context.Context) ([]Version, error)
	// Fetch returns the raw bytes of filePath at the given ref (git tag/commit,
	// or OCI tag). The ref is a Version.Version returned by Discover.
	Fetch(ctx context.Context, ref, filePath string) ([]byte, error)
}

type GitConfig struct {
	Repository       string
	SemverConstraint string
	TagPattern       string
	Depth            int32
}

type OCIConfig struct {
	Repository       string
	SemverConstraint string
	LayerMediaType   string
}

func NewGitClient(cfg GitConfig, auth Auth) (Client, error) {
	if cfg.Repository == "" {
		return nil, fmt.Errorf("git source: repository is required")
	}
	return &gitClient{cfg: cfg, auth: auth}, nil
}

func NewOCIClient(cfg OCIConfig, auth Auth) (Client, error) {
	if cfg.Repository == "" {
		return nil, fmt.Errorf("oci source: repository is required")
	}
	return &ociClient{cfg: cfg, auth: auth}, nil
}
