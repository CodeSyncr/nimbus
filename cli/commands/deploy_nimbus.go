package commands

/*
|--------------------------------------------------------------------------
| nimbus deploy → Nimbus Cloud
|--------------------------------------------------------------------------
|
| The default target: pack the app's folder, upload it to Nimbus Cloud
| (signed in with `nimbus login`), and follow the deployment's log until
| it is live or has failed. The cloud builds it on a server of the fleet,
| starts it beside the running version and switches over once it answers.
|
|   nimbus deploy                 the project named after the folder
|   nimbus deploy --app my-api    a named project (made on first deploy)
|
*/

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/CodeSyncr/nimbus/cli/auth"
)

// nimbusDeploySkip are folders and files never uploaded: local state,
// secrets and what the build makes itself.
var nimbusDeploySkip = map[string]bool{
	".git": true, "node_modules": true, "tmp": true, "storage": true, ".idea": true, ".vscode": true,
	".DS_Store": true, ".env": true, ".env.local": true,
}

func nimbusSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// packApp writes the folder as a gzipped tar, leaving out what is skipped
// and anything .nimbusignore names (one path per line, relative).
func packApp(root string) (*bytes.Buffer, int, error) {
	ignore := map[string]bool{}
	if b, err := os.ReadFile(filepath.Join(root, ".nimbusignore")); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if l = strings.Trim(strings.TrimSpace(l), "/"); l != "" && !strings.HasPrefix(l, "#") {
				ignore[l] = true
			}
		}
	}
	buf := &bytes.Buffer{}
	gz := gzip.NewWriter(buf)
	tw := tar.NewWriter(gz)
	files := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if nimbusDeploySkip[d.Name()] || ignore[rel] || strings.HasPrefix(d.Name(), ".env.") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() && !info.IsDir() {
			return nil // links and devices are not uploaded
		}
		h, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		h.Name = rel
		if info.IsDir() {
			h.Name += "/"
		}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := io.Copy(tw, f); err != nil {
			return err
		}
		files++
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	if err := tw.Close(); err != nil {
		return nil, 0, err
	}
	if err := gz.Close(); err != nil {
		return nil, 0, err
	}
	return buf, files, nil
}

func gitSubject() string {
	out, err := exec.Command("git", "log", "-1", "--pretty=%h %s").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// runNimbusDeploy deploys the app in the current folder to Nimbus Cloud.
func runNimbusDeploy(app string, out io.Writer) error {
	creds, err := auth.LoadCredentials()
	if err != nil || creds == nil || creds.AccessToken == "" {
		return errors.New("not signed in to Nimbus Cloud: run `nimbus login` first")
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	if app == "" {
		app = filepath.Base(root)
	}
	slug := nimbusSlug(app)
	if slug == "" {
		return errors.New("give the app a name with --app")
	}
	base := strings.TrimRight(auth.GetServerURL(), "/")

	fmt.Fprintf(out, "→ Packing %s\n", root)
	body, files, err := packApp(root)
	if err != nil {
		return fmt.Errorf("packing the app: %w", err)
	}
	fmt.Fprintf(out, "→ Uploading %d files (%.1f MB) to %s\n", files, float64(body.Len())/(1<<20), slug)
	req, _ := http.NewRequest(http.MethodPost, base+"/api/v1/deploy/projects/"+slug+"/deployments?create=1", body)
	req.Header.Set("Authorization", "Bearer "+creds.AccessToken)
	req.Header.Set("Content-Type", "application/gzip")
	if msg := gitSubject(); msg != "" {
		req.Header.Set("X-Nimbus-Message", msg)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Minute}).Do(req)
	if err != nil {
		return fmt.Errorf("uploading: %w", err)
	}
	var up struct {
		Error      string `json:"error"`
		URL        string `json:"url"`
		Dashboard  string `json:"dashboard"`
		Project    string `json:"project"`
		Deployment struct {
			ID     uint   `json:"ID"`
			Number int    `json:"number"`
			Status string `json:"status"`
		} `json:"deployment"`
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	_ = json.Unmarshal(raw, &up)
	if resp.StatusCode >= 300 || up.Deployment.ID == 0 {
		msg := up.Error
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
		}
		return fmt.Errorf("Nimbus Cloud refused the deploy (%d): %s", resp.StatusCode, msg)
	}
	fmt.Fprintf(out, "→ Deployment #%d  %s\n\n", up.Deployment.Number, up.Dashboard)

	// Follow it: print the log as it grows, until it ends.
	offset := 0
	for {
		time.Sleep(1500 * time.Millisecond)
		sreq, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/deploy/projects/%s/deployments/%d?offset=%d", base, up.Project, up.Deployment.ID, offset), nil)
		sreq.Header.Set("Authorization", "Bearer "+creds.AccessToken)
		sresp, err := (&http.Client{Timeout: 30 * time.Second}).Do(sreq)
		if err != nil {
			continue // a blip; keep following
		}
		var st struct {
			Status string `json:"status"`
			Error  string `json:"error"`
			Log    string `json:"log"`
			Offset int    `json:"offset"`
			URL    string `json:"url"`
		}
		_ = json.NewDecoder(sresp.Body).Decode(&st)
		sresp.Body.Close()
		if st.Log != "" {
			fmt.Fprint(out, indentLog(st.Log))
		}
		if st.Offset > 0 {
			offset = st.Offset
		}
		switch st.Status {
		case "live":
			fmt.Fprintf(out, "\n✓ Live at %s\n", st.URL)
			return nil
		case "failed":
			return fmt.Errorf("the deployment failed: %s", st.Error)
		}
	}
}

func indentLog(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = "  " + l
	}
	return strings.Join(lines, "\n") + "\n"
}
