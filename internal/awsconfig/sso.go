package awsconfig

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sso"
)

// DefaultCacheDir is where the AWS CLI caches SSO tokens.
func DefaultCacheDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".aws", "sso", "cache")
}

// ErrNoToken means there is no usable cached token: run aws sso login.
var ErrNoToken = errors.New("no valid SSO token")

// Token is the subset of an AWS CLI SSO cache entry we use.
type Token struct {
	AccessToken string    `json:"accessToken"`
	ExpiresAt   time.Time `json:"-"`
}

// ReadToken reads the AWS CLI's cached token for an sso-session
// (<cacheDir>/<sha1(session)>.json) and checks it has not expired.
func ReadToken(cacheDir, session string, now time.Time) (*Token, error) {
	sum := sha1.Sum([]byte(session))
	path := filepath.Join(cacheDir, hex.EncodeToString(sum[:])+".json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("%w: %s not found", ErrNoToken, path)
	}
	if err != nil {
		return nil, err
	}
	var raw struct {
		AccessToken string `json:"accessToken"`
		ExpiresAt   string `json:"expiresAt"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	exp, err := parseExpiry(raw.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("parse %s expiresAt: %w", path, err)
	}
	if raw.AccessToken == "" || !now.Before(exp) {
		return nil, fmt.Errorf("%w: token expired at %s", ErrNoToken, exp.Local().Format(time.RFC3339))
	}
	return &Token{AccessToken: raw.AccessToken, ExpiresAt: exp}, nil
}

func parseExpiry(s string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05UTC", "2006-01-02T15:04:05Z0700"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised time %q", s)
}

// RoleLister lists the SSO roles a token holder has in an account.
type RoleLister interface {
	ListRoles(ctx context.Context, accessToken, accountID string) ([]string, error)
}

// SSORoleLister calls the IAM Identity Center portal API (sso:ListAccountRoles).
type SSORoleLister struct{ Region string }

func (l SSORoleLister) ListRoles(ctx context.Context, accessToken, accountID string) ([]string, error) {
	client := sso.New(sso.Options{Region: l.Region, Credentials: aws.AnonymousCredentials{}})
	p := sso.NewListAccountRolesPaginator(client, &sso.ListAccountRolesInput{
		AccessToken: aws.String(accessToken),
		AccountId:   aws.String(accountID),
	})
	var roles []string
	for p.HasMorePages() {
		out, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, r := range out.RoleList {
			roles = append(roles, aws.ToString(r.RoleName))
		}
	}
	return roles, nil
}
