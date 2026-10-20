package stt

import (
	"strings"
	"testing"
)

// The local helper must pin the onnxruntime execution provider to the CPU.
//
// onnx-asr defaults to every available provider, so on Apple Silicon it picks
// CoreMLExecutionProvider first. CoreML has to compile the model to the Apple
// Neural Engine format on every fresh process, which costs ~12 s per call.
// Because ocode spawns a new Python process per utterance (and per 3-second
// live-partial tick), that compile was paid again and again: measured
// end-to-end through Transcribe() it made a 2.7-second clip take 17-22 s.
//
// The CPU provider loads the same model in ~1 s and produced an identical
// transcript, so pinning it is the difference between ~20 s and ~2 s per
// utterance. onnx-asr's CoreML path only supports a subset of the graph
// anyway (16 of 42 nodes on the encoder), so the "GPU" it bought was mostly
// a compile cost.
func TestOnnxHelperPinsCPUExecutionProvider(t *testing.T) {
	if !strings.Contains(transcribeOnnxScript, `providers=["CPUExecutionProvider"]`) {
		t.Fatalf("transcribe_onnx.py must pin providers=[\"CPUExecutionProvider\"]; " +
			"without it onnx-asr selects CoreML on Apple Silicon and each " +
			"utterance pays a ~12s Neural Engine compile")
	}
}

// The helper must still load the model exactly once, from the directory the
// Go side prepared. A regression here (loading twice, or loading from the
// Hugging Face cache) would reintroduce a multi-second cost per utterance.
func TestOnnxHelperLoadsModelOnceFromModelDir(t *testing.T) {
	if n := strings.Count(transcribeOnnxScript, "onnx_asr.load_model("); n != 1 {
		t.Fatalf("onnx_asr.load_model called %d times, want exactly 1", n)
	}
	if !strings.Contains(transcribeOnnxScript, "path=model_dir") {
		t.Fatal("onnx_asr.load_model must be given path=model_dir so the " +
			"prepared local model directory is used")
	}
}
