// Package repomap draws a ranked map of a repository's definitions, as
// Aider's repo map does, so a model can find its way in a large codebase.
// Go files are parsed with go/parser, other languages lexed with chroma.
package repomap

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

const (
	// defaultTokens is the map's budget when the request names none.
	defaultTokens = 2048
	// maxRoots is how many directories keep their tags in memory.
	maxRoots = 8
)

// File is a source file to map, by its slash path below the root, with the
// size and modification time that key its cached tags.
type File struct {
	Path    string
	Size    int64
	ModTime time.Time
}

// Request asks for the map of Files below Root. FocusFiles (slash paths
// below Root) are the files the work is about: what they use ranks higher and
// they are left out of the map. FocusSymbols are identifiers the work names.
// Tokens is the map's budget, 2048 when zero.
type Request struct {
	Root         string
	Files        []File
	FocusFiles   []string
	FocusSymbols []string
	Tokens       int
}

// Result is a drawn map: its text, about Tokens long, showing Shown of the
// Mapped files that hold definitions.
type Result struct {
	Text   string
	Tokens int
	Shown  int
	Mapped int
}

// Mapper keeps the tags of recently mapped directories, so a map of an
// unchanged tree reads no file again; safe for concurrent use.
type Mapper struct {
	mu    sync.Mutex
	roots []*rootCache
}

type rootCache struct {
	root  string
	mu    sync.Mutex
	files map[string]cachedFile
}

type cachedFile struct {
	size int64
	mod  time.Time
	tags fileTags
}

// Map draws the map Request asks for.
func (m *Mapper) Map(ctx context.Context, req Request) (Result, error) {
	cache := m.root(req.Root)
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if err := cache.refresh(ctx, req.Files); err != nil {
		return Result{}, err
	}

	var paths []string
	var tags []fileTags
	index := map[string]int{}
	for _, file := range req.Files {
		cached := cache.files[file.Path]
		if len(cached.tags.defs) == 0 && len(cached.tags.refs) == 0 {
			continue
		}
		index[file.Path] = len(paths)
		paths = append(paths, file.Path)
		tags = append(tags, cached.tags)
	}
	focus := map[int]bool{}
	for _, rel := range req.FocusFiles {
		if i, ok := index[rel]; ok {
			focus[i] = true
		}
	}
	symbols := map[string]bool{}
	for _, symbol := range req.FocusSymbols {
		symbols[symbol] = true
	}
	ranked := rankTags(paths, tags, focus, symbols)
	budget := req.Tokens
	if budget <= 0 {
		budget = defaultTokens
	}
	text, tokens, count := fit(paths, tags, ranked, budget)
	shown := map[int]bool{}
	for _, tag := range ranked[:count] {
		shown[tag.file] = true
	}
	return Result{Text: text, Tokens: tokens, Shown: len(shown), Mapped: len(paths)}, nil
}

// root is the cache of a directory, made most recently used.
func (m *Mapper) root(root string) *rootCache {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, cache := range m.roots {
		if cache.root == root {
			copy(m.roots[1:i+1], m.roots[:i])
			m.roots[0] = cache
			return cache
		}
	}
	cache := &rootCache{root: root, files: map[string]cachedFile{}}
	m.roots = append([]*rootCache{cache}, m.roots[:min(len(m.roots), maxRoots-1)]...)
	return cache
}

// refresh reads the tags of files that are new or changed since they were
// cached, in parallel, and forgets files no longer listed.
func (c *rootCache) refresh(ctx context.Context, files []File) error {
	listed := make(map[string]bool, len(files))
	var stale []File
	for _, file := range files {
		listed[file.Path] = true
		if cached, ok := c.files[file.Path]; !ok || cached.size != file.Size || !cached.mod.Equal(file.ModTime) {
			stale = append(stale, file)
		}
	}
	for rel := range c.files {
		if !listed[rel] {
			delete(c.files, rel)
		}
	}
	read := make([]fileTags, len(stale))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), len(stale)) {
		wg.Go(func() {
			for i := range jobs {
				read[i] = c.read(stale[i])
			}
		})
	}
	var err error
	for i := range stale {
		if err = ctx.Err(); err != nil {
			break
		}
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	if err != nil {
		return err
	}
	for i, file := range stale {
		c.files[file.Path] = cachedFile{size: file.Size, mod: file.ModTime, tags: read[i]}
	}
	return nil
}

func (c *rootCache) read(file File) fileTags {
	if file.Size > maxFileBytes || !Supported(file.Path) {
		return fileTags{}
	}
	src, err := os.ReadFile(filepath.Join(c.root, filepath.FromSlash(file.Path)))
	if err != nil {
		return fileTags{}
	}
	return extract(file.Path, src)
}
