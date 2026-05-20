package diffview

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/zeebo/xxh3"
)

func (dv *DiffView) hightlightCode(source string, bgColor color.Color) string {
	if dv.chromaStyle == nil {
		return source
	}

	cacheKey := dv.createSyntaxCacheKey(source, bgColor)
	if cached, exists := dv.syntaxCache[cacheKey]; exists {
		return cached
	}

	l := dv.getChromaLexer()
	f := dv.getChromaFormatter(bgColor)

	it, err := l.Tokenise(nil, source)
	if err != nil {
		return source
	}

	var b strings.Builder
	if err := f.Format(&b, dv.chromaStyle, it); err != nil {
		return source
	}

	result := b.String()
	dv.syntaxCache[cacheKey] = result
	return result
}

// createSyntaxCacheKey creates a compact cache key from source and background.
func (dv *DiffView) createSyntaxCacheKey(source string, bgColor color.Color) string {
	r, g, b, a := bgColor.RGBA()
	colorStr := fmt.Sprintf("%d,%d,%d,%d", r, g, b, a)

	h := xxh3.New()
	_, _ = h.Write([]byte(source))
	_, _ = h.Write([]byte(colorStr))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func (dv *DiffView) getChromaLexer() chroma.Lexer {
	if dv.cachedLexer != nil {
		return dv.cachedLexer
	}

	l := lexers.Match(dv.before.path)
	if l == nil {
		l = lexers.Analyse(dv.before.content)
	}
	if l == nil {
		l = lexers.Fallback
	}
	dv.cachedLexer = chroma.Coalesce(l)
	return dv.cachedLexer
}

func (dv *DiffView) getChromaFormatter(bgColor color.Color) chroma.Formatter {
	return chromaFormatter{
		bgColor: bgColor,
	}
}
