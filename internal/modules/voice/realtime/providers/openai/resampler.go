package openai

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
)

type pcm16Resampler struct {
	inputRate  int64
	outputRate int64
	pending    []int16
	next       int64
}

func newPCM16Resampler(inputRate int, outputRate int) *pcm16Resampler {
	if inputRate <= 0 {
		inputRate = 16000
	}
	if outputRate <= 0 {
		outputRate = 24000
	}
	return &pcm16Resampler{
		inputRate:  int64(inputRate),
		outputRate: int64(outputRate),
	}
}

func (r *pcm16Resampler) Convert(input []byte) ([]byte, error) {
	if len(input)%2 != 0 {
		return nil, errors.New("PCM16 input has an odd byte length")
	}
	for offset := 0; offset < len(input); offset += 2 {
		r.pending = append(r.pending, int16(binary.LittleEndian.Uint16(input[offset:])))
	}
	return r.convert(false), nil
}

func (r *pcm16Resampler) Flush() []byte {
	if r == nil {
		return nil
	}
	output := r.convert(true)
	r.pending = r.pending[:0]
	r.next = 0
	return output
}

func (r *pcm16Resampler) convert(flush bool) []byte {
	if r == nil || len(r.pending) == 0 {
		return nil
	}
	if r.inputRate == r.outputRate {
		output := samplesToBytes(r.pending)
		r.pending = r.pending[:0]
		r.next = 0
		return output
	}

	output := make([]int16, 0, len(r.pending)*int(r.outputRate)/int(r.inputRate)+1)
	for {
		index := r.next / r.outputRate
		fraction := r.next % r.outputRate
		if index >= int64(len(r.pending)) {
			break
		}
		if index+1 >= int64(len(r.pending)) && fraction != 0 && !flush {
			break
		}
		left := r.pending[index]
		right := left
		if index+1 < int64(len(r.pending)) {
			right = r.pending[index+1]
		}
		interpolated := int64(left)*(r.outputRate-fraction) + int64(right)*fraction
		output = append(output, int16(interpolated/r.outputRate))
		r.next += r.inputRate
	}

	drop := r.next / r.outputRate
	if drop > 0 && drop < int64(len(r.pending)) {
		copy(r.pending, r.pending[drop:])
		r.pending = r.pending[:int64(len(r.pending))-drop]
		r.next -= drop * r.outputRate
	} else if drop >= int64(len(r.pending)) {
		r.pending = r.pending[:0]
		r.next = 0
	}
	return samplesToBytes(output)
}

func samplesToBytes(samples []int16) []byte {
	if len(samples) == 0 {
		return nil
	}
	output := make([]byte, len(samples)*2)
	for index, sample := range samples {
		binary.LittleEndian.PutUint16(output[index*2:], uint16(sample))
	}
	return output
}

func decodeBase64Audio(value string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(value)
}
