package desktop

import (
	"bytes"
	"testing"
)

func TestAnnexBSplitter(t *testing.T) {
	sps := []byte{0x67, 0x64, 0x00, 0x33, 0xAA}
	pps := []byte{0x68, 0xCE, 0x38, 0x80}
	// first_mb_in_slice == 0 (MSB 1 => 0x88)
	idr := []byte{0x65, 0x88, 0x84, 0x21}
	// multi-slice frame: slice 1 has first_mb_in_slice == 0 (0x9A), slice 2 has first_mb_in_slice > 0 (0x0A)
	deltaSlice1 := []byte{0x41, 0x9A, 0x22, 0x11}
	deltaSlice2 := []byte{0x41, 0x0A, 0x33, 0x44}
	// next frame starts with first_mb_in_slice == 0 (0x9B)
	deltaNext := []byte{0x41, 0x9B, 0x55, 0x66}

	var stream bytes.Buffer
	// 4-byte and 3-byte start codes mixed
	stream.Write([]byte{0x00, 0x00, 0x00, 0x01})
	stream.Write(sps)
	stream.Write([]byte{0x00, 0x00, 0x01})
	stream.Write(pps)
	stream.Write([]byte{0x00, 0x00, 0x00, 0x01})
	stream.Write(idr)
	stream.Write([]byte{0x00, 0x00, 0x01})
	stream.Write(deltaSlice1)
	stream.Write([]byte{0x00, 0x00, 0x01})
	stream.Write(deltaSlice2)
	stream.Write([]byte{0x00, 0x00, 0x00, 0x01})
	stream.Write(deltaNext)

	raw := stream.Bytes()
	var got []H264Chunk
	sp := &AnnexBSplitter{}
	// Feed in small arbitrary chunks (5 bytes at a time)
	for i := 0; i < len(raw); i += 5 {
		end := i + 5
		if end > len(raw) {
			end = len(raw)
		}
		if err := sp.Push(raw[i:end], func(c H264Chunk) error {
			got = append(got, c)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := sp.Flush(func(c H264Chunk) error {
		got = append(got, c)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if len(got) != 3 {
		t.Fatalf("expected 3 chunks (key + multi-slice delta + next delta), got %d", len(got))
	}
	if !got[0].Key {
		t.Fatalf("expected first chunk to be keyframe")
	}
	if !bytes.Contains(got[0].Data, sps) || !bytes.Contains(got[0].Data, pps) || !bytes.Contains(got[0].Data, idr) {
		t.Fatalf("keyframe missing SPS/PPS/IDR: %x", got[0].Data)
	}
	if got[1].Key {
		t.Fatalf("expected second chunk to be delta frame")
	}
	if !bytes.Contains(got[1].Data, deltaSlice1) || !bytes.Contains(got[1].Data, deltaSlice2) {
		t.Fatalf("multi-slice delta chunk missing slices: %x", got[1].Data)
	}
	if got[2].Key || !bytes.Contains(got[2].Data, deltaNext) {
		t.Fatalf("third chunk mismatch: key=%v data=%x", got[2].Key, got[2].Data)
	}
}

func TestPresetStepping(t *testing.T) {
	if next, ok := StepDownPreset("ultra"); !ok || next != "high" {
		t.Fatalf("StepDownPreset(ultra) = (%q, %v)", next, ok)
	}
	if next, ok := StepDownPreset("high"); !ok || next != "medium" {
		t.Fatalf("StepDownPreset(high) = (%q, %v)", next, ok)
	}
	if next, ok := StepDownPreset("medium"); !ok || next != "low" {
		t.Fatalf("StepDownPreset(medium) = (%q, %v)", next, ok)
	}
	if next, ok := StepDownPreset("low"); ok || next != "low" {
		t.Fatalf("StepDownPreset(low) = (%q, %v)", next, ok)
	}
	if next, ok := StepUpPreset("low"); !ok || next != "medium" {
		t.Fatalf("StepUpPreset(low) = (%q, %v)", next, ok)
	}
	if next, ok := StepUpPreset("medium"); !ok || next != "high" {
		t.Fatalf("StepUpPreset(medium) = (%q, %v)", next, ok)
	}
	if next, ok := StepUpPreset("high"); !ok || next != "ultra" {
		t.Fatalf("StepUpPreset(high) = (%q, %v)", next, ok)
	}
	if next, ok := StepUpPreset("ultra"); ok || next != "ultra" {
		t.Fatalf("StepUpPreset(ultra) = (%q, %v)", next, ok)
	}
}

func TestHighBitratePresetsAndCodec(t *testing.T) {
	name, cfg := ResolveVideoPreset("ultra")
	if name != "ultra" || cfg.FPS != 60 || cfg.MaxDim != 3840 || cfg.BitrateKbps < 30000 {
		t.Fatalf("unexpected ultra preset: %s %+v", name, cfg)
	}
	if NormalizeVideoCodec("hevc") != "hevc" || NormalizeVideoCodec("H.265") != "hevc" {
		t.Fatalf("expected hevc normalization")
	}
	if NormalizeVideoCodec("") != "h264" {
		t.Fatalf("expected default h264 normalization")
	}
}
