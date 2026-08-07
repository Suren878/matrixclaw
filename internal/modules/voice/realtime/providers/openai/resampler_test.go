package openai

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestPCM16Resampler16kTo24k(t *testing.T) {
	input := make([]int16, 160)
	for index := range input {
		input[index] = 1200
	}
	resampler := newPCM16Resampler(16000, 24000)
	converted, err := resampler.Convert(samplesToBytes(input))
	if err != nil {
		t.Fatal(err)
	}
	converted = append(converted, resampler.Flush()...)
	if got, want := len(converted)/2, 240; got != want {
		t.Fatalf("output samples = %d, want %d", got, want)
	}
	for offset := 0; offset < len(converted); offset += 2 {
		if got := int16(binary.LittleEndian.Uint16(converted[offset:])); got != 1200 {
			t.Fatalf("sample %d = %d, want 1200", offset/2, got)
		}
	}
}

func TestPCM16ResamplerIsStableAcrossChunks(t *testing.T) {
	input := make([]int16, 161)
	for index := range input {
		input[index] = int16(index*100 - 8000)
	}
	allAtOnce := newPCM16Resampler(16000, 24000)
	want, err := allAtOnce.Convert(samplesToBytes(input))
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, allAtOnce.Flush()...)

	chunked := newPCM16Resampler(16000, 24000)
	var got []byte
	for _, samples := range [][]int16{input[:17], input[17:89], input[89:]} {
		part, err := chunked.Convert(samplesToBytes(samples))
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, part...)
	}
	got = append(got, chunked.Flush()...)
	if !bytes.Equal(got, want) {
		t.Fatalf("chunked conversion differs: got %d bytes, want %d", len(got), len(want))
	}
}

func TestPCM16ResamplerRejectsPartialSample(t *testing.T) {
	resampler := newPCM16Resampler(16000, 24000)
	if _, err := resampler.Convert([]byte{1}); err == nil {
		t.Fatal("expected odd PCM byte length to fail")
	}
}
