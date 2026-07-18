package service

import (
	"archive/zip"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

var canonicalTimezoneNames = struct {
	sync.Once
	names map[string]string
}{}

func resolveIANATimezone(name string) (string, *time.Location, error) {
	name = strings.TrimSpace(name)
	if strings.EqualFold(name, "Local") {
		return "", nil, fmt.Errorf("unknown time zone %s", name)
	}
	location, err := time.LoadLocation(name)
	if err == nil {
		return name, location, nil
	}

	canonicalTimezoneNames.Do(loadCanonicalTimezoneNames)
	canonical, ok := canonicalTimezoneNames.names[strings.ToLower(name)]
	if !ok {
		return "", nil, err
	}
	location, canonicalErr := time.LoadLocation(canonical)
	if canonicalErr != nil {
		return "", nil, canonicalErr
	}
	return canonical, location, nil
}

func loadCanonicalTimezoneNames() {
	names := map[string]string{"utc": "UTC"}
	for _, source := range timezoneSources() {
		info, err := os.Stat(source)
		if err != nil {
			continue
		}
		if info.IsDir() {
			loadTimezoneNamesFromDirectory(names, source)
			continue
		}
		loadTimezoneNamesFromZip(names, source)
	}
	canonicalTimezoneNames.names = names
}

func timezoneSources() []string {
	sources := make([]string, 0, 6)
	if source := os.Getenv("ZONEINFO"); source != "" {
		sources = append(sources, source)
	}
	sources = append(sources,
		"/usr/share/zoneinfo",
		"/usr/share/lib/zoneinfo",
		"/usr/lib/locale/TZ",
		"/etc/zoneinfo",
	)
	// This development fallback locates the zoneinfo bundled with the active Go toolchain.
	// Production images resolve system tzdata before reaching it.
	//nolint:staticcheck // runtime.GOROOT remains the only in-process path to Go's bundled zoneinfo.
	if root := runtime.GOROOT(); root != "" {
		sources = append(sources, filepath.Join(root, "lib", "time", "zoneinfo.zip"))
	}
	return sources
}

func loadTimezoneNamesFromDirectory(names map[string]string, root string) {
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return nil
		}
		name, err := filepath.Rel(root, path)
		if err == nil {
			addCanonicalTimezoneName(names, filepath.ToSlash(name))
		}
		return nil
	})
}

func loadTimezoneNamesFromZip(names map[string]string, path string) {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return
	}
	defer func() { _ = archive.Close() }()
	for _, file := range archive.File {
		if !file.FileInfo().IsDir() {
			addCanonicalTimezoneName(names, file.Name)
		}
	}
}

func addCanonicalTimezoneName(names map[string]string, name string) {
	key := strings.ToLower(name)
	if _, exists := names[key]; !exists {
		names[key] = name
	}
}
