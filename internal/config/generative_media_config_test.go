package config

import "testing"

func TestSaveVideoAndAudioGenConfigRoundTrip(t *testing.T) {
	setHomeTree(t, t.TempDir())

	video := VideoGenConfig{
		Enabled:     true,
		Model:       "custom-video-model",
		OutputPath:  "/tmp/videos",
		Timeout:     42,
		AspectRatio: "9:16",
		Resolution:  "1080p",
	}
	if err := SaveVideoGenConfig(video); err != nil {
		t.Fatalf("SaveVideoGenConfig: %v", err)
	}

	audio := AudioGenConfig{
		Enabled:    true,
		Model:      "custom-tts-model",
		Voice:      "Puck",
		OutputPath: "/tmp/audio",
		Timeout:    7,
	}
	if err := SaveAudioGenConfig(audio); err != nil {
		t.Fatalf("SaveAudioGenConfig: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Ocode.VideoGen != video {
		t.Errorf("VideoGen = %+v, want %+v", cfg.Ocode.VideoGen, video)
	}
	if cfg.Ocode.AudioGen != audio {
		t.Errorf("AudioGen = %+v, want %+v", cfg.Ocode.AudioGen, audio)
	}
}

func TestGenerativeMediaDefaults(t *testing.T) {
	v := DefaultVideoGenConfig()
	if v.Enabled || v.Timeout != 900 || v.AspectRatio != "16:9" || v.Resolution != "720p" {
		t.Errorf("DefaultVideoGenConfig = %+v", v)
	}
	a := DefaultAudioGenConfig()
	if a.Enabled || a.Voice != "Kore" || a.Timeout != 300 {
		t.Errorf("DefaultAudioGenConfig = %+v", a)
	}
}
