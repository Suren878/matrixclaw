// Package anim provides an animated spinner.
package anim

import (
	"fmt"
	"image/color"
	"math/rand/v2"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/lucasb-eyer/go-colorful"
	"github.com/zeebo/xxh3"
)

const (
	fps           = 20
	initialChar   = '.'
	labelGap      = " "
	labelGapWidth = 1

	ellipsisAnimSpeed      = 8
	maxBirthOffset         = time.Second
	prerenderedFrames      = 10
	defaultNumCyclingChars = 10
)

var (
	defaultGradColorA = color.RGBA{R: 0xff, G: 0, B: 0, A: 0xff}
	defaultGradColorB = color.RGBA{R: 0, G: 0, B: 0xff, A: 0xff}
	defaultLabelColor = color.RGBA{R: 0xcc, G: 0xcc, B: 0xcc, A: 0xff}
)

var (
	availableRunes = []rune("0123456789abcdefABCDEF~!@#$£€%^&*()+=_")
	ellipsisFrames = []string{".", "..", "...", ""}
)

var lastID atomic.Int64

func nextID() int {
	return int(lastID.Add(1))
}

type animCache struct {
	initialFrames  [][]string
	cyclingFrames  [][]string
	width          int
	labelWidth     int
	label          []string
	ellipsisFrames []string
}

var animCacheMap sync.Map

func settingsHash(opts Settings) string {
	h := xxh3.New()
	fmt.Fprintf(h, "%d-%s-%v-%v-%v-%t",
		opts.Size, opts.Label, opts.LabelColor, opts.GradColorA, opts.GradColorB, opts.CycleColors)
	return fmt.Sprintf("%x", h.Sum(nil))
}

// StepMsg is a message type used to trigger the next step in the animation.
type StepMsg struct{ ID string }

// Settings defines settings for the animation.
type Settings struct {
	ID          string
	Size        int
	Label       string
	LabelColor  color.Color
	GradColorA  color.Color
	GradColorB  color.Color
	CycleColors bool
}

// Anim is a Bubble for an animated spinner.
type Anim struct {
	width            int
	cyclingCharWidth int
	label            []string
	labelWidth       int
	labelColor       color.Color
	startTime        time.Time
	birthOffsets     []time.Duration
	initialFrames    [][]string
	initialized      atomic.Bool
	cyclingFrames    [][]string
	step             atomic.Int64
	ellipsisStep     atomic.Int64
	ellipsisFrames   []string
	id               string
}

// New creates a new Anim instance with the specified width and label.
func New(opts Settings) *Anim {
	a := &Anim{}
	if opts.Size < 1 {
		opts.Size = defaultNumCyclingChars
	}
	if colorIsUnset(opts.GradColorA) {
		opts.GradColorA = defaultGradColorA
	}
	if colorIsUnset(opts.GradColorB) {
		opts.GradColorB = defaultGradColorB
	}
	if colorIsUnset(opts.LabelColor) {
		opts.LabelColor = defaultLabelColor
	}

	if opts.ID != "" {
		a.id = opts.ID
	} else {
		a.id = fmt.Sprintf("%d", nextID())
	}
	a.startTime = time.Now()
	a.cyclingCharWidth = opts.Size
	a.labelColor = opts.LabelColor

	cacheKey := settingsHash(opts)
	if value, exists := animCacheMap.Load(cacheKey); exists {
		cached := value.(*animCache)
		a.width = cached.width
		a.labelWidth = cached.labelWidth
		a.label = append([]string(nil), cached.label...)
		a.ellipsisFrames = append([]string(nil), cached.ellipsisFrames...)
		a.initialFrames = cached.initialFrames
		a.cyclingFrames = cached.cyclingFrames
	} else {
		a.labelWidth = lipgloss.Width(opts.Label)
		a.width = opts.Size
		if opts.Label != "" {
			a.width += labelGapWidth + lipgloss.Width(opts.Label)
		}

		a.renderLabel(opts.Label)

		var ramp []color.Color
		numFrames := prerenderedFrames
		if opts.CycleColors {
			ramp = makeGradientRamp(a.width*3, opts.GradColorA, opts.GradColorB, opts.GradColorA, opts.GradColorB)
			numFrames = a.width * 2
		} else {
			ramp = makeGradientRamp(a.width, opts.GradColorA, opts.GradColorB)
		}

		a.initialFrames = make([][]string, numFrames)
		offset := 0
		for i := range a.initialFrames {
			a.initialFrames[i] = make([]string, a.width+labelGapWidth+a.labelWidth)
			for j := range a.initialFrames[i] {
				if j+offset >= len(ramp) {
					continue
				}
				var c color.Color
				if j <= a.cyclingCharWidth {
					c = ramp[j+offset]
				} else {
					c = opts.LabelColor
				}
				a.initialFrames[i][j] = lipgloss.NewStyle().
					Foreground(c).
					Render(string(initialChar))
			}
			if opts.CycleColors {
				offset++
			}
		}

		a.cyclingFrames = make([][]string, numFrames)
		offset = 0
		for i := range a.cyclingFrames {
			a.cyclingFrames[i] = make([]string, a.width)
			for j := range a.cyclingFrames[i] {
				if j+offset >= len(ramp) {
					continue
				}
				r := availableRunes[rand.IntN(len(availableRunes))]
				a.cyclingFrames[i][j] = lipgloss.NewStyle().
					Foreground(ramp[j+offset]).
					Render(string(r))
			}
			if opts.CycleColors {
				offset++
			}
		}

		animCacheMap.Store(cacheKey, &animCache{
			initialFrames:  a.initialFrames,
			cyclingFrames:  a.cyclingFrames,
			width:          a.width,
			labelWidth:     a.labelWidth,
			label:          append([]string(nil), a.label...),
			ellipsisFrames: append([]string(nil), a.ellipsisFrames...),
		})
	}

	a.birthOffsets = make([]time.Duration, a.width)
	for i := range a.birthOffsets {
		a.birthOffsets[i] = time.Duration(rand.N(int64(maxBirthOffset))) * time.Nanosecond
	}

	return a
}

