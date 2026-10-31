package cmd

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// runtimeConfig is what `javabin init` writes for a runtime: a minimal app
// that builds from a clean checkout (no wrapper scripts or lockfiles needed),
// listens on $PORT (baked in as the chosen port) and answers GET /health with
// 200. The image runs as uid 1000 and writes nothing outside /tmp, matching
// the platform's ECS task (user "1000", read-only root filesystem, /tmp
// mounted writable).
type runtimeConfig struct {
	defaultPort string
	// files maps repo-relative paths to contents. {{NAME}} and {{PORT}} are
	// replaced when rendering.
	files map[string]string
}

const dockerignoreCommon = `.git
.github
app.yaml
README.md
CLAUDE.md
`

var runtimes = map[string]runtimeConfig{
	"java": {
		defaultPort: "8080",
		files: map[string]string{
			"Dockerfile": `FROM maven:3.9-eclipse-temurin-21-alpine AS build
WORKDIR /app
COPY pom.xml .
COPY src ./src
RUN mvn -q -B package -DskipTests

FROM eclipse-temurin:21-jre-alpine
RUN adduser -D -H -u 1000 app
WORKDIR /app
COPY --from=build /app/target/app.jar app.jar
# The platform runs the task as uid 1000 with a read-only root filesystem;
# only /tmp is writable.
USER 1000
ENV PORT={{PORT}}
EXPOSE {{PORT}}
ENTRYPOINT ["java", "-jar", "/app/app.jar"]
`,
			".dockerignore": dockerignoreCommon + "target\n",
			"pom.xml": `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0"
         xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
         xsi:schemaLocation="http://maven.apache.org/POM/4.0.0 https://maven.apache.org/xsd/maven-4.0.0.xsd">
  <modelVersion>4.0.0</modelVersion>

  <groupId>no.java</groupId>
  <artifactId>{{NAME}}</artifactId>
  <version>1.0.0</version>
  <packaging>jar</packaging>

  <properties>
    <maven.compiler.release>21</maven.compiler.release>
    <project.build.sourceEncoding>UTF-8</project.build.sourceEncoding>
  </properties>

  <build>
    <finalName>app</finalName>
    <plugins>
      <plugin>
        <groupId>org.apache.maven.plugins</groupId>
        <artifactId>maven-jar-plugin</artifactId>
        <version>3.4.2</version>
        <configuration>
          <archive>
            <manifest>
              <mainClass>no.java.app.App</mainClass>
            </manifest>
          </archive>
        </configuration>
      </plugin>
    </plugins>
  </build>
</project>
`,
			"src/main/java/no/java/app/App.java": `package no.java.app;

import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;

import java.io.IOException;
import java.io.OutputStream;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;

public class App {
    public static void main(String[] args) throws IOException {
        int port = Integer.parseInt(System.getenv().getOrDefault("PORT", "{{PORT}}"));
        HttpServer server = HttpServer.create(new InetSocketAddress(port), 0);
        server.createContext("/health", exchange -> respond(exchange, "ok"));
        server.createContext("/", exchange -> respond(exchange, "Hello from {{NAME}}"));
        server.start();
        System.out.println("Listening on :" + port);
    }

    private static void respond(HttpExchange exchange, String body) throws IOException {
        byte[] bytes = body.getBytes(StandardCharsets.UTF_8);
        exchange.getResponseHeaders().set("Content-Type", "text/plain; charset=utf-8");
        exchange.sendResponseHeaders(200, bytes.length);
        try (OutputStream out = exchange.getResponseBody()) {
            out.write(bytes);
        }
    }
}
`,
		},
	},
	"kotlin": {
		defaultPort: "8080",
		files: map[string]string{
			"Dockerfile": `FROM gradle:8-jdk21-alpine AS build
WORKDIR /app
COPY settings.gradle.kts build.gradle.kts ./
COPY src ./src
RUN gradle --no-daemon -q installDist

FROM eclipse-temurin:21-jre-alpine
RUN adduser -D -H -u 1000 app
COPY --from=build /app/build/install/app /opt/app
# The platform runs the task as uid 1000 with a read-only root filesystem;
# only /tmp is writable.
USER 1000
ENV PORT={{PORT}}
EXPOSE {{PORT}}
CMD ["/opt/app/bin/app"]
`,
			".dockerignore": dockerignoreCommon + "build\n.gradle\n",
			"settings.gradle.kts": `rootProject.name = "app"
`,
			"build.gradle.kts": `plugins {
    kotlin("jvm") version "2.1.21"
    application
}

repositories {
    mavenCentral()
}

kotlin {
    jvmToolchain(21)
}

application {
    mainClass.set("no.java.app.MainKt")
}
`,
			"src/main/kotlin/no/java/app/Main.kt": `package no.java.app

import com.sun.net.httpserver.HttpExchange
import com.sun.net.httpserver.HttpServer
import java.net.InetSocketAddress

fun main() {
    val port = System.getenv("PORT")?.toIntOrNull() ?: {{PORT}}
    val server = HttpServer.create(InetSocketAddress(port), 0)
    server.createContext("/health") { respond(it, "ok") }
    server.createContext("/") { respond(it, "Hello from {{NAME}}") }
    server.start()
    println("Listening on :$port")
}

private fun respond(exchange: HttpExchange, body: String) {
    val bytes = body.toByteArray()
    exchange.responseHeaders.set("Content-Type", "text/plain; charset=utf-8")
    exchange.sendResponseHeaders(200, bytes.size.toLong())
    exchange.responseBody.use { it.write(bytes) }
}
`,
		},
	},
	"typescript": {
		defaultPort: "3000",
		files: map[string]string{
			"Dockerfile": `FROM node:22-alpine AS build
WORKDIR /app
COPY package*.json tsconfig.json ./
# npm ci once you commit a package-lock.json; npm install works without one.
RUN if [ -f package-lock.json ]; then npm ci; else npm install --no-audit --no-fund; fi
COPY src ./src
RUN npm run build && npm prune --omit=dev

FROM node:22-alpine
WORKDIR /app
ENV NODE_ENV=production
COPY --from=build /app/package.json ./
COPY --from=build /app/node_modules ./node_modules
COPY --from=build /app/dist ./dist
# The platform runs the task as uid 1000 (the image's "node" user) with a
# read-only root filesystem; only /tmp is writable.
USER 1000
ENV PORT={{PORT}}
EXPOSE {{PORT}}
CMD ["node", "dist/index.js"]
`,
			".dockerignore": dockerignoreCommon + "node_modules\ndist\n",
			"package.json": `{
  "name": "{{NAME}}",
  "version": "1.0.0",
  "private": true,
  "type": "module",
  "scripts": {
    "build": "tsc",
    "start": "node dist/index.js"
  },
  "devDependencies": {
    "@types/node": "^22.0.0",
    "typescript": "^5.6.0"
  }
}
`,
			"tsconfig.json": `{
  "compilerOptions": {
    "target": "ES2022",
    "module": "NodeNext",
    "moduleResolution": "NodeNext",
    "rootDir": "src",
    "outDir": "dist",
    "strict": true,
    "skipLibCheck": true
  },
  "include": ["src"]
}
`,
			"src/index.ts": `import { createServer } from "node:http";

const port = Number(process.env.PORT ?? {{PORT}});

const server = createServer((req, res) => {
  res.setHeader("Content-Type", "text/plain; charset=utf-8");
  if (req.url === "/health") {
    res.end("ok");
    return;
  }
  res.end("Hello from {{NAME}}");
});

server.listen(port, () => console.log(` + "`Listening on :${port}`" + `));
`,
		},
	},
	"python": {
		defaultPort: "8000",
		files: map[string]string{
			"Dockerfile": `FROM python:3.12-slim
# No .pyc writes: the root filesystem is read-only at runtime.
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY main.py .
RUN useradd --uid 1000 --no-create-home app
# The platform runs the task as uid 1000 with a read-only root filesystem;
# only /tmp is writable.
USER 1000
ENV PORT={{PORT}}
EXPOSE {{PORT}}
CMD ["python", "main.py"]
`,
			".dockerignore": dockerignoreCommon + "__pycache__\n.venv\n",
			"requirements.txt": `# Add dependencies here, one per line (the scaffold uses only the standard library).
`,
			"main.py": `import os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        body = b"ok" if self.path == "/health" else b"Hello from {{NAME}}"
        self.send_response(200)
        self.send_header("Content-Type", "text/plain; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


if __name__ == "__main__":
    port = int(os.environ.get("PORT", "{{PORT}}"))
    print(f"Listening on :{port}", flush=True)
    ThreadingHTTPServer(("0.0.0.0", port), Handler).serve_forever()
`,
		},
	},
	"go": {
		defaultPort: "8080",
		files: map[string]string{
			"Dockerfile": `FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.* ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 go build -o /out/server .

FROM alpine:3.20
RUN adduser -D -H -u 1000 app
COPY --from=build /out/server /server
# The platform runs the task as uid 1000 with a read-only root filesystem;
# only /tmp is writable.
USER 1000
ENV PORT={{PORT}}
EXPOSE {{PORT}}
CMD ["/server"]
`,
			".dockerignore": dockerignoreCommon,
			"go.mod": `module github.com/javaBin/{{NAME}}

go 1.23
`,
			"main.go": `package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "{{PORT}}"
	}
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	})
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "Hello from {{NAME}}")
	})
	log.Printf("Listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
`,
		},
	},
}

// render returns the runtime's files with name and port filled in.
func (rc runtimeConfig) render(name, port string) map[string]string {
	r := strings.NewReplacer("{{NAME}}", name, "{{PORT}}", port)
	out := make(map[string]string, len(rc.files))
	for path, body := range rc.files {
		out[path] = r.Replace(body)
	}
	return out
}

// writeFiles writes files under dir and returns their paths, sorted.
func writeFiles(dir string, files map[string]string) ([]string, error) {
	var paths []string
	for path, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}
