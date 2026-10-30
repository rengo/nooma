// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/channels"
	"github.com/rengo/nooma/internal/channels/telegram"
	"github.com/rengo/nooma/internal/config"
	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/phrase"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// I58 (doc 02 §5, ADR-0062): a voice note from an allowed chat is
// transcribed and read exactly like typed text; a stranger's is never
// fetched; the audio is never stored; a note that cannot be heard captures
// nothing and asks for it typed.

// voiceAudio stands in for a Telegram OGG/Opus voice note.
var voiceAudio = []byte("OggS fake opus frames")

// botAPI is a fake Bot API: one getUpdates batch, getFile, the file
// download, and sendMessage, every call recorded.
type botAPI struct {
	mu     sync.Mutex
	batch  []string
	polled bool
	// redeliver returns the batch on every poll, as Telegram does while an
	// earlier update in it is unconfirmed.
	redeliver bool
	getFiles  int
	fetched   int
	sent      []string
	file      []byte
}

func newBotAPI(t *testing.T, updates ...string) (*httptest.Server, *botAPI) {
	t.Helper()
	b := &botAPI{batch: updates, file: voiceAudio}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		defer b.mu.Unlock()
		switch {
		case strings.HasPrefix(r.URL.Path, "/file/bot"):
			b.fetched++
			_, _ = w.Write(b.file)
		case strings.HasSuffix(r.URL.Path, "/getFile"):
			b.getFiles++
			_, _ = w.Write([]byte(`{"ok":true,"result":{"file_id":"` + r.URL.Query().Get("file_id") + `","file_path":"voice/file_7.oga"}}`))
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			b.sent = append(b.sent, r.URL.Query().Get("text"))
			_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
		default:
			body := `{"ok":true,"result":[]}`
			if !b.polled || b.redeliver {
				body = `{"ok":true,"result":[` + strings.Join(b.batch, ",") + `]}`
				b.polled = true
			}
			_, _ = w.Write([]byte(body))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, b
}

func voiceUpdate(updateID, chatID int64, seconds int, lang string) string {
	return sizedVoiceUpdate(updateID, chatID, seconds, 0, lang)
}

// sizedVoiceUpdate declares the note's file_size, which Telegram may omit
// (zero leaves it out).
func sizedVoiceUpdate(updateID, chatID int64, seconds int, size int64, lang string) string {
	declared := ""
	if size > 0 {
		declared = `,"file_size":` + strconv.FormatInt(size, 10)
	}
	return `{"update_id":` + strconv.FormatInt(updateID, 10) +
		`,"message":{"message_id":1,"from":{"language_code":"` + lang + `"},"chat":{"id":` + strconv.FormatInt(chatID, 10) +
		`},"voice":{"file_id":"voice-` + strconv.FormatInt(updateID, 10) + `","duration":` + strconv.Itoa(seconds) + declared + `,"mime_type":"audio/ogg"}}}`
}

// fakeTranscriber answers every call with text or err, recording the audio.
type fakeTranscriber struct {
	text  string
	err   error
	calls [][]byte
}

func (f *fakeTranscriber) Transcribe(_ context.Context, req ports.TranscribeRequest) (ports.TranscribeResponse, error) {
	f.calls = append(f.calls, req.Audio)
	return ports.TranscribeResponse{Text: f.text, Model: "fake-transcribe"}, f.err
}

func voiceChannel(t *testing.T, baseURL string, tr ports.TranscriptionProvider) *telegram.Channel {
	t.Helper()
	cfg := config.Telegram{Enabled: true, BotTokenEnv: "BOT", AllowedChatIDs: []int64{42}}
	ch, err := telegram.New(cfg, func(string) (string, bool) { return "token", true }, nil, baseURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tr != nil {
		ch.WithTranscriber(tr)
	}
	return ch
}

func receive(t *testing.T, ch *telegram.Channel) []ports.ChannelMessage {
	t.Helper()
	msgs, err := ch.Receive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return msgs
}

// TestI58_AStrangersVoiceNoteIsNeverFetched: the allow-list runs before any
// download, so a stranger's audio never leaves Telegram.
func TestI58_AStrangersVoiceNoteIsNeverFetched(t *testing.T) {
	srv, api := newBotAPI(t, voiceUpdate(1, 7, 5, "es"))
	tr := &fakeTranscriber{text: "hola"}
	if msgs := receive(t, voiceChannel(t, srv.URL, tr)); len(msgs) != 0 {
		t.Fatalf("admitted %+v from a chat outside the allow-list", msgs)
	}
	if api.getFiles != 0 || api.fetched != 0 || len(tr.calls) != 0 || len(api.sent) != 0 {
		t.Errorf("getFile %d, downloads %d, transcriptions %d, replies %d; want none for a stranger", api.getFiles, api.fetched, len(tr.calls), len(api.sent))
	}
}

// TestI58_AVoiceNoteIsAdmittedAsItsTranscriptAndNeverStored: the audio is
// downloaded, transcribed in memory and dropped; only the transcript goes
// on, marked as voice.
func TestI58_AVoiceNoteIsAdmittedAsItsTranscriptAndNeverStored(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	srv, api := newBotAPI(t, voiceUpdate(1, 42, 12, "es"))
	tr := &fakeTranscriber{text: "  Comprar pan mañana  "}

	msgs := receive(t, voiceChannel(t, srv.URL, tr))
	if len(msgs) != 1 {
		t.Fatalf("admitted %d messages, want the voice note", len(msgs))
	}
	if got := msgs[0]; got.Text != "Comprar pan mañana" || !got.Voice || got.Conversation != "42" || got.Channel != telegram.Name {
		t.Errorf("admitted %+v, want the trimmed transcript, marked as voice, from chat 42", got)
	}
	if len(tr.calls) != 1 || string(tr.calls[0]) != string(voiceAudio) {
		t.Errorf("transcriber heard %q, want the downloaded audio once", tr.calls)
	}
	if len(api.sent) != 0 {
		t.Errorf("the channel replied %q itself; a transcript is answered by capture", api.sent)
	}
	if left, _ := os.ReadDir(tmp); len(left) != 0 {
		t.Errorf("the audio left %d file(s) in the temp dir; it is never written", len(left))
	}
}

// TestI58_VoiceUnconfiguredIsSaidAndNothingIsFetched: with no transcription
// task bound, the sender is asked to type it, in their language.
func TestI58_VoiceUnconfiguredIsSaidAndNothingIsFetched(t *testing.T) {
	srv, api := newBotAPI(t, voiceUpdate(1, 42, 5, "en"))
	if msgs := receive(t, voiceChannel(t, srv.URL, nil)); len(msgs) != 0 {
		t.Fatalf("admitted %+v with no transcriber", msgs)
	}
	if api.getFiles != 0 || api.fetched != 0 {
		t.Errorf("fetched the audio (%d getFile, %d downloads) with nothing to transcribe it", api.getFiles, api.fetched)
	}
	if want := phrase.For(classify.LanguageEN).VoiceOff; len(api.sent) != 1 || api.sent[0] != want {
		t.Errorf("replied %q, want %q", api.sent, want)
	}
}

// TestI58_ATooLongVoiceNoteIsRefusedBeforeItIsFetched: past either cap,
// the duration or the size Telegram declares, the note is refused politely
// and never downloaded.
func TestI58_ATooLongVoiceNoteIsRefusedBeforeItIsFetched(t *testing.T) {
	for name, update := range map[string]string{
		"longer than the cap": voiceUpdate(1, 42, telegram.MaxVoiceSeconds+1, "es"),
		"declared too large":  sizedVoiceUpdate(1, 42, 30, telegram.MaxVoiceBytes+1, "es"),
	} {
		t.Run(name, func(t *testing.T) {
			srv, api := newBotAPI(t, update)
			tr := &fakeTranscriber{text: "hola"}
			if msgs := receive(t, voiceChannel(t, srv.URL, tr)); len(msgs) != 0 {
				t.Fatalf("admitted %+v past the cap", msgs)
			}
			if api.getFiles != 0 || api.fetched != 0 || len(tr.calls) != 0 {
				t.Errorf("fetched or transcribed a note past the cap")
			}
			want := fmt.Sprintf(phrase.For(classify.LanguageES).VoiceTooLong, telegram.MaxVoiceSeconds/60)
			if len(api.sent) != 1 || api.sent[0] != want {
				t.Errorf("replied %q, want %q", api.sent, want)
			}
		})
	}
}

// TestI58_ARefusedNoteIsAnsweredOnceWhileItIsRedelivered: behind an
// unconfirmed message Telegram delivers a refused note again on every
// poll; it is downloaded and answered once, not once per poll.
func TestI58_ARefusedNoteIsAnsweredOnceWhileItIsRedelivered(t *testing.T) {
	held := `{"update_id":1,"message":{"message_id":1,"text":"hola","chat":{"id":42}}}`
	srv, api := newBotAPI(t, held, voiceUpdate(2, 42, 5, "es"))
	api.redeliver = true
	tr := &fakeTranscriber{err: errors.New("provider down")}
	ch := voiceChannel(t, srv.URL, tr)
	for poll := 1; poll <= 3; poll++ {
		// Update 1 is never confirmed, so the cursor stays behind the note.
		if msgs := receive(t, ch); len(msgs) != 1 || msgs[0].ID != "1" {
			t.Fatalf("poll %d admitted %+v, want only the held text", poll, msgs)
		}
	}
	if len(api.sent) != 1 || api.fetched != 1 || len(tr.calls) != 1 {
		t.Errorf("over 3 polls: %d replies %q, %d downloads, %d transcriptions; want one of each", len(api.sent), api.sent, api.fetched, len(tr.calls))
	}
}

// TestI58_AVoiceNoteThatCannotBeHeardCapturesNothing: an error, an empty
// transcript or an oversized download is answered by asking for it typed,
// and nothing is admitted.
func TestI58_AVoiceNoteThatCannotBeHeardCapturesNothing(t *testing.T) {
	cases := map[string]struct {
		tr   *fakeTranscriber
		file []byte
	}{
		"transcription fails": {&fakeTranscriber{err: errors.New("timeout")}, voiceAudio},
		"empty transcript":    {&fakeTranscriber{text: " \n "}, voiceAudio},
		"download too large":  {&fakeTranscriber{text: "hola"}, make([]byte, telegram.MaxVoiceBytes+1)},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			srv, api := newBotAPI(t, voiceUpdate(1, 42, 5, "es"))
			api.file = tc.file
			if msgs := receive(t, voiceChannel(t, srv.URL, tc.tr)); len(msgs) != 0 {
				t.Fatalf("admitted %+v from a note that was not heard", msgs)
			}
			if want := phrase.For(classify.LanguageES).VoiceUnheard; len(api.sent) != 1 || api.sent[0] != want {
				t.Errorf("replied %q, want %q", api.sent, want)
			}
		})
	}
}

