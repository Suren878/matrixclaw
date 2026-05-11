package setup

import (
	"math/rand"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
)

type tickMsg time.Time

type rainColumn struct {
	head      int
	length    int
	speed     int
	delay     int
	gap       int
	enabled   bool
	flicker   int
	glyphBias int
}

type rainGlyphSet struct {
	raw        []rune
	head       []string
	brightTail []string
	tail       []string
	dim        []string
}

var (
	rainGlyphsOnce sync.Once
	rainGlyphs     rainGlyphSet
)

func tickCmd() tea.Cmd {
	return tea.Tick(90*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m *model) syncRain(width, height int) {
	if width <= 0 || height <= 0 {
		return
	}
	if len(m.rain) == width {
		return
	}
	m.rain = make([]rainColumn, width)
	for i := range m.rain {
		m.rain[i] = newRainColumn(height, true)
	}
}

func (m *model) updateRain() {
	if len(m.rain) == 0 || m.height <= 0 {
		return
	}
	m.tickCount++
	for i := range m.rain {
		col := &m.rain[i]
		if !col.enabled {
			if col.delay > 0 {
				col.delay--
				continue
			}
			m.rain[i] = newRainColumn(m.height, false)
			continue
		}
		if col.delay > 0 {
			col.delay--
			continue
		}
		if m.tickCount%col.speed == 0 {
			col.head++
		}
		if col.flicker > 0 && m.tickCount%col.flicker == 0 {
			col.glyphBias++
		}
		if col.head-col.length > m.height+col.gap {
			m.rain[i] = newRainColumn(m.height, false)
		}
	}
}

func newRainColumn(height int, warmStart bool) rainColumn {
	if height < 8 {
		height = 8
	}
	enabled := rand.Intn(100) < 90
	if !enabled {
		return rainColumn{enabled: false, delay: 1 + rand.Intn(max(3, height/3))}
	}
	maxLength := max(10, min(height-2, 24))
	length := 8 + rand.Intn(maxLength-7)
	head := -rand.Intn(max(4, height*2))
	delay := rand.Intn(6)
	if warmStart {
		head = rand.Intn(height + length)
		delay = 0
	}
	return rainColumn{
		head:      head,
		length:    length,
		speed:     1 + rand.Intn(2),
		delay:     delay,
		gap:       1 + rand.Intn(max(4, height/4)),
		enabled:   true,
		flicker:   2 + rand.Intn(3),
		glyphBias: rand.Intn(32),
	}
}

func (m *model) renderRain(areaW, areaH int, exclude rect) string {
	width := max(1, areaW)
	height := max(1, areaH)
	glyphs := styledRainGlyphs()
	lines := make([]string, 0, height)
	for row := 0; row < height; row++ {
		var line strings.Builder
		line.Grow(width * 8)
		for col := 0; col < width; col++ {
			if col >= exclude.x && col < exclude.x+exclude.w && row >= exclude.y && row < exclude.y+exclude.h {
				line.WriteString(" ")
				continue
			}
			if col >= len(m.rain) {
				line.WriteString(" ")
				continue
			}
			column := m.rain[col]
			if !column.enabled {
				line.WriteString(" ")
				continue
			}
			distance := column.head - row
			switch {
			case distance == 0:
				line.WriteString(rainGlyphAt(glyphs.head, column.glyphBias+row+col))
			case distance > 0 && distance <= 1:
				line.WriteString(rainGlyphAt(glyphs.brightTail, column.glyphBias+row))
			case distance > 1 && distance <= 4:
				line.WriteString(rainGlyphAt(glyphs.tail, column.glyphBias+col))
			case distance > 4 && distance < column.length:
				line.WriteString(rainGlyphAt(glyphs.dim, column.glyphBias+row+m.tickCount))
			default:
				line.WriteString(" ")
			}
		}
		lines = append(lines, line.String())
	}
	return strings.Join(lines, "\n")
}

func styledRainGlyphs() rainGlyphSet {
	rainGlyphsOnce.Do(func() {
		raw := []rune("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZｱｲｳｴｵｶｷｸｹｺｻｼｽｾｿﾀﾁﾂﾃﾄﾅﾆﾇﾈﾉﾊﾋﾌﾍﾎﾏﾐﾑﾒﾓﾔﾕﾖﾗﾘﾙﾚﾛﾜ")
		rainGlyphs = rainGlyphSet{
			raw:        raw,
			head:       renderRainGlyphs(raw, rainHeadStyle.Render),
			brightTail: renderRainGlyphs(raw, rainBrightTailStyle.Render),
			tail:       renderRainGlyphs(raw, rainTailStyle.Render),
			dim:        renderRainGlyphs(raw, rainDimStyle.Render),
		}
	})
	return rainGlyphs
}

func renderRainGlyphs(glyphs []rune, render func(...string) string) []string {
	styled := make([]string, len(glyphs))
	for i, glyph := range glyphs {
		styled[i] = render(string(glyph))
	}
	return styled
}

func rainGlyphAt(glyphs []string, seed int) string {
	if len(glyphs) == 0 {
		return " "
	}
	return glyphs[rainGlyphIndex(seed, len(glyphs))]
}

func rainGlyphIndex(seed int, size int) int {
	if size <= 0 {
		return 0
	}
	index := seed % size
	if index < 0 {
		index += size
	}
	return index
}
