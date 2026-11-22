package desktop

import (
	"bytes"
	"testing"
)

func TestAnnexBSplitter(t *testing.T) {
	sps := []byte{0x67, 0x42, 0xE0, 0x1F, 0xAA}
	pps := []byte{0x68, 0xCE, 0x38, 0x80}
	idr := []byte{0x65, 0x88, 0x84, 0x21}
	delta := []byte{0x41, 0x9A, 0x22, 0x11}

	var stream bytes.Buffer
	// 4-byte and 3-byte start codes mixed
	stream.Write([]byte{0x00, 0x00, 0x00, 0x01})
	stream.Write(sps)
	stream.Write([]byte{0x00, 0x00, 0x01})
	stream.Write(pps)
	stream.Write([]byte{0x00, 0x00, 0x00, 0x01})
	stream.Write(idr)
	stream.Write([]byte{0x00, 0x00, 0x01})
	stream.Write(delta)

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

	if len(got) != 2 {
		t.Fatalf("expected 2 chunks (key + delta), got %d", len(got))
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
	if !bytes.Contains(got[1].Data, delta) {
		t.Fatalf("delta chunk missing slice: %x", got[1].Data)
	}
}

func TestPresetStepping(t *testing.T) {
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
	if next, ok := StepUpPreset("high"); ok || next != "high" {
		t.Fatalf("StepUpPreset(high) = (%q, %v)", next, ok)
	}
}
