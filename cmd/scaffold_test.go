package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gh "github.com/javaBin/javabin-cli/internal/github"
)

// buildContextSources returns the source paths of COPY instructions that read
// from the build context (not --from=<stage>).
func buildContextSources(dockerfile string) []string {
	var srcs []string
	for _, line := range strings.Split(dockerfile, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "COPY" || strings.HasPrefix(fields[1], "--from") {
			continue
		}
		srcs = append(srcs, fields[1:len(fields)-1]...)
	}
	return srcs
}

func TestScaffoldsAreSelfContained(t *testing.T) {
	for runtime, rc := range runtimes {
		t.Run(runtime, func(t *testing.T) {
			files := rc.render("demo", "9090")
			df, ok := files["Dockerfile"]
			if !ok {
				t.Fatal("no Dockerfile")
			}
			// Every file the build copies from the context is part of the scaffold.
			for _, src := range buildContextSources(df) {
				found := false
				for path := range files {
					if m, _ := filepath.Match(src, path); m || path == src || strings.HasPrefix(path, strings.TrimSuffix(src, "/")+"/") {
						found = true
					}
				}
				if !found {
					t.Errorf("Dockerfile copies %q, which the scaffold does not write", src)
				}
			}
			for _, banned := range []string{"mvnw", "gradlew"} {
				if strings.Contains(df, banned) {
					t.Errorf("Dockerfile uses %s, which the scaffold does not ship", banned)
				}
			}
			for _, want := range []string{"USER 1000\n", "ENV PORT=9090\n", "EXPOSE 9090\n"} {
				if !strings.Contains(df, want) {
					t.Errorf("Dockerfile missing %q", want)
				}
			}
			var src strings.Builder
			for path, body := range files {
				if strings.Contains(body, "{{") {
					t.Errorf("%s has an unrendered placeholder", path)
				}
				src.WriteString(body)
			}
			if !strings.Contains(src.String(), "/health") || !strings.Contains(src.String(), "9090") {
				t.Error("app does not serve /health on the chosen port by default")
			}
		})
	}
}

func TestWriteFilesCreatesDirectories(t *testing.T) {
	dir := t.TempDir()
	paths, err := writeFiles(dir, runtimes["java"].render("demo", "8080"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "src/main/java/no/java/app/App.java")); err != nil {
		t.Error(err)
	}
	if len(paths) != len(runtimes["java"].files) || paths[0] != ".dockerignore" {
		t.Errorf("paths = %v", paths)
	}
}

// TestScaffoldImagesServeHealth builds each scaffold and runs it the way the
// platform does (uid 1000, read-only root, writable /tmp). It needs Docker
// and network, so it only runs with JAVABIN_SCAFFOLD_DOCKER=1.
func TestScaffoldImagesServeHealth(t *testing.T) {
	if os.Getenv("JAVABIN_SCAFFOLD_DOCKER") != "1" {
		t.Skip("set JAVABIN_SCAFFOLD_DOCKER=1 to build and run the scaffold images")
	}
	hostPort := 18080
	for runtime, rc := range runtimes {
		hostPort++
		port := "9090"
		t.Run(runtime, func(t *testing.T) {
			dir := t.TempDir()
			if _, err := writeFiles(dir, rc.render("demo", port)); err != nil {
				t.Fatal(err)
			}
			tag := "javabin-scaffold-test-" + runtime
			if out, err := exec.Command("docker", "build", "-q", "-t", tag, dir).CombinedOutput(); err != nil {
				t.Fatalf("docker build: %v\n%s", err, out)
			}
			out, err := exec.Command("docker", "run", "-d", "--rm", "--user", "1000", "--read-only",
				"--tmpfs", "/tmp", "-p", fmt.Sprintf("%d:%s", hostPort, port), tag).Output()
			if err != nil {
				t.Fatalf("docker run: %v", err)
			}
			id := strings.TrimSpace(string(out))
			defer exec.Command("docker", "rm", "-f", id).Run()

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			url := fmt.Sprintf("http://127.0.0.1:%d/health", hostPort)
			for {
				resp, err := http.Get(url)
				if err == nil {
					resp.Body.Close()
					if resp.StatusCode != 200 {
						t.Fatalf("GET /health = %d", resp.StatusCode)
					}
					return
				}
				select {
				case <-ctx.Done():
					logs, _ := exec.Command("docker", "logs", id).CombinedOutput()
					t.Fatalf("no /health response: %v\n%s", err, logs)
				case <-time.After(time.Second):
				}
			}
		})
	}
}

func TestTeamAccessErrorSaysHowToRecover(t *testing.T) {
	err := teamAccessError(fmt.Errorf("x: %w", gh.ErrNoTeamAccess), "video", "recorder").Error()
	for _, want := range []string{
		"gh api -X PUT orgs/javaBin/teams/video/repos/javaBin/recorder -f permission=push",
		"NOT pushed",
		"cd recorder && git push",
		"maintainer",
	} {
		if !strings.Contains(err, want) {
			t.Errorf("error missing %q:\n%s", want, err)
		}
	}
}
