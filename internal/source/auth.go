package source

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/bradleyfalzon/ghinstallation/v2"
	gittransport "github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

const gitBasicAuthUser = "x-access-token"

func parseInt64(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

func (c *gitClient) authMethod() (gittransport.AuthMethod, error) {
	switch {
	case c.auth.App != nil:
		token, err := appInstallationToken(c.auth.App)
		if err != nil {
			return nil, err
		}
		return &githttp.BasicAuth{Username: gitBasicAuthUser, Password: token}, nil
	case c.auth.Basic != nil:
		return &githttp.BasicAuth{Username: c.auth.Basic.Username, Password: c.auth.Basic.Password}, nil
	case c.auth.Token != "":
		return &githttp.BasicAuth{Username: gitBasicAuthUser, Password: c.auth.Token}, nil
	default:
		return nil, nil // anonymous access
	}
}

func appInstallationToken(app *GitHubApp) (string, error) {
	if app.AppID == "" {
		return "", fmt.Errorf("git source: githubAppID is empty")
	}
	if app.InstallationID == "" {
		return "", fmt.Errorf("git source: githubAppInstallationID is empty")
	}
	if len(app.PrivateKey) == 0 {
		return "", fmt.Errorf("git source: githubAppPrivateKey is empty")
	}

	appID, err := parseInt64(app.AppID)
	if err != nil {
		return "", fmt.Errorf("git source: invalid githubAppID: %w", err)
	}
	installationID, err := parseInt64(app.InstallationID)
	if err != nil {
		return "", fmt.Errorf("git source: invalid githubAppInstallationID: %w", err)
	}

	itr, err := ghinstallation.New(http.DefaultTransport, appID, installationID, app.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("git source: creating installation transport: %w", err)
	}
	if app.BaseURL != "" {
		itr.BaseURL = app.BaseURL
	}

	token, err := itr.Token(context.Background())
	if err != nil {
		return "", fmt.Errorf("git source: minting installation token: %w", err)
	}
	return token, nil
}