// echoCapturer answers every capture with one fixed result.
type echoCapturer struct {
	result brain.CaptureResult
	got    brain.CaptureInput
}

func (c *echoCapturer) Capture(_ context.Context, in brain.CaptureInput) (brain.CaptureResult, error) {
	c.got = in
	return c.result, nil
}

type replyChannel struct {
	ports.Channel
	sent []string
}

func (c *replyChannel) Name() string { return "telegram" }
func (c *replyChannel) Send(_ context.Context, _ ports.ConversationID, text string) error {
	c.sent = append(c.sent, text)
	return nil
}

// TestI58_TheTranscriptIsEchoedOnlyWhenTheReadingIsNotActionable: the
// reply to a voice note is the reply to the same text typed, prefixed with
// what was heard exactly for a chitchat, an out-of-scope request, an ask
// and a which-timer question. Typed text is never prefixed.
func TestI58_TheTranscriptIsEchoedOnlyWhenTheReadingIsNotActionable(t *testing.T) {
	echoed := map[brain.CaptureOutcome]bool{
		brain.OutcomeConversed: true, brain.OutcomeOutOfScope: true, brain.OutcomeAsked: true,
	}
	which := &brain.TimersAnswer{Which: true}
	results := map[string]brain.CaptureResult{"timers which": {Outcome: brain.OutcomeTimers, Timers: which}}
	for _, o := range brain.AllCaptureOutcomes() {
		results[string(o)] = brain.CaptureResult{Outcome: o}
	}
	const heard = "comprá pan"
	for name, result := range results {
		result.Language = classify.LanguageES
		want := echoed[result.Outcome] || name == "timers which"
		if got := channels.EchoesTranscript(result); got != want {
			t.Errorf("%s: EchoesTranscript = %v, want %v", name, got, want)
		}
		for _, voice := range []bool{false, true} {
			capture := &echoCapturer{result: result}
			ch := &replyChannel{}
			msg := ports.ChannelMessage{ID: "1", Conversation: "42", Text: heard, Channel: "telegram", Voice: voice}
			if err := channels.CaptureHandler(capture, ch, nil, nil)(context.Background(), msg); err != nil {
				t.Fatal(err)
			}
			if capture.got.Text != heard || capture.got.Voice != voice {
				t.Errorf("%s: captured %+v, want the transcript with Voice=%v", name, capture.got, voice)
			}
			prefix := strings.Replace(phrase.For(classify.LanguageES).VoiceHeard, "%s", heard, 1)
			if got := strings.HasPrefix(ch.sent[0], prefix+"\n"); got != (voice && want) {
				t.Errorf("%s voice=%v: reply %q echoes the transcript = %v", name, voice, ch.sent[0], got)
			}
		}
	}
}

