package gateway

import "encoding/binary"

func pcm16kBytesFromPCM8k(in []int16) []byte {
	if len(in) == 0 {
		return nil
	}
	out := make([]byte, len(in)*4)
	for i, sample := range in {
		next := sample
		if i+1 < len(in) {
			next = in[i+1]
		}
		interpolated := int16((int(sample) + int(next)) / 2)
		binary.LittleEndian.PutUint16(out[i*4:], uint16(sample))
		binary.LittleEndian.PutUint16(out[i*4+2:], uint16(interpolated))
	}
	return out
}

type pcmToPCM8Resampler struct {
	pending    []int16
	nextCenter int
	step       int
	inputRate  int
}

type pcm24ToPCM8Resampler = pcmToPCM8Resampler

func newPCMToPCM8Resampler(inputRate int) *pcmToPCM8Resampler {
	if inputRate <= 0 {
		inputRate = 24000
	}
	step := inputRate / 8000
	if step <= 0 || inputRate%8000 != 0 {
		inputRate = 24000
		step = 3
	}
	return &pcmToPCM8Resampler{
		nextCenter: step / 2,
		step:       step,
		inputRate:  inputRate,
	}
}

func newPCM24ToPCM8Resampler() *pcm24ToPCM8Resampler {
	return newPCMToPCM8Resampler(24000)
}

func (r *pcmToPCM8Resampler) Convert(in []byte) []int16 {
	if r == nil {
		return pcm24BytesToPCM8k(in)
	}
	if len(in) < 2 {
		return nil
	}
	samples := len(in) / 2
	for i := 0; i < samples; i++ {
		r.pending = append(r.pending, int16(binary.LittleEndian.Uint16(in[i*2:])))
	}
	out := make([]int16, 0, len(r.pending)/r.step)
	for r.nextCenter+4 < len(r.pending) {
		out = append(out, lowpassSample(r.pending, r.nextCenter))
		r.nextCenter += r.step
	}
	r.compact()
	return out
}

func (r *pcmToPCM8Resampler) Flush() []int16 {
	if r == nil || len(r.pending) == 0 {
		return nil
	}
	out := make([]int16, 0, len(r.pending)/r.step+1)
	for r.nextCenter < len(r.pending) {
		out = append(out, lowpassSample(r.pending, r.nextCenter))
		r.nextCenter += r.step
	}
	r.pending = r.pending[:0]
	r.nextCenter = r.step / 2
	return out
}

func (r *pcmToPCM8Resampler) InputRate() int {
	if r == nil {
		return 0
	}
	return r.inputRate
}

func (r *pcmToPCM8Resampler) compact() {
	keepFrom := r.nextCenter - 4
	if keepFrom <= 0 {
		return
	}
	copy(r.pending, r.pending[keepFrom:])
	r.pending = r.pending[:len(r.pending)-keepFrom]
	r.nextCenter -= keepFrom
}

func pcm24BytesToPCM8k(in []byte) []int16 {
	return newPCM24ToPCM8Resampler().Convert(in)
}

func lowpassSample(samples []int16, center int) int16 {
	weights := [...]int{1, 2, 3, 4, 5, 4, 3, 2, 1}
	sum := 0
	weightSum := 0
	for i, weight := range weights {
		index := center + i - len(weights)/2
		if index < 0 || index >= len(samples) {
			continue
		}
		sum += int(samples[index]) * weight
		weightSum += weight
	}
	if weightSum == 0 {
		return 0
	}
	return int16(sum / weightSum)
}

func alawDecode(value byte) int16 {
	value ^= 0x55
	t := int16(value&0x0f) << 4
	seg := (value & 0x70) >> 4
	switch seg {
	case 0:
		t += 8
	case 1:
		t += 0x108
	default:
		t += 0x108
		t <<= seg - 1
	}
	if value&0x80 != 0 {
		return t
	}
	return -t
}

func alawEncode(sample int16) byte {
	pcm := int(sample)
	mask := byte(0xD5)
	if pcm < 0 {
		mask = 0x55
		pcm = -pcm - 8
		if pcm < 0 {
			pcm = 0
		}
	}
	if pcm > 32635 {
		pcm = 32635
	}
	var aval byte
	if pcm >= 256 {
		seg := alawSegment(pcm)
		aval = byte(seg << 4)
		aval |= byte((pcm >> (seg + 3)) & 0x0f)
	} else {
		aval = byte(pcm >> 4)
	}
	return aval ^ mask
}

func alawSegment(pcm int) int {
	for i, end := range []int{0xFF, 0x1FF, 0x3FF, 0x7FF, 0xFFF, 0x1FFF, 0x3FFF, 0x7FFF} {
		if pcm <= end {
			return i
		}
	}
	return 7
}