// SetLabel updates the label text and re-renders it.
func (a *Anim) SetLabel(newLabel string) {
	a.labelWidth = lipgloss.Width(newLabel)
	a.width = a.cyclingCharWidth
	if newLabel != "" {
		a.width += labelGapWidth + a.labelWidth
	}
	a.renderLabel(newLabel)
}

// renderLabel renders the label with the current label color.
func (a *Anim) renderLabel(label string) {
	if a.labelWidth > 0 {
		labelRunes := []rune(label)
		a.label = make([]string, 0, len(labelRunes))
		for i := range labelRunes {
			rendered := lipgloss.NewStyle().
				Foreground(a.labelColor).
				Render(string(labelRunes[i]))
			a.label = append(a.label, rendered)
		}

		a.ellipsisFrames = make([]string, 0, len(ellipsisFrames))
		for _, frame := range ellipsisFrames {
			rendered := lipgloss.NewStyle().
				Foreground(a.labelColor).
				Render(frame)
			a.ellipsisFrames = append(a.ellipsisFrames, rendered)
		}
	} else {
		a.label = nil
		a.ellipsisFrames = nil
	}
}

// Width returns the total width of the animation.
func (a *Anim) Width() (w int) {
	w = a.width
	if a.labelWidth > 0 {
		w += labelGapWidth + a.labelWidth

		var widestEllipsisFrame int
		for _, f := range ellipsisFrames {
			fw := lipgloss.Width(f)
			if fw > widestEllipsisFrame {
				widestEllipsisFrame = fw
			}
		}
		w += widestEllipsisFrame
	}
	return w
}

// Start starts the animation.
func (a *Anim) Start() tea.Cmd {
	return a.Step()
}

// Animate advances the animation to the next step.
func (a *Anim) Animate(msg StepMsg) tea.Cmd {
	if msg.ID != a.id {
		return nil
	}

	step := a.step.Add(1)
	if int(step) >= len(a.cyclingFrames) {
		a.step.Store(0)
	}

	if a.initialized.Load() && a.labelWidth > 0 {
		ellipsisStep := a.ellipsisStep.Add(1)
		if int(ellipsisStep) >= ellipsisAnimSpeed*len(ellipsisFrames) {
			a.ellipsisStep.Store(0)
		}
	} else if !a.initialized.Load() && time.Since(a.startTime) >= maxBirthOffset {
		a.initialized.Store(true)
	}
	return a.Step()
}

// Render renders the current state of the animation.
func (a *Anim) Render() string {
	var b strings.Builder
	step := int(a.step.Load())
	for i := range a.width {
		switch {
		case !a.initialized.Load() && i < len(a.birthOffsets) && time.Since(a.startTime) < a.birthOffsets[i]:
			b.WriteString(a.initialFrames[step][i])
		case i < a.cyclingCharWidth:
			b.WriteString(a.cyclingFrames[step][i])
		case i == a.cyclingCharWidth:
			b.WriteString(labelGap)
		case i > a.cyclingCharWidth:
			index := i - a.cyclingCharWidth - labelGapWidth
			if index >= 0 && index < len(a.label) {
				b.WriteString(a.label[index])
			}
		}
	}
	if a.initialized.Load() && a.labelWidth > 0 {
		ellipsisStep := int(a.ellipsisStep.Load())
		index := ellipsisStep / ellipsisAnimSpeed
		if index >= 0 && index < len(a.ellipsisFrames) {
			b.WriteString(a.ellipsisFrames[index])
		}
	}

	return b.String()
}

// RenderDots renders a compact label with animated trailing dots.
func (a *Anim) RenderDots(label string) string {
	step := int(a.step.Load())
	index := (step / max(1, fps/3)) % len(ellipsisFrames)
	return lipgloss.NewStyle().
		Foreground(a.labelColor).
		Render(label + ellipsisFrames[index])
}

// Step is a command that triggers the next step in the animation.
func (a *Anim) Step() tea.Cmd {
	return tea.Tick(time.Second/time.Duration(fps), func(t time.Time) tea.Msg {
		return StepMsg{ID: a.id}
	})
}

// makeGradientRamp returns a slice of colors blended between the given keys.
func makeGradientRamp(size int, stops ...color.Color) []color.Color {
	if len(stops) < 2 {
		return nil
	}

	points := make([]colorful.Color, len(stops))
	for i, k := range stops {
		points[i], _ = colorful.MakeColor(k)
	}

	numSegments := len(stops) - 1
	if numSegments == 0 {
		return nil
	}
	blended := make([]color.Color, 0, size)

	segmentSizes := make([]int, numSegments)
	baseSize := size / numSegments
	remainder := size % numSegments

	for i := range numSegments {
		segmentSizes[i] = baseSize
		if i < remainder {
			segmentSizes[i]++
		}
	}

	for i := range numSegments {
		c1 := points[i]
		c2 := points[i+1]
		segmentSize := segmentSizes[i]

		for j := range segmentSize {
			if segmentSize == 0 {
				continue
			}
			t := float64(j) / float64(segmentSize)
			c := c1.BlendHcl(c2, t)
			blended = append(blended, c)
		}
	}

	return blended
}

func colorIsUnset(c color.Color) bool {
	if c == nil {
		return true
	}
	_, _, _, a := c.RGBA()
	return a == 0
}
