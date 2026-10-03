package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ProfileProject reads bounded allowlisted manifests, not source trees, secrets or contributor history.
func ProfileProject(root, project string) (Profile, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Profile{}, err
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return Profile{}, fmt.Errorf("project root must be a directory")
	}
	p := Profile{Project: project, Features: map[string][]string{}, Evidence: map[string][]string{}}
	add := func(k, v, path string) {
		if !overlap([]string{v}, p.Features[k]) {
			p.Features[k] = append(p.Features[k], v)
		}
		p.Evidence[k+"="+v] = append(p.Evidence[k+"="+v], path)
	}
	read := func(name string) ([]byte, bool, error) {
		path := filepath.Join(root, name)
		fi, e := os.Lstat(path)
		if os.IsNotExist(e) {
			return nil, false, nil
		}
		if e != nil {
			return nil, false, e
		}
		if !fi.Mode().IsRegular() || fi.Size() > 128<<10 {
			return nil, false, fmt.Errorf("refusing nonregular or oversized profile file %s", name)
		}
		b, e := os.ReadFile(path)
		return b, e == nil, e
	}
	data, ok, err := read("package.json")
	if err != nil {
		return p, err
	}
	if ok {
		var manifest struct {
			Dependencies map[string]string `json:"dependencies"`
			Dev          map[string]string `json:"devDependencies"`
		}
		if err = json.Unmarshal(data, &manifest); err != nil {
			return p, fmt.Errorf("package.json: %w", err)
		}
		deps := map[string]bool{}
		for k := range manifest.Dependencies {
			deps[k] = true
		}
		for k := range manifest.Dev {
			deps[k] = true
		}
		add("language", "javascript", "package.json")
		if deps["typescript"] {
			add("language", "typescript", "package.json")
		}
		for _, k := range []string{"next", "react", "express", "vue", "svelte", "nuxt"} {
			if deps[k] {
				v := k
				if k == "next" {
					v = "nextjs"
					add("app", "web", "package.json")
				}
				add("framework", v, "package.json")
			}
		}
		for k := range deps {
			switch {
			case strings.HasPrefix(k, "@aws-sdk/"):
				add("cloud", "aws", "package.json")
			case strings.HasPrefix(k, "@azure/"):
				add("cloud", "azure", "package.json")
			}
		}
		for dep, db := range map[string]string{"pg": "postgres", "postgres": "postgres", "mysql2": "mysql", "better-sqlite3": "sqlite", "mongodb": "mongodb", "mongoose": "mongodb", "snowflake-sdk": "snowflake", "redis": "redis", "ioredis": "redis"} {
			if deps[dep] {
				add("database", db, "package.json")
				model := "sql"
				if db == "mongodb" || db == "redis" {
					model = "nosql"
				}
				add("data_model", model, "package.json")
			}
		}
	}
	for file, lang := range map[string]string{"go.mod": "go", "Cargo.toml": "rust", "pyproject.toml": "python", "tsconfig.json": "typescript"} {
		_, ok, e := read(file)
		if e != nil {
			return p, e
		}
		if ok {
			add("language", lang, file)
		}
	}
	// Explicit feature overrides cover intent, reviewer identities and policy. Values are literal labels.
	configName := ".elephant.json"
	data, ok, err = read(configName)
	if err != nil {
		return p, err
	}
	if !ok {
		configName = ".agent-memory.json" // Read legacy settings only when the new file is absent.
		data, ok, err = read(configName)
		if err != nil {
			return p, err
		}
	}
	if ok {
		var explicit struct {
			Features map[string][]string `json:"features"`
		}
		if err = json.Unmarshal(data, &explicit); err != nil {
			return p, err
		}
		if err = ValidateLabels(explicit.Features); err != nil {
			return p, err
		}
		for k, values := range explicit.Features {
			for _, v := range values {
				add(k, strings.ToLower(v), configName)
			}
		}
	}
	for k := range p.Features {
		sort.Strings(p.Features[k])
	}
	return p, nil
}
