package setup

type rect struct {
	x int
	y int
	w int
	h int
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func centerRect(areaW, areaH, innerW, innerH int) rect {
	if areaW < 0 {
		areaW = 0
	}
	if areaH < 0 {
		areaH = 0
	}
	if innerW < 0 {
		innerW = 0
	}
	if innerH < 0 {
		innerH = 0
	}

	x := max(0, (areaW-innerW)/2)
	y := max(0, (areaH-innerH)/2)
	return rect{x: x, y: y, w: innerW, h: innerH}
}
