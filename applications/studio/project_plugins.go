package studio

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"horizon/foundation/kernel/plugin"
)

// Only checked-in module manifests are inspected. This does not run project code
// or infer installation state from a declared plugin.
func projectPlugins(project projectRef) ([]plugin.Manifest, error) {
	root, err := projectRoot(project.Root)
	if err != nil {
		return nil, err
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	modules, err := dir.Open("applications")
	if errors.Is(err, os.ErrNotExist) {
		return []plugin.Manifest{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer modules.Close()
	entries, err := modules.ReadDir(1025)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(entries) > 1024 {
		return nil, errors.New("too many project modules")
	}
	result := []plugin.Manifest{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join("applications", entry.Name(), "plugin.yaml")
		info, err := dir.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Size() > 64<<10 {
			return nil, errors.New("invalid project plugin manifest")
		}
		file, err := dir.Open(path)
		if err != nil {
			return nil, err
		}
		raw, readErr := io.ReadAll(io.LimitReader(file, (64<<10)+1))
		closeErr := file.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return nil, err
		}
		manifest, err := plugin.ParseManifest(raw)
		if err != nil || manifest.ID == "" || manifest.Version == "" || len(raw) > 64<<10 {
			return nil, errors.New("invalid project plugin manifest")
		}
		result = append(result, manifest)
	}
	slices.SortFunc(result, func(a, b plugin.Manifest) int {
		return strings.Compare(a.ID+"@"+a.Version, b.ID+"@"+b.Version)
	})
	return result, nil
}
