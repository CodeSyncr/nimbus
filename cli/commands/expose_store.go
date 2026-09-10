package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/CodeSyncr/nimbus/cli/auth"
)

// exposeStoreFile keeps, per project directory, the tunnel subdomain that
// project last published under. A reserved name belongs to the account, so
// remembering it here is only about not retyping --subdomain: `nimbus expose`
// in the same checkout keeps handing out the same URL, which is what makes a
// tunnel usable as a fixed webhook or OAuth callback target.
const exposeStoreFile = "tunnels.json"

type exposeStore struct {
	// Projects maps an absolute project path to a subdomain.
	Projects map[string]string `json:"projects"`
}

func exposeStorePath() (string, error) {
	dir, err := auth.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, exposeStoreFile), nil
}

func loadExposeStore() *exposeStore {
	s := &exposeStore{Projects: map[string]string{}}
	path, err := exposeStorePath()
	if err != nil {
		return s
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	if err := json.Unmarshal(data, s); err != nil || s.Projects == nil {
		return &exposeStore{Projects: map[string]string{}}
	}
	return s
}

// rememberedSubdomain returns the name this project last used ("" if none).
func rememberedSubdomain(appRoot string) string {
	key := projectKey(appRoot)
	if key == "" {
		return ""
	}
	return loadExposeStore().Projects[key]
}

// rememberSubdomain records name for this project, replacing any previous
// one. Failures are silent: this is a convenience, never a reason to end a
// working tunnel.
func rememberSubdomain(appRoot, name string) {
	key := projectKey(appRoot)
	if key == "" || name == "" {
		return
	}
	store := loadExposeStore()
	if store.Projects[key] == name {
		return
	}
	store.Projects[key] = name
	path, err := exposeStorePath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, append(data, '\n'), 0o600) != nil {
		return
	}
	if os.Rename(tmp, path) != nil {
		_ = os.Remove(tmp)
	}
}

// forgetSubdomain drops this project's remembered name.
func forgetSubdomain(appRoot string) {
	key := projectKey(appRoot)
	if key == "" {
		return
	}
	store := loadExposeStore()
	if _, ok := store.Projects[key]; !ok {
		return
	}
	delete(store.Projects, key)
	path, err := exposeStorePath()
	if err != nil {
		return
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, append(data, '\n'), 0o600)
}

func projectKey(appRoot string) string {
	appRoot = strings.TrimSpace(appRoot)
	if appRoot == "" {
		return ""
	}
	if abs, err := filepath.Abs(appRoot); err == nil {
		return abs
	}
	return appRoot
}

// subdomainOfURL pulls the first label out of "https://name.tunnel.host".
func subdomainOfURL(publicURL string) string {
	host := publicURL
	if i := strings.Index(host, "://"); i >= 0 {
		host = host[i+3:]
	}
	if i := strings.IndexAny(host, "/:"); i >= 0 {
		host = host[:i]
	}
	label, _, found := strings.Cut(host, ".")
	if !found {
		return ""
	}
	return label
}
