// Package synth defines the text-to-speech client contract and its ElevenLabs
// implementation. Only a handful of endpoints are needed (list voices, TTS, TTS
// with timestamps), so the engine owns a small REST client rather than pulling
// in a third-party SDK.
package synth

import "context"

// AudioFormat is an ElevenLabs output_format identifier.
type AudioFormat string

const (
	MP3_44100_192 AudioFormat = "mp3_44100_192"
	MP3_44100_128 AudioFormat = "mp3_44100_128"
	PCM_44100     AudioFormat = "pcm_44100" // wrapped into WAV by the output stage
)

// VoiceSettings are the per-request expressiveness knobs.
type VoiceSettings struct {
	Stability       float64
	SimilarityBoost float64
	Style           float64
	UseSpeakerBoost bool
}

// Request is a single synthesis request.
type Request struct {
	VoiceID        string
	Text           string
	ModelID        string
	Format         AudioFormat
	WithTimestamps bool
	VoiceSettings  *VoiceSettings // optional per-line override
}

// WordTiming is a word-level timing derived from ElevenLabs character alignment.
type WordTiming struct {
	Word  string
	Start float64 // seconds
	End   float64 // seconds
}

// Result is the synthesized audio plus optional word timings.
type Result struct {
	Audio []byte
	Words []WordTiming // populated only when Request.WithTimestamps is true
}

// Voice is an available ElevenLabs voice.
type Voice struct {
	ID   string
	Name string
}

// Subscription describes the account behind the API key. The API does not
// report a concurrency limit, so callers derive one from Tier via MaxConcurrency.
type Subscription struct {
	Tier           string
	CharacterCount int
	CharacterLimit int
}

// Client is the ElevenLabs contract the engine depends on.
type Client interface {
	Synthesize(ctx context.Context, req Request) (*Result, error)
	Voices(ctx context.Context) ([]Voice, error)
	Subscription(ctx context.Context) (*Subscription, error)
}