// TestI58_AVoiceCaptureIsMarkedInTheTrail: a capture from a voice note
// writes one capture.voice.transcribed row naming its outcome and unit at
// the capture's instant, and a typed capture writes no such row.
func TestI58_AVoiceCaptureIsMarkedInTheTrail(t *testing.T) {
	const message = "Llamar a Ana"
	const response = `{"type":"task","normalized_content":"Llamar a Ana","weight":0.5,"decay_rate":0.03}`
	for _, voice := range []bool{true, false} {
		ctx := context.Background()
		now := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
		decisions, embeddings, units := memrepo.NewDecisionLog(), memrepo.NewEmbeddings(), memrepo.NewUnits()
		idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
		if err != nil {
			t.Fatal(err)
		}
		llm := fakeprovider.New(t, i28Case(t, "voice", message, response), "voice")
		svc := brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, units, embeddings, memrepo.NewLexical(),
			memrepo.NewRelations(), decisions, llm, llm, llm, fakeprovider.NewEmbeddingFake(embedFakeModel),
			brain.NewIndex(idx), memrepo.NewSignals(), memrepo.NewTriggers(), memrepo.NewTimers(), 0.5, nil)
		result, err := svc.Capture(ctx, brain.CaptureInput{Text: message, Channel: "telegram", Voice: voice})
		if err != nil {
			t.Fatal(err)
		}
		rows, err := decisions.Since(ctx, time.Time{}, -1)
		if err != nil {
			t.Fatal(err)
		}
		var marked []ports.Decision
		for _, r := range rows {
			if r.Action == ports.ActionCaptureVoiceTranscribed {
				marked = append(marked, r)
			}
		}
		if !voice {
			if len(marked) != 0 {
				t.Errorf("a typed capture wrote %d voice rows", len(marked))
			}
			continue
		}
		if len(marked) != 1 {
			t.Fatalf("a voice capture wrote %d %s rows, want one", len(marked), ports.ActionCaptureVoiceTranscribed)
		}
		var got struct{ Source, Outcome, UnitID string }
		if err := json.Unmarshal(marked[0].Context, &struct {
			Source  *string `json:"source"`
			Outcome *string `json:"outcome"`
			UnitID  *string `json:"unit_id"`
		}{&got.Source, &got.Outcome, &got.UnitID}); err != nil {
			t.Fatal(err)
		}
		if got.Source != "voice" || got.Outcome != string(brain.OutcomeStored) || got.UnitID != result.UnitID || !marked[0].OccurredAt.Equal(now) {
			t.Errorf("voice row = %+v at %v, want source voice, outcome stored, unit %s at %v", got, marked[0].OccurredAt, result.UnitID, now)
		}
	}
}
