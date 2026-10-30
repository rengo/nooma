package telegram

import (
	"context"
	"fmt"
	"strings"

	"github.com/rengo/nooma/internal/core/phrase"
	"github.com/rengo/nooma/internal/ports"
)

// MaxVoiceSeconds and MaxVoiceBytes cap a voice note Nooma will hear
// (doc 02 §13, ADR-0062). Five minutes is a long thought, not a lecture,
// and bounds one transcription's cost; five MiB is several times what five
// minutes of Telegram's Opus weigh, and far inside both the Bot API's 20 MB
// download limit and the transcription API's 25 MB upload limit.
const (
	MaxVoiceSeconds       = 300
	MaxVoiceBytes   int64 = 5 << 20
)

// WithTranscriber lets the channel hear voice notes through t, the
// audio_transcription task. Without one, a voice note is answered by
// asking for it typed.
func (c *Channel) WithTranscriber(t ports.TranscriptionProvider) *Channel {
	c.transcriber = t
	return c
}

// hear turns an admitted voice note into the text capture reads, or answers
// it itself and reports ok false: no transcriber, past the cap, or not
// heard. Every refusal is answered once, as a command is. The audio lives in
// memory for the one call and is dropped; only the transcript goes on.
//
// err is set only when ctx ended mid-way, which is shutdown, not a note that
// could not be heard.
func (c *Channel) hear(ctx context.Context, id, chat int64, v *voice, lang string) (text string, ok bool, err error) {
	if c.answeredAlready(id) {
		return "", false, nil
	}
	say := phrase.For(languageOf(lang))
	var refusal string
	switch {
	case c.transcriber == nil:
		refusal = say.VoiceOff
	case v.Duration > MaxVoiceSeconds || v.FileSize > MaxVoiceBytes:
		refusal = fmt.Sprintf(say.VoiceTooLong, MaxVoiceSeconds/60)
	default:
		heard, terr := c.transcribe(ctx, v)
		if ctx.Err() != nil {
			return "", false, ctx.Err()
		}
		if terr == nil && heard != "" {
			return heard, true, nil
		}
		c.logf("telegram: a voice note from chat %d was not heard (%d s): %v\n", chat, v.Duration, terr)
		refusal = say.VoiceUnheard
	}
	c.answer(id)
	if serr := c.client.sendMessage(ctx, chat, refusal); serr != nil {
		c.logf("telegram: a voice note from chat %d was not answered: %v\n", chat, serr)
	}
	return "", false, nil
}

// transcribe downloads the note into memory and transcribes it. An empty
// transcript is returned as "", which hear treats as not heard.
func (c *Channel) transcribe(ctx context.Context, v *voice) (string, error) {
	audio, err := c.client.download(ctx, v.FileID, MaxVoiceBytes)
	if err != nil {
		return "", err
	}
	resp, err := c.transcriber.Transcribe(ctx, ports.TranscribeRequest{Audio: audio, Format: "ogg"})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Text), nil
}

// answeredAlready reports whether update id was already answered by the
// channel itself, so a redelivery is not downloaded or answered again.
func (c *Channel) answeredAlready(id int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.answered[id]
}
