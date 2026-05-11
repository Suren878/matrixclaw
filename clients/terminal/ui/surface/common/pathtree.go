package common

import (
	"sort"
	"strings"
)

func RenderPathTree(paths ...string) string {
	groups := make(map[string]map[string]struct{})

	for _, raw := range paths {
		path := normalizeTreePath(raw)
		if path == "" {
			continue
		}

		dir, file := splitTreePath(path)
		if file == "" {
			continue
		}
		if _, ok := groups[dir]; !ok {
			groups[dir] = make(map[string]struct{})
		}
		groups[dir][file] = struct{}{}
	}

	if len(groups) == 0 {
		return ""
	}

	dirs := make([]string, 0, len(groups))
	for dir := range groups {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	blocks := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		files := make([]string, 0, len(groups[dir]))
		for file := range groups[dir] {
			files = append(files, file)
		}
		sort.Strings(files)

		lines := make([]string, 0, len(files)+1)
		if dir != "" {
			lines = append(lines, "  "+dir+"/")
		}
		for i, file := range files {
			branch := "├"
			if i == len(files)-1 {
				branch = "└"
			}
			prefix := "    "
			if dir == "" {
				prefix = "  "
			}
			lines = append(lines, prefix+branch+" "+file)
		}

		blocks = append(blocks, strings.Join(lines, "\n"))
	}

	return strings.Join(blocks, "\n\n")
}

func normalizeTreePath(path string) string {
	path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	path = strings.TrimSuffix(path, "/")
	return path
}

func splitTreePath(path string) (dir string, file string) {
	index := strings.LastIndex(path, "/")
	if index < 0 {
		return "", path
	}
	dir = strings.TrimSpace(path[:index])
	dir = strings.TrimSuffix(dir, "/")
	return dir, strings.TrimSpace(path[index+1:])
}
